// Package health provides periodic provider health checking with auto-recovery.
package health

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/lhy/openfusion/internal/logger"
)

// Checker periodically pings providers and tracks health status.
type Checker struct {
	configs  map[string]Config
	statuses map[string]*ProviderStatus
}

// Config defines health check parameters for one provider.
type Config struct {
	Enabled          bool
	Interval         time.Duration
	Timeout          time.Duration
	Endpoint         string
	FailureThreshold int
	// PingFn is called to check health. If nil, provider is assumed healthy.
	PingFn func(ctx context.Context, name string) error
}

// ProviderStatus tracks the health state of one provider.
type ProviderStatus struct {
	Healthy    atomic.Bool
	failCount  atomic.Int64
}

// NewChecker creates a health checker with the given provider configs.
func NewChecker(configs map[string]Config) *Checker {
	c := &Checker{
		configs:  configs,
		statuses: make(map[string]*ProviderStatus),
	}
	for name, cfg := range configs {
		ps := &ProviderStatus{}
		ps.Healthy.Store(true) // initially assumed healthy
		c.statuses[name] = ps
		_ = cfg // will use when starting
	}
	return c
}

// Start begins periodic health checks for all enabled providers.
// Each provider runs on its own goroutine with the configured interval.
func (c *Checker) Start(ctx context.Context) {
	for name, cfg := range c.configs {
		if !cfg.Enabled {
			continue
		}
		ps := c.statuses[name]
		interval := cfg.Interval
		if interval <= 0 {
			interval = 30 * time.Second
		}

		go func(providerName string, providerCfg Config, status *ProviderStatus) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("health check panelic", fmt.Errorf("%v", r), "provider", providerName)
				}
			}()
			// Initial check
			c.check(providerName, providerCfg, status)

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					c.check(providerName, providerCfg, status)
				}
			}
		}(name, cfg, ps)
	}
}

// IsHealthy returns whether the named provider is considered healthy.
func (c *Checker) IsHealthy(name string) bool {
	ps, ok := c.statuses[name]
	if !ok {
		return true // unknown providers assumed healthy
	}
	return ps.Healthy.Load()
}

// check performs a single health check and updates status.
func (c *Checker) check(name string, cfg Config, status *ProviderStatus) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	var err error
	if cfg.PingFn != nil {
		err = cfg.PingFn(ctx, name)
	}

	if err != nil {
		fail := status.failCount.Add(1)
		threshold := int64(cfg.FailureThreshold)
		if threshold <= 0 {
			threshold = 3
		}
		if fail >= threshold {
			if status.Healthy.Swap(false) {
				logger.Warn("provider unhealthy", "name", name, "failures", fmt.Sprintf("%d", fail), "threshold", fmt.Sprintf("%d", threshold), "error", err.Error())
			}
		}
	} else {
		status.failCount.Store(0)
		if !status.Healthy.Swap(true) {
			logger.Info("provider recovered", "name", name)
		}
	}
}
