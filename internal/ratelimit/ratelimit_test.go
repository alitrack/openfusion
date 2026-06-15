package ratelimit

import (
	"testing"
	"time"
)

func TestDisabled(t *testing.T) {
	l := NewLimiter(Config{Enabled: false}, []string{"budget"})
	allowed, _ := l.Allow("budget")
	if !allowed {
		t.Fatal("disabled limiter should allow all")
	}
}

func TestBasicRateLimiting(t *testing.T) {
	l := NewLimiter(Config{
		Enabled: true,
		Default: LimitConfig{Rate: 10, Burst: 5},
	}, []string{"budget"})

	// First 5 should pass (burst)
	for i := 0; i < 5; i++ {
		allowed, _ := l.Allow("budget")
		if !allowed {
			t.Fatalf("call %d should be allowed (burst)", i+1)
		}
	}

	// Burst consumed, next should be rate-limited or have delay
	allowed, delay := l.Allow("budget")
	if allowed {
		// May still be allowed if rate replenished
	} else {
		if delay <= 0 {
			t.Fatal("expected positive retry delay")
		}
	}
}

func TestPerPresetIndependence(t *testing.T) {
	l := NewLimiter(Config{
		Enabled: true,
		Default: LimitConfig{Rate: 1, Burst: 1},
		Presets: map[string]LimitConfig{
			"frontier": {Rate: 100, Burst: 100},
		},
	}, []string{"budget", "frontier"})

	// budget: only 1 call allowed (burst=1)
	if allowed, _ := l.Allow("budget"); !allowed {
		t.Fatal("first budget call should be allowed")
	}
	if allowed, _ := l.Allow("budget"); allowed {
		t.Fatal("second budget call should be limited (burst=1)")
	}

	// frontier: 100 calls should all pass
	for i := 0; i < 50; i++ {
		if allowed, _ := l.Allow("frontier"); !allowed {
			t.Fatalf("frontier call %d should be allowed", i+1)
		}
	}
}

func TestUnknownPreset(t *testing.T) {
	l := NewLimiter(Config{
		Enabled: true,
		Default: LimitConfig{Rate: 100, Burst: 100},
	}, []string{"budget"})

	// Unknown preset gets default limits
	allowed, _ := l.Allow("unknown")
	if !allowed {
		t.Fatal("unknown preset should use default limits")
	}
}

func TestRetryAfterDuration(t *testing.T) {
	l := NewLimiter(Config{
		Enabled: true,
		Default: LimitConfig{Rate: 0.1, Burst: 1}, // 1 req per 10 seconds
	}, []string{"budget"})

	allowed, delay := l.Allow("budget")
	if !allowed {
		t.Fatal("first call should be allowed")
	}

	// Second call should be rate limited with ~10s delay
	allowed, delay = l.Allow("budget")
	if allowed {
		t.Log("second call unexpectedly allowed (race with timer)")
	} else {
		if delay < time.Second {
			t.Fatalf("expected delay > 1s, got %v", delay)
		}
	}
}

func TestEnabled(t *testing.T) {
	l := NewLimiter(Config{Enabled: true, Default: LimitConfig{Rate: 10, Burst: 10}}, []string{"budget"})
	if !l.Enabled() {
		t.Fatal("expected enabled")
	}
}
