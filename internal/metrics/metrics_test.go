package metrics

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector()
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	s := c.Snapshot()
	if s.UptimeSeconds < 0 {
		t.Fatal("negative uptime")
	}
	if len(s.Presets) != 0 {
		t.Fatal("expected empty presets")
	}
}

func TestRecordRequest(t *testing.T) {
	c := NewCollector()
	c.RecordRequest("budget")
	s := c.Snapshot()
	if s.TotalRequests != 1 {
		t.Fatalf("expected 1 request, got %d", s.TotalRequests)
	}
	ps, ok := s.Presets["budget"]
	if !ok {
		t.Fatal("expected budget preset")
	}
	if ps.Requests != 1 {
		t.Fatalf("expected 1 preset request, got %d", ps.Requests)
	}
}

func TestRecordPanelCall(t *testing.T) {
	c := NewCollector()
	c.RecordPanelCall("budget", "deepseek/V4-Pro", 2*time.Second, 150, 0.0015, true)
	c.RecordPanelCall("budget", "qwen/3.5-27B", 3*time.Second, 200, 0.0010, true)
	c.RecordPanelCall("budget", "deepseek/V4-Pro", 1*time.Second, 100, 0.0010, false)

	s := c.Snapshot()
	ps := s.Presets["budget"]
	if ps.TotalPanelCost == 0 {
		t.Fatal("expected panel cost > 0")
	}

	// Check model breakdown
	deepseekMs := ps.PanelModels["deepseek/V4-Pro"]
	if deepseekMs.Calls != 2 {
		t.Fatalf("expected 2 calls, got %d", deepseekMs.Calls)
	}
	if deepseekMs.Success != 1 {
		t.Fatalf("expected 1 success, got %d", deepseekMs.Success)
	}
	if deepseekMs.Failed != 1 {
		t.Fatalf("expected 1 failure, got %d", deepseekMs.Failed)
	}
	if deepseekMs.TotalCostUSD < 0.002 {
		t.Fatal("expected cost > 0")
	}
}

func TestRecordFusionComplete(t *testing.T) {
	c := NewCollector()
	c.RecordRequest("budget")
	c.RecordPanelCall("budget", "m1", 1*time.Second, 100, 0.001, true)
	c.RecordFusionComplete("budget", 5*time.Second, true)

	s := c.Snapshot()
	ps := s.Presets["budget"]
	if ps.Success != 1 {
		t.Fatalf("expected 1 success, got %d", ps.Success)
	}
	if ps.AvgDurationMs < 4000 || ps.AvgDurationMs > 6000 {
		t.Fatalf("avg duration ~5s, got %f", ps.AvgDurationMs)
	}
}

func TestRecordJudgeCall(t *testing.T) {
	c := NewCollector()
	c.RecordJudgeCall("budget", 4*time.Second, 500, 0.005)

	s := c.Snapshot()
	ps := s.Presets["budget"]
	if ps.TotalJudgeCost < 0.004 {
		t.Fatal("expected judge cost > 0")
	}
	if ps.TotalTokens < 500 {
		t.Fatal("expected tokens > 500")
	}
}

func TestQuantiles(t *testing.T) {
	c := NewCollector()
	for i := 0; i < 100; i++ {
		d := time.Duration(1000+i*100) * time.Millisecond
		c.RecordJudgeCall("budget", d, 100, 0.001)
	}

	s := c.Snapshot()
	ps := s.Presets["budget"]
	if ps.P50Ms < 5000 || ps.P50Ms > 6500 {
		t.Fatalf("p50 ~5.95s, got %f", ps.P50Ms)
	}
	if ps.P90Ms < 9000 || ps.P90Ms > 11000 {
		t.Fatalf("p90 ~10s, got %f", ps.P90Ms)
	}
}

func TestJSONSerialization(t *testing.T) {
	c := NewCollector()
	c.RecordRequest("budget")
	c.RecordPanelCall("budget", "m1", 2*time.Second, 150, 0.0015, true)
	c.RecordJudgeCall("budget", 4*time.Second, 500, 0.005)
	c.RecordFusionComplete("budget", 6*time.Second, true)

	s := c.Snapshot()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty json")
	}

	// Verify it can be unmarshalled back
	var restored Snapshot
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if restored.TotalRequests != 1 {
		t.Fatal("round-trip lost data")
	}
}

func TestConcurrentSafety(t *testing.T) {
	c := NewCollector()
	done := make(chan struct{})

	// Concurrent writers
	go func() {
		for i := 0; i < 100; i++ {
			c.RecordRequest("budget")
			c.RecordPanelCall("budget", "m1", 1*time.Second, 100, 0.001, true)
			c.RecordFusionComplete("budget", 2*time.Second, true)
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			c.RecordRequest("frontier")
			c.RecordPanelCall("frontier", "m2", 2*time.Second, 200, 0.002, true)
			c.RecordFusionComplete("frontier", 3*time.Second, true)
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 50; i++ {
			_ = c.Snapshot()
		}
		done <- struct{}{}
	}()

	<-done
	<-done
	<-done

	s := c.Snapshot()
	if s.Presets["budget"].Requests != 100 {
		t.Fatalf("expected 100 budget requests, got %d", s.Presets["budget"].Requests)
	}
	if s.Presets["frontier"].Requests != 100 {
		t.Fatalf("expected 100 frontier requests, got %d", s.Presets["frontier"].Requests)
	}
}
