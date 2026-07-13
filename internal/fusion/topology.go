package fusion

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

// ----------------------------------------
// Topology types for multi-layer fusion
// ----------------------------------------

// TopologyDef defines a multi-layer fusion DAG (used in config/presets).
type TopologyDef struct {
	Layers []LayerDef `yaml:"layers" json:"layers"`
}

// LayerDef defines one stage in the fusion pipeline.
type LayerDef struct {
	Name   string     `yaml:"name" json:"name"`
	Models []ModelRef `yaml:"models" json:"models"`
	Role   string     `yaml:"role,omitempty" json:"role,omitempty"` // "panel" or "judge"
}

// ModelRef identifies a specific model + provider combination.
type ModelRef struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
	Role     string `yaml:"role,omitempty" json:"role,omitempty"`
}

// LayerOutput holds the collected outputs from one topology layer.
type LayerOutput struct {
	LayerName string
	Outputs   []PanelResult
}

// PanelResult is the output of a single model call within a layer.
type PanelResult struct {
	ModelRef ModelRef
	Content  string
	Usage    types.Usage
	Error    string
	Duration time.Duration
}

// Validate checks topology structural integrity.
func (t *TopologyDef) Validate() error {
	if len(t.Layers) == 0 {
		return fmt.Errorf("topology: at least one layer required")
	}

	judgeCount := 0
	for i, layer := range t.Layers {
		if len(layer.Models) == 0 {
			return fmt.Errorf("topology: layer %q has no models", layer.Name)
		}
		for _, m := range layer.Models {
			if m.Provider == "" || m.Model == "" {
				return fmt.Errorf("topology: layer %q has model with empty provider/model", layer.Name)
			}
		}
		if layer.Role == "judge" {
			judgeCount++
			if i != len(t.Layers)-1 {
				return fmt.Errorf("topology: judge layer %q must be the last layer", layer.Name)
			}
		}
	}
	if judgeCount != 1 {
		return fmt.Errorf("topology: exactly 1 judge layer required, got %d", judgeCount)
	}
	return nil
}

// ----------------------------------------
// Multi-layer topology execution
// ----------------------------------------

// ExecuteTopology runs a multi-layer fusion topology using the engine's provider manager.
func (e *Engine) ExecuteTopology(ctx context.Context, topo *TopologyDef, presetName string, req *types.ChatRequest) (*types.ChatResponse, error) {
	if topo == nil {
		return nil, fmt.Errorf("topology is nil")
	}
	if err := topo.Validate(); err != nil {
		return nil, fmt.Errorf("invalid topology: %w", err)
	}

	// Apply optimizer if configured (strip context + tail append)
	messages := req.Messages

	ctx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	pm := e.getProviderManager()

	// Execute layers sequentially (within each layer, models run in parallel)
	var allOutputs []LayerOutput
	for i, layer := range topo.Layers {
		outputs := executeLayer(ctx, pm, layer, messages, allOutputs)
		allOutputs = append(allOutputs, outputs)

		if i == len(topo.Layers)-1 {
			// Last layer → extract judge output
			return extractFinalResponse(presetName, topo, allOutputs)
		}
	}

	return nil, fmt.Errorf("fusion: unreachable (no judge layer)")
}

