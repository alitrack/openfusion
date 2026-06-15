// Package api implements the OpenAI-compatible HTTP API.
package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lhy/openfusion/internal/logger"
	"github.com/lhy/openfusion/internal/ratelimit"
	"github.com/lhy/openfusion/internal/types"
)

//go:embed dashboard.html
var dashboardHTML string

// FusionEngine is the interface the API layer needs from the fusion orchestrator.
type FusionEngine interface {
	// Execute runs a fusion request against the named preset.
	Execute(presetName string, req *types.ChatRequest) (*types.ChatResponse, error)
	// ExecuteAuto uses skill matching to automatically route the request.
	ExecuteAuto(req *types.ChatRequest) (*types.ChatResponse, error)
	// ListPresets returns all available preset summaries.
	ListPresets() []PresetSummary
	// Metrics returns the metrics collector for snapshot retrieval.
	Metrics() interface{} // returns *metrics.Collector or nil
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
	engine      FusionEngine
	authToken   string
	port        string
	mux         *http.ServeMux
	rateLimiter *ratelimit.Limiter
	log         *logger.Logger
}

// NewServer creates a new API server.
func NewServer(engine FusionEngine, authToken, addr string, rl *ratelimit.Limiter) *Server {
	s := &Server{
		engine:      engine,
		authToken:   authToken,
		port:        addr,
		mux:         http.NewServeMux(),
		rateLimiter: rl,
		log:         logger.New(nil).NewModule("api"),
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
	s.mux.HandleFunc("GET /v1/metrics", s.handleMetrics)
	s.mux.HandleFunc("GET /v1/dashboard", s.handleDashboard)
	s.mux.HandleFunc("GET /", s.handleDashboard)
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

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	metricsObj := s.engine.Metrics()
	if metricsObj == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"uptime_seconds":  0,
			"total_requests":  0,
			"total_cost_usd":  0,
			"presets":         map[string]interface{}{},
		})
		return
	}
	writeJSON(w, http.StatusOK, metricsObj)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(dashboardHTML))
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

	// Rate limit check
	if s.rateLimiter != nil && s.rateLimiter.Enabled() {
		allowed, retryAfter := s.rateLimiter.Allow(req.Model)
		if !allowed {
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"error":              fmt.Sprintf("rate limit exceeded for preset '%s'", req.Model),
				"retry_after_seconds": retryAfter.Seconds(),
			})
			return
		}
	}

	// Auto-route via skill matching when model is auto/empty
	model := req.Model
	if model == "" || model == "auto" || model == "openfusion/auto" {
		resp, err := s.engine.ExecuteAuto(&req)
		if err != nil {
			s.log.Warn("auto-route failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "auto-route failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Handle streaming
	if req.Stream {
		s.handleStreamingCompletion(w, r, &req)
		return
	}

	resp, err := s.engine.Execute(req.Model, &req)
	if err != nil {
		s.log.Warn("fusion execution failed", "preset", model, "error", err.Error())
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
