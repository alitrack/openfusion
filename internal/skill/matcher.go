package skill

import (
	"sort"
)

// ---------------------------------------------------------------------------
// Matcher
// ---------------------------------------------------------------------------

// Matcher matches requests to skills based on request features.
type Matcher struct {
	skills     []*Skill // ordered by priority desc
	defaultRef string   // fallback skill name or "direct"
}

// NewMatcher creates a matcher from an ordered skill list.
func NewMatcher(skills []*Skill, defaultRef string) *Matcher {
	// Sort by priority descending
	sorted := make([]*Skill, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	return &Matcher{
		skills:     sorted,
		defaultRef: defaultRef,
	}
}

// Match finds the best skill for the given request features.
// Returns nil if no skill matches (caller should use default).
func (m *Matcher) Match(f *RequestFeatures) *Skill {
	for _, s := range m.skills {
		if s.Matches(f) {
			return s
		}
	}
	return nil
}

// Skills returns all registered skills (sorted by priority).
func (m *Matcher) Skills() []*Skill {
	return m.skills
}

// DefaultRef returns the configured default reference.
func (m *Matcher) DefaultRef() string {
	return m.defaultRef
}

// ---------------------------------------------------------------------------
// Feature helpers
// ---------------------------------------------------------------------------

// containsAny checks if the slice contains any of the given items.
func containsAny(slice []string, items []string) bool {
	for _, s := range slice {
		for _, item := range items {
			if s == item {
				return true
			}
		}
	}
	return false
}

// clamp restricts a value to [lo, hi].
func clamp(val, lo, hi int) int {
	if val < lo {
		return lo
	}
	if val > hi {
		return hi
	}
	return val
}
