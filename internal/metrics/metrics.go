// Package metrics provides request-level and cost tracking for OpenFusion.
package metrics

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// Collector aggregates fusion request metrics.
// All methods are thread-safe.
type Collector struct {
	startTime time.Time

	mu      sync.RWMutex
	presets map[string]*presetMetrics
}

// presetMetrics holds counters for one fusion preset.
// All access goes through Collector methods which hold Collector.mu.
type presetMetrics struct {
	Requests       atomic.Int64
	Success        atomic.Int64
	Failed         atomic.Int64
	TotalCostUSD   atomic.Value // float64
	TotalPanelCost atomic.Value // float64
	TotalJudgeCost atomic.Value // float64
	TotalTokens    atomic.Int64
	TotalDuration  atomic.Int64 // ms

	// Per-model breakdown
	models sync.Map // map[string]*modelMetrics

	// Duration samples for quantile calculation (capped ring)
	durations    []float64
	durationsCap int
}

type modelMetrics struct {
	Calls         atomic.Int64
	Success       atomic.Int64
	Failed        atomic.Int64
	TotalDuration atomic.Int64 // ms
	TotalTokens   atomic.Int64
	TotalCostUSD  atomic.Value // float64
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewCollector creates a metrics collector.
func NewCollector() *Collector {
	return &Collector{
		startTime: time.Now(),
		presets:   make(map[string]*presetMetrics),
	}
}

// ---------------------------------------------------------------------------
// Recorder methods
// ---------------------------------------------------------------------------

// RecordRequest increments the request counter for a preset.
func (c *Collector) RecordRequest(preset string) {
	pm := c.getOrCreatePreset(preset)
	if pm != nil {
		pm.Requests.Add(1)
	}
}

// RecordPanelCall records a single panel model call result.
func (c *Collector) RecordPanelCall(preset, model string, duration time.Duration, tokens int, costUSD float64, success bool) {
	pm := c.getOrCreatePreset(preset)
	if pm == nil {
		return
	}

	mm := pm.getOrCreateModel(model)
	if success {
		mm.Success.Add(1)
	} else {
		mm.Failed.Add(1)
	}
	mm.Calls.Add(1)
	mm.TotalDuration.Add(duration.Milliseconds())
	mm.TotalTokens.Add(int64(tokens))
	addFloat(&mm.TotalCostUSD, costUSD)

	// Accumulate panel cost to preset
	addFloat(&pm.TotalPanelCost, costUSD)
	addFloat(&pm.TotalCostUSD, costUSD)
}

// RecordJudgeCall records the judge model call result.
func (c *Collector) RecordJudgeCall(preset string, duration time.Duration, tokens int, costUSD float64) {
	pm := c.getOrCreatePreset(preset)
	if pm == nil {
		return
	}
	pm.TotalTokens.Add(int64(tokens))
	addFloat(&pm.TotalJudgeCost, costUSD)
	addFloat(&pm.TotalCostUSD, costUSD)

	c.mu.Lock()
	pm.durations = append(pm.durations, float64(duration.Milliseconds()))
	if len(pm.durations) > pm.durationsCap {
		pm.durations = pm.durations[len(pm.durations)-pm.durationsCap:]
	}
	c.mu.Unlock()
}

// RecordFusionComplete records a completed fusion call (success or failure).
func (c *Collector) RecordFusionComplete(preset string, duration time.Duration, success bool) {
	pm := c.getOrCreatePreset(preset)
	if pm == nil {
		return
	}
	if success {
		pm.Success.Add(1)
	} else {
		pm.Failed.Add(1)
	}
	pm.TotalDuration.Add(duration.Milliseconds())
}

// ---------------------------------------------------------------------------
// Query methods
// ---------------------------------------------------------------------------

// Snapshot returns a serializable snapshot of all metrics.
func (c *Collector) Snapshot() *Snapshot {
	s := &Snapshot{
		UptimeSeconds: int64(time.Since(c.startTime).Seconds()),
		Presets:       make(map[string]PresetSnapshot),
	}

	c.mu.RLock()
	names := make([]string, 0, len(c.presets))
	for name := range c.presets {
		names = append(names, name)
	}
	c.mu.RUnlock()

	// Total across all presets
	var totalReq, totalCost float64

	for _, name := range names {
		c.mu.RLock()
		pm := c.presets[name]
		c.mu.RUnlock()

		ps := PresetSnapshot{
			Requests:      pm.Requests.Load(),
			Success:       pm.Success.Load(),
			Failed:        pm.Failed.Load(),
			TotalTokens:   pm.TotalTokens.Load(),
			AvgDurationMs: avgFromSum(pm.TotalDuration.Load(), pm.Requests.Load()),
			PanelModels:   make(map[string]ModelSnapshot),
		}
		ps.TotalCostUSD = loadFloat(&pm.TotalCostUSD)
		ps.TotalPanelCost = loadFloat(&pm.TotalPanelCost)
		ps.TotalJudgeCost = loadFloat(&pm.TotalJudgeCost)
		ps.P50Ms, ps.P90Ms, ps.P99Ms = c.computeQuantiles(pm)

		// Iterate models
		c.mu.RLock()
		modelNames := make([]string, 0)
		pm.models.Range(func(key, _ interface{}) bool {
			modelNames = append(modelNames, key.(string))
			return true
		})
		c.mu.RUnlock()

		for _, mn := range modelNames {
			c.mu.RLock()
			mmv, _ := pm.models.Load(mn)
		mm := mmv.(*modelMetrics)
		c.mu.RUnlock()

		ms := ModelSnapshot{
				Calls:         mm.Calls.Load(),
				Success:       mm.Success.Load(),
				Failed:        mm.Failed.Load(),
				AvgDurationMs: avgFromSum(mm.TotalDuration.Load(), mm.Calls.Load()),
				TotalTokens:   mm.TotalTokens.Load(),
			}
			ms.TotalCostUSD = loadFloat(&mm.TotalCostUSD)
			ps.PanelModels[mn] = ms
		}

		s.Presets[name] = ps
		totalReq += float64(ps.Requests)
		totalCost += ps.TotalCostUSD
	}

	s.TotalRequests = int64(totalReq)
	s.TotalCostUSD = totalCost
	return s
}

// ---------------------------------------------------------------------------
// Snapshot types (exported for JSON serialization)
// ---------------------------------------------------------------------------

// Snapshot is the complete metrics state.
type Snapshot struct {
	UptimeSeconds int64                    `json:"uptime_seconds"`
	TotalRequests int64                    `json:"total_requests"`
	TotalCostUSD  float64                  `json:"total_cost_usd"`
	Presets       map[string]PresetSnapshot `json:"presets"`
}

// PresetSnapshot is the metrics for one preset.
type PresetSnapshot struct {
	Requests       int64                   `json:"requests"`
	Success        int64                   `json:"success"`
	Failed         int64                   `json:"failed"`
	TotalCostUSD   float64                 `json:"total_cost_usd"`
	TotalPanelCost float64                 `json:"total_panel_cost_usd"`
	TotalJudgeCost float64                 `json:"total_judge_cost_usd"`
	TotalTokens    int64                   `json:"total_tokens"`
	AvgDurationMs  float64                 `json:"avg_duration_ms"`
	P50Ms          float64                 `json:"p50_duration_ms"`
	P90Ms          float64                 `json:"p90_duration_ms"`
	P99Ms          float64                 `json:"p99_duration_ms"`
	PanelModels    map[string]ModelSnapshot `json:"panel_models,omitempty"`
}

// ModelSnapshot is the metrics for one panel model within a preset.
type ModelSnapshot struct {
	Calls         int64   `json:"calls"`
	Success       int64   `json:"success"`
	Failed        int64   `json:"failed"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
	TotalTokens   int64   `json:"total_tokens"`
	TotalCostUSD  float64 `json:"total_cost_usd"`
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (c *Collector) getOrCreatePreset(name string) *presetMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pm, ok := c.presets[name]; ok {
		return pm
	}
	pm := &presetMetrics{
		durations:    make([]float64, 0, 1000),
		durationsCap: 1000,
	}
	pm.TotalCostUSD.Store(float64(0))
	pm.TotalPanelCost.Store(float64(0))
	pm.TotalJudgeCost.Store(float64(0))
	c.presets[name] = pm
	return pm
}

func (pm *presetMetrics) getOrCreateModel(name string) *modelMetrics {
	if mmv, ok := pm.models.Load(name); ok {
		return mmv.(*modelMetrics)
	}
	mm := &modelMetrics{}
	mm.TotalCostUSD.Store(float64(0))
	mmv, _ := pm.models.LoadOrStore(name, mm)
	return mmv.(*modelMetrics)
}

func (c *Collector) computeQuantiles(pm *presetMetrics) (p50, p90, p99 float64) {
	c.mu.RLock()
	durs := make([]float64, len(pm.durations))
	copy(durs, pm.durations)
	c.mu.RUnlock()

	if len(durs) == 0 {
		return 0, 0, 0
	}

	sort.Float64s(durs)
	p50 = percentile(durs, 50)
	p90 = percentile(durs, 90)
	p99 = percentile(durs, 99)
	return
}

func avgFromSum(sum int64, count int64) float64 {
	if count == 0 {
		return 0
	}
	return float64(sum) / float64(count)
}

func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(p)/100.0*float64(len(sorted))) - 1)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// atomic.Value wrapper for float64
func addFloat(v *atomic.Value, delta float64) {
	for {
		old := v.Load().(float64)
		if v.CompareAndSwap(old, old+delta) {
			return
		}
	}
}

func loadFloat(v *atomic.Value) float64 {
	return v.Load().(float64)
}
