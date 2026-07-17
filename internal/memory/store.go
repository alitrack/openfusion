// Package memory provides a multi-tenant structured memory store.
// Inspired by Raven's MemCell model and forge's lesson store.
// Uses JSON files for zero-dependency persistence (same as forge).
//
// Memory types map to Raven's taxonomy:
//
//	fact    → EventLog: atomic fact learned from a conversation
//	pattern → proven coding/analysis approaches
//	warning → pitfalls, gotchas, environment quirks (Foresight equivalent)
//	insight → strategic knowledge, architecture decisions (Episode equivalent)
package memory

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind classifies the type of memory.
type Kind string

const (
	KindFact    Kind = "fact"
	KindPattern Kind = "pattern"
	KindWarning Kind = "warning"
	KindInsight Kind = "insight"
)

// Entry is a stored memory.
type Entry struct {
	ID          string    `json:"id"`
	Kind        Kind      `json:"kind"`
	Content     string    `json:"content"`
	Context     string    `json:"context"`
	Tags        []string  `json:"tags,omitempty"`
	UserID      string    `json:"user_id"`
	ProjectID   string    `json:"project_id"`
	Successes   int       `json:"successes"`
	Failures    int       `json:"failures"`
	LastUsed    time.Time `json:"last_used"`
	CreatedAt   time.Time `json:"created_at"`
	Reliability float64   `json:"reliability"`
}

// Store provides persistent, multi-tenant memory storage.
type Store struct {
	mu         sync.RWMutex
	dir        string
	minSamples int
	minRel     float64
	halfLife   float64 // days
	maxEntries int
}

const DefaultHalfLifeDays = 7.0
const DefaultMaxEntries = 1000

// NewStore creates a memory store at the given directory.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("memory store: %w", err)
	}
	return &Store{
		dir:        dir,
		minSamples: 3,
		minRel:     0.5,
		halfLife:   DefaultHalfLifeDays,
		maxEntries: DefaultMaxEntries,
	}, nil
}

// tenantPath returns the JSON file path for a tenant.
func (s *Store) tenantPath(userID, projectID string) string {
	safe := strings.NewReplacer("/", "_", "\\", "_", "..", "_")
	return filepath.Join(s.dir, safe.Replace(userID)+"+"+safe.Replace(projectID)+".json")
}

// load reads entries for a tenant from disk.
func (s *Store) load(userID, projectID string) (map[string]*Entry, error) {
	path := s.tenantPath(userID, projectID)
	data, err := os.ReadFile(path)
	if err != nil {
		return make(map[string]*Entry), nil // no memories yet
	}
	entries := make(map[string]*Entry)
	if err := json.Unmarshal(data, &entries); err != nil {
		return make(map[string]*Entry), nil
	}
	return entries, nil
}

// save writes entries for a tenant to disk.
func (s *Store) save(userID, projectID string, entries map[string]*Entry) error {
	data, _ := json.MarshalIndent(entries, "", "  ")
	return os.WriteFile(s.tenantPath(userID, projectID), data, 0o600)
}

// Add inserts a new memory entry.
func (s *Store) Add(userID, projectID string, kind Kind, content, context string, tags []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.load(userID, projectID)
	if err != nil {
		return "", err
	}

	id := fmt.Sprintf("%s-%d", projectID, len(entries)+1)
	entries[id] = &Entry{
		ID:          id,
		Kind:        kind,
		Content:     content,
		Context:     context,
		Tags:        tags,
		UserID:      userID,
		ProjectID:   projectID,
		CreatedAt:   time.Now(),
		LastUsed:    time.Now(),
		Reliability: 0.5, // default: unknown quality
	}

	s.autoPrune(entries)
	return id, s.save(userID, projectID, entries)
}

// RecordSuccess increments the success counter.
func (s *Store) RecordSuccess(userID, projectID, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, _ := s.load(userID, projectID)
	if e, ok := entries[id]; ok {
		e.Successes++
		e.LastUsed = time.Now()
		total := e.Successes + e.Failures
		if total > 0 {
			e.Reliability = float64(e.Successes) / float64(total)
		}
		s.save(userID, projectID, entries)
	}
}

// RecordFailure increments the failure counter.
func (s *Store) RecordFailure(userID, projectID, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, _ := s.load(userID, projectID)
	if e, ok := entries[id]; ok {
		e.Failures++
		e.LastUsed = time.Now()
		total := e.Successes + e.Failures
		if total > 0 {
			e.Reliability = float64(e.Successes) / float64(total)
		}
		s.save(userID, projectID, entries)
	}
}

// Filter returns top-N reliable memories for a tenant.
func (s *Store) Filter(userID, projectID string, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, _ := s.load(userID, projectID)
	return s.rankedFilter(entries, limit)
}

