// Package audit provides a structured, hash-chained audit trail for tamper evidence.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// EventType categorizes audit events.
type EventType string

const (
	EventFusionRequest  EventType = "fusion.request"
	EventFusionResponse EventType = "fusion.response"
	EventGuardBlock     EventType = "guard.block"
	EventGuardWarn      EventType = "guard.warn"
	EventPolicyDeny     EventType = "policy.deny"
	EventPolicyWarn     EventType = "policy.warn"
	EventPolicyEscalate EventType = "policy.escalate"
	EventCostUpdate     EventType = "cost.update"
	EventSystemError    EventType = "system.error"
)

// AuditEvent is a structured audit trail entry.
type AuditEvent struct {
	ID          string                 `json:"id"`
	Timestamp   string                 `json:"timestamp"`
	EventType   EventType              `json:"event_type"`
	Preset      string                 `json:"preset,omitempty"`
	FusionID    string                 `json:"fusion_id,omitempty"`
	UserID      string                 `json:"user_id,omitempty"`
	ProjectID   string                 `json:"project_id,omitempty"`
	Details     map[string]interface{} `json:"details,omitempty"`
	PrevHash    string                 `json:"prev_hash"`
	Hash        string                 `json:"hash"`
}

// EventLogger writes structured, hash-chained audit events.
type EventLogger struct {
	mu       sync.Mutex
	prevHash string
	store    Store
	seq      int64
}

// Store is the interface for persisting audit events.
type Store interface {
	Write(event *AuditEvent) error
	Close() error
}

// NewEventLogger creates a new event logger with the given persistence store.
func NewEventLogger(store Store) *EventLogger {
	return &EventLogger{
		store:    store,
		prevHash: "0000000000000000000000000000000000000000000000000000000000000000", // genesis hash
	}
}

// Log creates and persists a new audit event with hash chaining.
func (l *EventLogger) Log(eventType EventType, details map[string]interface{}, preset, fusionID, userID, projectID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.seq++
	now := time.Now().UTC()

	event := &AuditEvent{
		ID:        fmt.Sprintf("audit_%d_%d", now.UnixNano(), l.seq),
		Timestamp: now.Format(time.RFC3339Nano),
		EventType: eventType,
		Preset:    preset,
		FusionID:  fusionID,
		UserID:    userID,
		ProjectID: projectID,
		Details:   details,
		PrevHash:  l.prevHash,
	}

	event.Hash = l.computeHash(event)

	if l.store != nil {
		if err := l.store.Write(event); err != nil {
			fmt.Printf("audit: write error: %v\n", err)
		}
	}

	l.prevHash = event.Hash
}

// computeHash computes SHA-256 over the event fields (excluding the Hash field itself).
func (l *EventLogger) computeHash(event *AuditEvent) string {
	h := sha256.New()

	// Order matters for deterministic hashing
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%v|%s",
		event.ID,
		event.Timestamp,
		event.EventType,
		event.Preset,
		event.FusionID,
		event.UserID,
		event.ProjectID,
		event.Details,
		event.PrevHash,
	)

	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyChain validates the entire hash chain from a sequence of events.
// Returns the index of the first invalid event, or -1 if all valid.
func VerifyChain(events []*AuditEvent) int {
	if len(events) == 0 {
		return -1
	}

	prevHash := "0000000000000000000000000000000000000000000000000000000000000000"

	for i, event := range events {
		if event.PrevHash != prevHash {
			return i
		}

		expectedHash := computeEventHash(event)
		if event.Hash != expectedHash {
			return i
		}

		prevHash = event.Hash
	}

	return -1
}

// computeEventHash is a package-level helper used by VerifyChain.
func computeEventHash(event *AuditEvent) string {
	h := sha256.New()
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%v|%s",
		event.ID,
		event.Timestamp,
		event.EventType,
		event.Preset,
		event.FusionID,
		event.UserID,
		event.ProjectID,
		event.Details,
		event.PrevHash,
	)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// PrevHash returns the current chain tip hash.
func (l *EventLogger) PrevHash() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.prevHash
}
