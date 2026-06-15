package health

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestInitiallyHealthy(t *testing.T) {
	configs := map[string]Config{
		"test-provider": {Enabled: false},
	}
	c := NewChecker(configs)
	if !c.IsHealthy("test-provider") {
		t.Fatal("provider should start healthy")
	}
}

func TestUnknownProvider(t *testing.T) {
	c := NewChecker(map[string]Config{})
	if !c.IsHealthy("unknown") {
		t.Fatal("unknown provider should be assumed healthy")
	}
}

func TestHealthCheckFailure(t *testing.T) {
	configs := map[string]Config{
		"test-provider": {
			Enabled:          true,
			Interval:         10 * time.Millisecond,
			Timeout:          100 * time.Millisecond,
			FailureThreshold: 2,
			PingFn: func(ctx context.Context, name string) error {
				return errors.New("connection refused")
			},
		},
	}
	c := NewChecker(configs)

	// Manually trigger checks
	for i := 0; i < 5; i++ {
		status := c.statuses["test-provider"]
		status.failCount.Add(1)
		if status.failCount.Load() >= 2 {
			status.Healthy.Store(false)
		}
	}

	if c.IsHealthy("test-provider") {
		t.Fatal("expected provider to become unhealthy after failures")
	}
}

func TestHealthCheckRecovery(t *testing.T) {
	var callCount atomic.Int64
	configs := map[string]Config{
		"test-provider": {
			Enabled:          true,
			Interval:         10 * time.Millisecond,
			Timeout:          100 * time.Millisecond,
			FailureThreshold: 2,
			PingFn: func(ctx context.Context, name string) error {
				callCount.Add(1)
				return nil
			},
		},
	}
	c := NewChecker(configs)

	// Simulate failure then recovery
	status := c.statuses["test-provider"]
	status.failCount.Store(3)
	status.Healthy.Store(false)

	if c.IsHealthy("test-provider") {
		t.Fatal("expected unhealthy")
	}

	// Simulate successful ping
	status.failCount.Store(0)
	status.Healthy.Store(true)

	if !c.IsHealthy("test-provider") {
		t.Fatal("expected recovered")
	}
}
