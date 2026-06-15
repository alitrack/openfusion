// Package concurrency provides adaptive per-provider concurrency limiting.
package concurrency

import (
	"math"
	"runtime"
	"sync"
	"sync/atomic"
)

// ---------------------------------------------------------------------------
// AdaptiveLimiter
// ---------------------------------------------------------------------------

// AdaptiveLimiter adjusts its concurrency limit based on success/failure rate.
// On high error rate → reduce capacity; on sustained success → gradually recover.
type AdaptiveLimiter struct {
	mu           sync.Mutex
	max          int64 // maximum concurrent calls
	current      int64 // current limit (may be reduced below max)
	inFlight     atomic.Int64
	failures     atomic.Int64
	totalCalls   atomic.Int64
	recoveryStep float64
}

// NewAdaptiveLimiter creates a limiter with the given max concurrency.
func NewAdaptiveLimiter(max int) *AdaptiveLimiter {
	if max < 1 {
		max = 1
	}
	return &AdaptiveLimiter{
		max:          int64(max),
		current:      int64(max),
		recoveryStep: 0.25, // recover 25% per recovery window
	}
}

// Acquire blocks until a slot is available. Returns false if context cancelled.
// This is a non-blocking check for integration with existing goroutine patterns.
// Returns true if acquired, false if at capacity.
func (l *AdaptiveLimiter) TryAcquire() bool {
	for {
		cur := atomic.LoadInt64(&l.current)
		if cur <= 0 {
			return false
		}
		in := l.inFlight.Load()
		if in >= cur {
			return false
		}
		if l.inFlight.CompareAndSwap(in, in+1) {
			return true
		}
		runtime.Gosched()
	}
}

// Release releases a previously acquired slot.
func (l *AdaptiveLimiter) Release() {
	l.inFlight.Add(-1)
}

// RecordResult feeds back the outcome of a call. errorRate is 0-1.
func (l *AdaptiveLimiter) RecordResult(success bool) {
	l.totalCalls.Add(1)
	if !success {
		l.failures.Add(1)
	}
}

// Adjust recalculates the current limit based on recent failure rate.
func (l *AdaptiveLimiter) Adjust() {
	total := l.totalCalls.Load()
	if total == 0 {
		return
	}

	fails := l.failures.Load()
	errorRate := float64(fails) / float64(total)

	l.mu.Lock()
	defer l.mu.Unlock()

	cur := atomic.LoadInt64(&l.current)
	max := atomic.LoadInt64(&l.max)

	if errorRate > 0.3 && cur > 1 {
		// High error rate → reduce by 50%, min 1
		newLimit := int64(math.Max(1, float64(cur)*0.5))
		atomic.StoreInt64(&l.current, newLimit)
	} else if errorRate < 0.05 && cur < max {
		// Low error rate → recover by recovery_step
		newLimit := int64(math.Min(float64(max), float64(cur)+float64(max)*l.recoveryStep))
		atomic.StoreInt64(&l.current, newLimit)
	}

	// Age out old failures (keep history finite)
	l.failures.Store(int64(float64(fails) * 0.5))
	l.totalCalls.Store(int64(float64(total) * 0.5))
}

// CurrentLimit returns the current adaptive limit.
func (l *AdaptiveLimiter) CurrentLimit() int {
	return int(atomic.LoadInt64(&l.current))
}

// InFlight returns the current number of in-flight calls.
func (l *AdaptiveLimiter) InFlight() int {
	return int(l.inFlight.Load())
}

// MaxLimit returns the configured maximum.
func (l *AdaptiveLimiter) MaxLimit() int {
	return int(atomic.LoadInt64(&l.max))
}

// ---------------------------------------------------------------------------
// ProviderConcurrencyManager
// ---------------------------------------------------------------------------

// ProviderConcurrencyManager manages per-provider adaptive limiters.
type ProviderConcurrencyManager struct {
	mu        sync.RWMutex
	limiters  map[string]*AdaptiveLimiter
	defaultMax int
}

// NewProviderConcurrencyManager creates a manager with the default max per provider.
func NewProviderConcurrencyManager(defaultMax int) *ProviderConcurrencyManager {
	if defaultMax < 1 {
		defaultMax = 4
	}
	return &ProviderConcurrencyManager{
		limiters:   make(map[string]*AdaptiveLimiter),
		defaultMax: defaultMax,
	}
}

// GetLimiter returns the limiter for a provider, creating it if needed.
func (m *ProviderConcurrencyManager) GetLimiter(provider string) *AdaptiveLimiter {
	m.mu.RLock()
	l, ok := m.limiters[provider]
	m.mu.RUnlock()
	if ok {
		return l
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Double-check after acquiring write lock
	if l, ok := m.limiters[provider]; ok {
		return l
	}
	l = NewAdaptiveLimiter(m.defaultMax)
	m.limiters[provider] = l
	return l
}

// AdjustAll calls Adjust on every provider limiter.
func (m *ProviderConcurrencyManager) AdjustAll() {
	m.mu.RLock()
	limiters := make([]*AdaptiveLimiter, 0, len(m.limiters))
	for _, l := range m.limiters {
		limiters = append(limiters, l)
	}
	m.mu.RUnlock()

	for _, l := range limiters {
		l.Adjust()
	}
}

// Snapshot returns current limits for all providers.
func (m *ProviderConcurrencyManager) Snapshot() map[string]map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := make(map[string]map[string]int, len(m.limiters))
	for name, l := range m.limiters {
		snap[name] = map[string]int{
			"max":       l.MaxLimit(),
			"current":   l.CurrentLimit(),
			"in_flight": l.InFlight(),
		}
	}
	return snap
}
