// Package guard provides a pluggable content safety middleware chain.
package guard

import (
	"context"

	"github.com/lhy/openfusion/internal/types"
)

// Action defines the guard response action.
type Action string

const (
	ActionBlock  Action = "block"
	ActionWarn   Action = "warn"
	ActionRedact Action = "redact"
	ActionLog    Action = "log"
)

// GuardResult holds the result of a guard check.
type GuardResult struct {
	Allowed   bool    `json:"allowed"`
	Score     float64 `json:"score"`
	Reason    string  `json:"reason,omitempty"`
	Action    Action  `json:"action"`
	GuardName string  `json:"guard_name,omitempty"`
}

// Guard is the interface that all guard implementations must satisfy.
type Guard interface {
	// Name returns a human-readable name for this guard.
	Name() string

	// CheckInput inspects an incoming ChatRequest before processing.
	// Return nil GuardResult to indicate "pass / no action".
	CheckInput(ctx context.Context, req *types.ChatRequest) (*GuardResult, error)

	// CheckOutput inspects a ChatResponse before returning to the client.
	// Return nil GuardResult to indicate "pass / no action".
	CheckOutput(ctx context.Context, resp *types.ChatResponse) (*GuardResult, error)
}

// Pass is a convenience guard result that allows the request/response through.
func Pass() *GuardResult {
	return &GuardResult{Allowed: true, Action: ActionLog}
}

// Block creates a blocking guard result.
func Block(reason string, score float64) *GuardResult {
	return &GuardResult{
		Allowed: false,
		Score:   score,
		Reason:  reason,
		Action:  ActionBlock,
	}
}

// Warn creates a warning guard result.
func Warn(reason string, score float64) *GuardResult {
	return &GuardResult{
		Allowed: true,
		Score:   score,
		Reason:  reason,
		Action:  ActionWarn,
	}
}
