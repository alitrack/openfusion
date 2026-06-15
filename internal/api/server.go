// Package api implements the OpenAI-compatible HTTP API.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lhy/openfusion/internal/types"
)

// FusionEngine is the interface the API layer needs from the fusion orchestrator.
type FusionEngine interface {
	// Execute runs a fusion request against the named preset.
	Execute(presetName string, req *types.ChatRequest) (*types.ChatResponse, error)
	// ListPresets returns all available preset summaries.
	ListPresets() []PresetSummary
}

// PresetSummary is the public view of a preset.
type PresetSummary struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	Description string `json:"description,omitempty"`
}

// Server holds the HTTP server dependencies.
type Server struct {
	engine FusionEngine
	authToken string
	port     string
	mux      *http.ServeMux
}

// NewServer creates a new API server.
func NewServer(engine FusionEngine, authToken, addr string) *Server {
	s := &Server{
		engine:    engine,
		authToken: authToken,
		mux:       http.NewServeMux(),
	}

	s.registerRoutes()
	return s
}

// Handler returns the HTTP handler (for use with net/http).
func (s *Server) Handler() http.Handler {
	if s.authToken != "" {
		return s.authMiddleware(s.mux)
	}
	return s.mux
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /v1/models", s.handleListModels)
	s.mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		if token != s.authToken {
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	presets := s.engine.ListPresets()
	// Wrap in OpenAI-compatible model list format
	type modelObj struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	items := make([]modelObj, 0, len(presets))
	for _, p := range presets {
		items = append(items, modelObj{
			ID:      p.ID,
			Object:  "model",
			Created: p.Created,
			OwnedBy: "openfusion",
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   items,
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req types.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages array is empty")
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model field is required")
		return
	}

	// Handle streaming
	if req.Stream {
		s.handleStreamingCompletion(w, r, &req)
		return
	}

	resp, err := s.engine.Execute(req.Model, &req)
	if err != nil {
		log.Printf("Fusion execution error: %v", err)
		writeError(w, http.StatusInternalServerError, "fusion execution failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleStreamingCompletion runs fusion and streams the answer as SSE.
func (s *Server) handleStreamingCompletion(w http.ResponseWriter, r *http.Request, req *types.ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Run fusion synchronously (panel + judge must complete first)
	resp, err := s.engine.Execute(req.Model, req)
	if err != nil {
		fmt.Fprintf(w, "data: {\"error\":\"%s\"}\n\n", err.Error())
		flusher.Flush()
		return
	}

	answer := resp.Choices[0].Message.Content
	if answer == "" {
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// Send analysis metadata as first chunk (non-standard, useful for debugging)
	if resp.Analysis != nil {
		analysisJSON, _ := json.Marshal(map[string]interface{}{
			"type":            "analysis",
			"consensus_count":  len(resp.Analysis.Consensus),
			"contradictions":   len(resp.Analysis.Contradictions),
			"blind_spots":      len(resp.Analysis.BlindSpots),
			"unique_insights":  len(resp.Analysis.UniqueInsights),
		})
		fmt.Fprintf(w, "data: %s\n\n", analysisJSON)
		flusher.Flush()
	}

	// Stream the answer content character by character
	created := time.Now().Unix()
	for _, ch := range answer {
		select {
		case <-r.Context().Done():
			return
		default:
		}

		chunk := types.StreamChunk{
			ID:      resp.ID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   resp.Model,
			Choices: []types.StreamChoice{{
				Index: 0,
				Delta: types.StreamDelta{Content: string(ch)},
			}},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		// Small delay for natural streaming feel (skip for spaces)
		if ch != ' ' {
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Send usage as final metadata chunk
	usageJSON, _ := json.Marshal(map[string]interface{}{
		"type":            "usage",
		"prompt_tokens":   resp.Usage.PromptTokens,
		"completion_tokens": resp.Usage.CompletionTokens,
		"total_tokens":    resp.Usage.TotalTokens,
		"cost_usd":        resp.Usage.CostUSD,
	})
	fmt.Fprintf(w, "data: %s\n\n", usageJSON)

	// Send termination signal
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
