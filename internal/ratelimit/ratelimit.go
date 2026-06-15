// Package ratelimit provides per-preset token bucket rate limiting.
package ratelimit

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Config mirrors the YAML rate_limit config.
type Config struct {
	Enabled bool
	Default LimitConfig
	Presets map[string]LimitConfig
}

// LimitConfig defines rate and burst for a single limiter.
type LimitConfig struct {
	Rate  float64
	Burst int
}

// Limiter manages per-preset token bucket limiters.
type Limiter struct {
	mu       sync.RWMutex
	enabled  bool
	limiters map[string]*rate.Limiter
	defaultC LimitConfig
}

// NewLimiter creates a rate limiter from config.
// presetNames is the list of all known preset names.
func NewLimiter(cfg Config, presetNames []string) *Limiter {
	l := &Limiter{
		enabled:  cfg.Enabled,
		limiters: make(map[string]*rate.Limiter),
		defaultC: cfg.Default,
	}

	if !cfg.Enabled {
		return l
	}

	// Create a limiter for each known preset
	for _, name := range presetNames {
		presetCfg, ok := cfg.Presets[name]
		if !ok {
			presetCfg = cfg.Default
		}
		l.limiters[name] = rate.NewLimiter(rate.Limit(presetCfg.Rate), presetCfg.Burst)
	}

	return l
}

// Allow checks if a request for the given preset should be allowed.
// Returns (allowed bool, retryAfter time.Duration).
// If not allowed, retryAfter is the suggested wait time.
func (l *Limiter) Allow(presetName string) (bool, time.Duration) {
	if !l.enabled {
		return true, 0
	}

	l.mu.RLock()
	lim, ok := l.limiters[presetName]
	l.mu.RUnlock()

	if !ok {
		// Unknown preset — create on the fly with default limits
		l.mu.Lock()
		lim = rate.NewLimiter(rate.Limit(l.defaultC.Rate), l.defaultC.Burst)
		l.limiters[presetName] = lim
		l.mu.Unlock()
	}

	// Reserve a token
	r := lim.Reserve()
	if r.OK() {
		delay := r.Delay()
		if delay > 0 {
			r.Cancel() // Let the caller decide what to do with the delay
			return false, delay
		}
		return true, 0
	}

	return false, time.Duration(0)
}

// Enabled returns whether rate limiting is active.
func (l *Limiter) Enabled() bool {
	return l.enabled
}

// IsRateLimitedError creates a standard 429 error message.
func IsRateLimitedError(presetName string, retryAfter time.Duration) error {
	return fmt.Errorf("rate limit exceeded for preset '%s', retry after %.1fs", presetName, retryAfter.Seconds())
}
