package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JSONLStore implements Store as an append-only JSONL file writer with monthly rotation.
type JSONLStore struct {
	mu        sync.Mutex
	outputDir string
	currFile  *os.File
	currMonth string // e.g. "2006-01"
}

// NewJSONLStore creates an append-only JSONL audit store.
// Writes one JSON object per line, rotated monthly.
func NewJSONLStore(outputDir string) (*JSONLStore, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("audit store: mkdir %s: %w", outputDir, err)
	}

	return &JSONLStore{
		outputDir: outputDir,
	}, nil
}

// Write appends an AuditEvent as a JSON line to the current month's file.
func (s *JSONLStore) Write(event *AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	month := time.Now().UTC().Format("2006-01")

	// Rotate if the month changed
	if s.currMonth != month {
		if s.currFile != nil {
			s.currFile.Close()
			s.currFile = nil
		}
		s.currMonth = month
	}

	if s.currFile == nil {
		filename := filepath.Join(s.outputDir, fmt.Sprintf("audit_%s.jsonl", month))
		fh, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("audit store: open %s: %w", filename, err)
		}
		s.currFile = fh
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("audit store: marshal: %w", err)
	}

	data = append(data, '\n')
	if _, err := s.currFile.Write(data); err != nil {
		return fmt.Errorf("audit store: write: %w", err)
	}

	return nil
}

// Close flushes and closes the current month's file.
func (s *JSONLStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currFile != nil {
		if err := s.currFile.Close(); err != nil {
			return err
		}
		s.currFile = nil
	}
	return nil
}
