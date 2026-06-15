package concurrency

import (
	"sync"
	"testing"
)

func TestAdaptiveLimiter_Basic(t *testing.T) {
	l := NewAdaptiveLimiter(4)
	if l.CurrentLimit() != 4 {
		t.Errorf("expected limit 4, got %d", l.CurrentLimit())
	}
	if l.MaxLimit() != 4 {
		t.Errorf("expected max 4, got %d", l.MaxLimit())
	}
}

func TestAdaptiveLimiter_TryAcquire(t *testing.T) {
	l := NewAdaptiveLimiter(2)

	if !l.TryAcquire() {
		t.Error("expected acquire 1 to succeed")
	}
	if !l.TryAcquire() {
		t.Error("expected acquire 2 to succeed")
	}
	if l.TryAcquire() {
		t.Error("expected acquire 3 to fail (limit=2)")
	}

	l.Release()
	if !l.TryAcquire() {
		t.Error("expected acquire after release to succeed")
	}
}

func TestAdaptiveLimiter_AdjustReduction(t *testing.T) {
	l := NewAdaptiveLimiter(8)

	// Simulate 80% failure rate: 8 failures, 2 successes
	for i := 0; i < 8; i++ {
		l.RecordResult(false)
	}
	for i := 0; i < 2; i++ {
		l.RecordResult(true)
	}

	l.Adjust()

	if l.CurrentLimit() >= 8 {
		t.Errorf("expected limit to be reduced after 80%% failure, got %d", l.CurrentLimit())
	}
}

func TestAdaptiveLimiter_AdjustRecovery(t *testing.T) {
	l := NewAdaptiveLimiter(8)

	// First, cause reduction
	for i := 0; i < 20; i++ {
		l.RecordResult(false)
	}
	l.Adjust()
	reduced := l.CurrentLimit()

	// Then, simulate recovery with 100% success
	l.failures.Store(0)
	l.totalCalls.Store(20)
	for i := 0; i < 20; i++ {
		l.RecordResult(true)
	}
	l.Adjust()

	if l.CurrentLimit() <= reduced {
		t.Errorf("expected limit to increase after recovery, was %d, now %d", reduced, l.CurrentLimit())
	}
}

func TestProviderConcurrencyManager(t *testing.T) {
	m := NewProviderConcurrencyManager(4)

	l1 := m.GetLimiter("provider-a")
	l2 := m.GetLimiter("provider-b")
	l3 := m.GetLimiter("provider-a") // same as l1

	if l1 != l3 {
		t.Error("expected same limiter for same provider name")
	}
	if l1 == l2 {
		t.Error("expected different limiters for different providers")
	}

	snap := m.Snapshot()
	if _, ok := snap["provider-a"]; !ok {
		t.Error("expected provider-a in snapshot")
	}
	if _, ok := snap["provider-b"]; !ok {
		t.Error("expected provider-b in snapshot")
	}
}

func TestAdaptiveLimiter_Concurrent(t *testing.T) {
	l := NewAdaptiveLimiter(8)
	var wg sync.WaitGroup

	// Acquire all 8 slots concurrently
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !l.TryAcquire() {
				t.Error("expected all 8 acquires to succeed")
			}
		}()
	}
	wg.Wait()

	// 9th should fail
	if l.TryAcquire() {
		t.Error("expected 9th acquire to fail")
	}

	// Release all
	for i := 0; i < 8; i++ {
		l.Release()
	}

	// Now should succeed again
	if !l.TryAcquire() {
		t.Error("expected acquire after full release to succeed")
	}
	l.Release()
}

func TestAdaptiveLimiter_DefaultMax(t *testing.T) {
	m := NewProviderConcurrencyManager(0) // should default to 4
	l := m.GetLimiter("test")
	if l.MaxLimit() != 4 {
		t.Errorf("expected max 4, got %d", l.MaxLimit())
	}
}
