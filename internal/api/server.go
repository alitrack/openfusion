// Package api implements the OpenAI-compatible HTTP API.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

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

	resp, err := s.engine.Execute(req.Model, &req)
	if err != nil {
		log.Printf("Fusion execution error: %v", err)
		writeError(w, http.StatusInternalServerError, "fusion execution failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
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
