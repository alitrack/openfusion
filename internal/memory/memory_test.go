package memory

import (
	"os"
	"strings"
	"testing"
)

func TestMemoryStore(t *testing.T) {
	dir, _ := os.MkdirTemp("", "of-mem-test-*")
	defer os.RemoveAll(dir)

	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Add typed memories
	s.Add("lhy", "npp-analytics", KindWarning, "Never USE GROUP BY on timestamp without DATE_TRUNC", "duckdb-queries", []string{"sql", "pitfall"})
	s.Add("lhy", "npp-analytics", KindPattern, "Always check data types before CAST", "duckdb-queries", []string{"sql", "type-safety"})
	s.Add("lhy", "npp-analytics", KindFact, "sales_2026Q3 has 1.2M rows", "data-catalog", []string{"dataset"})
	s.Add("lhy", "npp-analytics", KindWarning, "Mac build needs CGO_ENABLED=0", "build", []string{"macos"})
	s.Add("lhy", "npp-analytics", KindInsight, "DAG reduced token usage 40%", "architecture", []string{"atg"})

	// Add feedback BEFORE filtering (Filter requires minSamples=3)
	for i := 0; i < 6; i++ {
		s.RecordSuccess("lhy", "npp-analytics", "npp-analytics-1")
		s.RecordSuccess("lhy", "npp-analytics", "npp-analytics-2")
		s.RecordSuccess("lhy", "npp-analytics", "npp-analytics-3")
		s.RecordSuccess("lhy", "npp-analytics", "npp-analytics-4")
		s.RecordSuccess("lhy", "npp-analytics", "npp-analytics-5")
	}

	// Filter
	entries := s.Filter("lhy", "npp-analytics", 10)
	if len(entries) < 3 {
		t.Fatalf("expected >= 3 entries, got %d", len(entries))
	}

	// Filter by kind
	warnings := s.FilterByKind("lhy", "npp-analytics", KindWarning, 10)
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d", len(warnings))
	}

	// Context summary
	summary := s.ContextSummary("lhy", "npp-analytics", 5)
	if !strings.Contains(summary, "DATE_TRUNC") {
		t.Fatalf("expected DATE_TRUNC in summary: %s", summary)
	}

	// Stats
	stats := s.Stats("lhy", "npp-analytics")
	if stats["total"] != 5 {
		t.Fatalf("expected 5 total, got %d", stats["total"])
	}

	// Multi-tenant isolation
	summaryOther := s.ContextSummary("other", "other", 5)
	if summaryOther != "" {
		t.Fatalf("expected empty for other tenant: %s", summaryOther)
	}

	// Forget
	if len(entries) > 0 {
		s.Forget("lhy", "npp-analytics", entries[0].ID)
		entries2 := s.Filter("lhy", "npp-analytics", 10)
		for _, e := range entries2 {
			if e.ID == entries[0].ID {
				t.Fatal("expected forgotten entry to be gone")
			}
		}
	}

	t.Logf("✅ memory store test PASSED")
}