// executeLayer dispatches all models in a layer in parallel.
func executeLayer(
	ctx context.Context,
	pm *provider.Manager,
	layer LayerDef,
	messages []types.ChatMessage,
	previousOutputs []LayerOutput,
) LayerOutput {
	output := LayerOutput{LayerName: layer.Name}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, model := range layer.Models {
		wg.Add(1)
		go func(m ModelRef) {
			defer wg.Done()
			modelMessages := buildLayerContext(messages, previousOutputs)

			result := PanelResult{ModelRef: m}
			start := time.Now()

			prov, err := pm.Get(m.Provider)
			if err != nil {
				result.Error = err.Error()
				result.Duration = time.Since(start)
				mu.Lock()
				output.Outputs = append(output.Outputs, result)
				mu.Unlock()
				return
			}

			req := &types.ChatRequest{
				Model:    m.Model,
				Messages: modelMessages,
			}

			resp, err := prov.ChatCompletion(ctx, req)
			result.Duration = time.Since(start)
			if err != nil {
				result.Error = err.Error()
			} else if len(resp.Choices) > 0 {
				result.Content = resp.Choices[0].Message.Content
				result.Usage = resp.Usage
			}

			mu.Lock()
			output.Outputs = append(output.Outputs, result)
			mu.Unlock()
		}(model)
	}

	wg.Wait()
	return output
}

// buildLayerContext appends previous layer outputs to the last user message.
func buildLayerContext(
	messages []types.ChatMessage,
	previousOutputs []LayerOutput,
) []types.ChatMessage {
	if len(previousOutputs) == 0 {
		return messages
	}

	out := make([]types.ChatMessage, len(messages))
	copy(out, messages)

	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == "user" {
			var sb strings.Builder
			sb.WriteString(out[i].Content)
			for _, lo := range previousOutputs {
				sb.WriteString("\n\n---\n[Layer: ")
				sb.WriteString(lo.LayerName)
				sb.WriteString("]\n")
				for _, pr := range lo.Outputs {
					if pr.Error != "" {
						sb.WriteString(fmt.Sprintf("\n[%s: error - %s]", pr.ModelRef.Model, pr.Error))
					} else {
						sb.WriteString(fmt.Sprintf("\n[%s]: %s", pr.ModelRef.Model, pr.Content))
					}
				}
			}
			out[i].Content = sb.String()
			break
		}
	}

	return out
}

// extractFinalResponse builds the ChatResponse from the last layer's judge output.
func extractFinalResponse(
	presetName string,
	topo *TopologyDef,
	allOutputs []LayerOutput,
) (*types.ChatResponse, error) {
	if len(allOutputs) == 0 {
		return nil, fmt.Errorf("fusion: no layer outputs")
	}

	lastLayer := allOutputs[len(allOutputs)-1]
	var summaries []types.PanelResponseSummary
	var totalUsage types.Usage

	for _, lo := range allOutputs {
		for _, pr := range lo.Outputs {
			summary := types.PanelResponseSummary{
				Model:            pr.ModelRef.Model,
				Content:          pr.Content,
				DurationMs:       pr.Duration.Milliseconds(),
				PromptTokens:     pr.Usage.PromptTokens,
				CompletionTokens: pr.Usage.CompletionTokens,
				TotalTokens:      pr.Usage.TotalTokens,
				Error:            pr.Error,
			}
			summaries = append(summaries, summary)
			totalUsage.PromptTokens += pr.Usage.PromptTokens
			totalUsage.CompletionTokens += pr.Usage.CompletionTokens
			totalUsage.TotalTokens += pr.Usage.TotalTokens
		}
	}

	var finalContent string
	for _, pr := range lastLayer.Outputs {
		if pr.Error != "" {
			continue
		}
		finalContent = pr.Content
		totalUsage.PromptTokens += pr.Usage.PromptTokens
		totalUsage.CompletionTokens += pr.Usage.CompletionTokens
		totalUsage.TotalTokens += pr.Usage.TotalTokens
		break
	}

	if finalContent == "" {
		var errs []string
		for _, pr := range lastLayer.Outputs {
			if pr.Error != "" {
				errs = append(errs, pr.Error)
			}
		}
		return nil, fmt.Errorf("fusion: all judge models failed: %s", strings.Join(errs, "; "))
	}

	return &types.ChatResponse{
		ID:      fmt.Sprintf("fusion-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Model:   presetName,
		Choices: []types.Choice{{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: finalContent}}},
		Usage:   totalUsage,
		PanelResponses: summaries,
	}, nil
}
