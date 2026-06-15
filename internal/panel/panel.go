// Package panel handles parallel dispatch to multiple models.
package panel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

// HealthChecker is an optional interface for provider health checking.
type HealthChecker interface {
	IsHealthy(name string) bool
}

// Dispatcher dispatches a chat request to all panel members in parallel.
type Dispatcher struct {
	providerManager *provider.Manager
	timeout         time.Duration
	healthChecker   HealthChecker
}

// NewDispatcher creates a panel dispatcher.
func NewDispatcher(pm *provider.Manager, timeout time.Duration, hc HealthChecker) *Dispatcher {
	return &Dispatcher{
		providerManager: pm,
		timeout:         timeout,
		healthChecker:   hc,
	}
}

// Dispatch sends the request to all panel members concurrently and collects responses.
// Failed or timed-out members are marked but don't abort the whole panel.
func (d *Dispatcher) Dispatch(ctx context.Context, preset *types.Preset, req *types.ChatRequest) []types.PanelResponse {
	if len(preset.Panel) == 0 {
		return nil
	}

	results := make([]types.PanelResponse, len(preset.Panel))
	var wg sync.WaitGroup

	for i, member := range preset.Panel {
		wg.Add(1)
		go func(idx int, m types.PanelMember) {
			defer wg.Done()
			results[idx] = d.callMember(ctx, m, req)
		}(i, member)
	}

	wg.Wait()
	return results
}

// callMember sends the request to a single panel member with timeout tracking.
func (d *Dispatcher) callMember(ctx context.Context, member types.PanelMember, req *types.ChatRequest) types.PanelResponse {
	start := time.Now()

	// Health check: skip unhealthy providers
	if d.healthChecker != nil && !d.healthChecker.IsHealthy(member.Provider) {
		return types.PanelResponse{
			Member:   member,
			Error:    "provider unhealthy (health check)",
			Duration: time.Since(start),
		}
	}

	p, err := d.providerManager.Get(member.Provider)
	if err != nil {
		return types.PanelResponse{
			Member:   member,
			Error:    fmt.Sprintf("provider error: %v", err),
			Duration: time.Since(start),
		}
	}

	// Build the request for this panel member
	panelReq := &types.ChatRequest{
		Model:       member.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}

	// Prepend system message if configured
	if member.System != "" {
		panelReq.Messages = append([]types.ChatMessage{
			{Role: "system", Content: member.System},
		}, req.Messages...)
	} else {
		panelReq.Messages = req.Messages
	}

	// Create per-member context with timeout
	memberCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	resp, err := p.ChatCompletion(memberCtx, panelReq)
	duration := time.Since(start)

	if err != nil {
		if memberCtx.Err() == context.DeadlineExceeded {
			return types.PanelResponse{
				Member:   member,
				TimedOut: true,
				Error:    fmt.Sprintf("timeout after %v", d.timeout),
				Duration: duration,
			}
		}
		return types.PanelResponse{
			Member:   member,
			Error:    err.Error(),
			Duration: duration,
		}
	}

	content := ""
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}

	return types.PanelResponse{
		Member:   member,
		Content:  content,
		Usage:    resp.Usage,
		Duration: duration,
	}
}