// FilterByKind returns top memories of a specific kind.
func (s *Store) FilterByKind(userID, projectID string, kind Kind, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, _ := s.load(userID, projectID)
	filtered := make(map[string]*Entry)
	for id, e := range entries {
		if e.Kind == kind {
			filtered[id] = e
		}
	}
	return s.rankedFilter(filtered, limit)
}

// ContextSummary builds a compact context injection string.
func (s *Store) ContextSummary(userID, projectID string, maxPerKind int) string {
	if maxPerKind <= 0 {
		maxPerKind = 5
	}
	var parts []string

	warnings := s.FilterByKind(userID, projectID, KindWarning, maxPerKind)
	if len(warnings) > 0 {
		parts = append(parts, "### Warnings (pitfalls to avoid)")
		for _, w := range warnings {
			parts = append(parts, "- "+w.Content)
		}
	}

	patterns := s.FilterByKind(userID, projectID, KindPattern, maxPerKind/2)
	if len(patterns) > 0 {
		parts = append(parts, "### Patterns (proven approaches)")
		for _, p := range patterns {
			parts = append(parts, "- "+p.Content)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return "\n" + strings.Join(parts, "\n") + "\n"
}

// Stats returns counts per kind for a tenant.
func (s *Store) Stats(userID, projectID string) map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, _ := s.load(userID, projectID)
	stats := map[string]int{"total": len(entries), "fact": 0, "pattern": 0, "warning": 0, "insight": 0}
	for _, e := range entries {
		stats[string(e.Kind)]++
	}
	return stats
}

// Forget removes a memory.
func (s *Store) Forget(userID, projectID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load(userID, projectID)
	if err != nil {
		return err
	}
	delete(entries, id)
	return s.save(userID, projectID, entries)
}

// rankedFilter applies Thompson Sampling + time decay and returns top entries.
func (s *Store) rankedFilter(entries map[string]*Entry, limit int) []Entry {
	if limit <= 0 {
		limit = 10
	}
	now := time.Now()
	type scored struct {
		entry *Entry
		score float64
	}
	var candidates []scored
	for _, e := range entries {
		total := e.Successes + e.Failures
		if total < s.minSamples || e.Reliability < s.minRel {
			continue
		}
		ageHours := now.Sub(e.LastUsed).Hours()
		if ageHours < 0 {
			ageHours = 0
		}
		decayWeight := math.Pow(0.5, ageHours/(s.halfLife*24))
		decayedS := int(math.Round(float64(e.Successes) * decayWeight))
		decayedF := int(math.Round(float64(e.Failures) * decayWeight))
		if decayedS+decayedF < 1 {
			continue
		}
		score := thompsonSample(decayedS+1, decayedF+1)
		if score >= s.minRel {
			candidates = append(candidates, scored{entry: e, score: score})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	result := make([]Entry, 0, limit)
	for i, c := range candidates {
		if i >= limit {
			break
		}
		cp := *c.entry
		cp.Reliability = c.score
		result = append(result, cp)
	}
	return result
}

// autoPrune removes lowest-reliability entries when exceeding maxEntries.
func (s *Store) autoPrune(entries map[string]*Entry) {
	if len(entries) <= s.maxEntries {
		return
	}
	type scored struct {
		id    string
		score float64
	}
	var list []scored
	now := time.Now()
	for id, e := range entries {
		ageHours := now.Sub(e.LastUsed).Hours()
		if ageHours < 0 {
			ageHours = 0
		}
		decayWeight := math.Pow(0.5, ageHours/(s.halfLife*24))
		score := e.Reliability * decayWeight
		list = append(list, scored{id, score})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].score < list[j].score
	})
	removeCount := len(entries) - s.maxEntries
	for i := 0; i < removeCount && i < len(list); i++ {
		delete(entries, list[i].id)
	}
}

// thompsonSample draws from Beta(alpha, beta).
func thompsonSample(successes, failures int) float64 {
	alpha := float64(successes)
	betaVal := float64(failures)
	x := gammaRand(alpha)
	y := gammaRand(betaVal)
	if x+y == 0 {
		return 0.5
	}
	return x / (x + y)
}

func gammaRand(shape float64) float64 {
	if shape < 1 {
		return gammaRand(shape+1) * math.Pow(rand.Float64(), 1.0/shape)
	}
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)
	for {
		x := rand.NormFloat64()
		v := (1 + c*x) * (1 + c*x) * (1 + c*x)
		if v <= 0 {
			continue
		}
		u := rand.Float64()
		if u < 1-0.0331*(x*x)*(x*x) {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}
