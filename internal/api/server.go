// Package api implements the OpenAI-compatible HTTP API.
package api

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lhy/openfusion/internal/logger"
	"github.com/lhy/openfusion/internal/logging"
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
	Metrics() any // returns *metrics.Collector or nil
	// CreatePreset adds a new preset at runtime.
	CreatePreset(name string, preset types.Preset) error
	// DeletePreset removes a preset by name.
	DeletePreset(name string) error
	// GetPreset returns the full preset detail by name.
	GetPreset(name string) (*types.Preset, error)
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
	mux         *http.ServeMux
	rateLimiter *ratelimit.Limiter
	log         *logger.Logger
	hook        *logging.Hook
}

// NewServer creates a new API server.
func NewServer(engine FusionEngine, authToken, addr string, rl *ratelimit.Limiter, hook *logging.Hook) *Server {
	s := &Server{
		engine:      engine,
		authToken:   authToken,
		mux:         http.NewServeMux(),
		rateLimiter: rl,
		log:         logger.New(nil).NewModule("api"),
		hook:        hook,
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
	s.mux.HandleFunc("GET /v1/presets", s.handleListPresetsDetail)
	s.mux.HandleFunc("POST /v1/presets", s.handleCreatePreset)
	s.mux.HandleFunc("GET /v1/presets/{name}", s.handleGetPreset)
	s.mux.HandleFunc("DELETE /v1/presets/{name}", s.handleDeletePreset)
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
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
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
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   items,
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	metricsObj := s.engine.Metrics()
	if metricsObj == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"uptime_seconds":  0,
			"total_requests":  0,
			"total_cost_usd":  0,
			"presets":         map[string]any{},
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

// ---------------------------------------------------------------------------
// Preset CRUD handlers
// ---------------------------------------------------------------------------

func (s *Server) handleListPresetsDetail(w http.ResponseWriter, r *http.Request) {
	presets := s.engine.ListPresets()
	// Also get full detail for each where possible
	items := make([]map[string]any, 0, len(presets))
	for _, p := range presets {
		item := map[string]any{
			"id":          p.ID,
			"object":       p.Object,
			"created":      p.Created,
			"owned_by":     p.OwnedBy,
			"description":  p.Description,
		}
		// Try to get full detail
		name := strings.TrimPrefix(p.ID, "openfusion/")
		if detail, err := s.engine.GetPreset(name); err == nil && detail != nil {
			item["panel"] = detail.Panel
			item["judge"] = detail.Judge
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"total":  len(items),
		"data":   items,
	})
}

type createPresetRequest struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Panel       []types.PanelMember `json:"panel"`
	Judge       types.JudgeConfig   `json:"judge"`
}

func (s *Server) handleCreatePreset(w http.ResponseWriter, r *http.Request) {
	var req createPresetRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Panel) == 0 {
		writeError(w, http.StatusBadRequest, "panel must have at least one member")
		return
	}
	for i, m := range req.Panel {
		if m.Provider == "" || m.Model == "" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("panel[%d]: provider and model are required", i))
			return
		}
	}
	if req.Judge.Provider == "" || req.Judge.Model == "" {
		writeError(w, http.StatusBadRequest, "judge: provider and model are required")
		return
	}

	preset := types.Preset{
		Name:        req.Name,
		Description: req.Description,
		Panel:       req.Panel,
		Judge:       req.Judge,
	}

	if err := s.engine.CreatePreset(req.Name, preset); err != nil {
		s.log.Warn("create preset failed", "name", req.Name, "error", err.Error())
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     "openfusion/" + req.Name,
		"object": "model",
		"created": time.Now().Unix(),
	})
}

func (s *Server) handleGetPreset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	preset, err := s.engine.GetPreset(name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("preset not found: %s", name))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          "openfusion/" + name,
		"object":      "model",
		"description": preset.Description,
		"panel":       preset.Panel,
		"judge":       preset.Judge,
	})
}

func (s *Server) handleDeletePreset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if err := s.engine.DeletePreset(name); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("preset not found: %s", name))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req types.ChatRequest
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB limit
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.log.Warn("invalid request body", "error", err.Error())
		writeError(w, http.StatusBadRequest, "invalid request body")
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

	// Validate field ranges
	if req.MaxTokens < 0 {
		req.MaxTokens = 0
	}
	if req.MaxTokens > 131072 {
		req.MaxTokens = 131072
	}
	if req.Temperature != nil {
		if *req.Temperature < 0 || *req.Temperature > 2 {
			t := 1.0
			req.Temperature = &t
		}
	}
	if req.ThinkBudget < 0 {
		req.ThinkBudget = 0
	}
	if req.ThinkBudget > 131072 {
		req.ThinkBudget = 131072
	}

	// Validate panel override
	if len(req.PanelOverride) > 0 {
		for i, m := range req.PanelOverride {
			if m.Provider == "" || m.Model == "" {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("panel[%d]: provider and model are required", i))
				return
			}
		}
	}
	// Validate judge override
	if req.JudgeOverride != nil {
		if req.JudgeOverride.Provider == "" || req.JudgeOverride.Model == "" {
			writeError(w, http.StatusBadRequest, "judge override: provider and model are required")
			return
		}
	}

	// Rate limit check (normalize model name to prevent case/whitespace bypass)
	model := strings.ToLower(strings.TrimSpace(req.Model))
	if s.rateLimiter != nil && s.rateLimiter.Enabled() {
		allowed, retryAfter := s.rateLimiter.Allow(model)
		if !allowed {
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":              fmt.Sprintf("rate limit exceeded for preset '%s'", model),
				"retry_after_seconds": retryAfter.Seconds(),
			})
			return
		}
	}

	// Auto-route via skill matching when model is auto
	if model == "auto" || model == "openfusion/auto" {
		resp, err := s.engine.ExecuteAuto(&req)
		if err != nil {
		s.log.Warn("auto-route failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "auto-route failed")
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

	resp, err := s.engine.Execute(model, &req)
	if err != nil {
		s.log.Warn("fusion execution failed", "preset", model, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "fusion execution failed")
		return
	}

	s.logFusion(model, &req, resp)

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

	// Run fusion synchronously (panel + judge must complete first)
	resp, err := s.engine.Execute(req.Model, req)
	if err != nil {
		s.log.Warn("streaming fusion failed", "error", err.Error())
		fmt.Fprintf(w, "data: {\"error\":\"internal error\"}\n\n")
		flusher.Flush()
		return
	}

	// Check for zero choices to avoid index panic
	if len(resp.Choices) == 0 {
		fmt.Fprintf(w, "data: {\"error\":\"empty response\"}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
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
		analysisJSON, _ := json.Marshal(map[string]any{
			"type":            "analysis",
			"consensus_count":  len(resp.Analysis.Consensus),
			"contradictions":   len(resp.Analysis.Contradictions),
			"blind_spots":      len(resp.Analysis.BlindSpots),
			"unique_insights":  len(resp.Analysis.UniqueInsights),
		})
		fmt.Fprintf(w, "data: %s\n\n", analysisJSON)
		flusher.Flush()
	}

	// Stream answer content using buffered stream with sentence-boundary flush
	created := time.Now().Unix()
	buf := NewStreamBuffer(200, 50*time.Millisecond)
	lastFlush := time.Now()

	flushChunk := func(content string) {
		chunk := types.StreamChunk{
			ID:      resp.ID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   resp.Model,
			Choices: []types.StreamChoice{{
				Index: 0,
				Delta: types.StreamDelta{Content: content},
			}},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for _, ch := range answer {
		select {
		case <-r.Context().Done():
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		default:
		}

		if flushed := buf.Add(ch); flushed != "" {
			flushChunk(flushed)
			lastFlush = time.Now()
		} else if buf.ShouldFlush(lastFlush) {
			if content := buf.Finalize(); content != "" {
				flushChunk(content)
				lastFlush = time.Now()
			}
		}
	}

	// Flush remaining content
	if remaining := buf.Finalize(); remaining != "" {
		flushChunk(remaining)
	}

	// Send usage as final metadata chunk
	usageJSON, _ := json.Marshal(map[string]any{
		"type":              "usage",
		"prompt_tokens":     resp.Usage.PromptTokens,
		"completion_tokens": resp.Usage.CompletionTokens,
		"total_tokens":      resp.Usage.TotalTokens,
		"cost_usd":          resp.Usage.CostUSD,
	})
	fmt.Fprintf(w, "data: %s\n\n", usageJSON)

	// Send termination signal
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// logFusion writes a fusion log entry asynchronously after a successful completion.
func (s *Server) logFusion(model string, req *types.ChatRequest, resp *types.ChatResponse) {
	if s.hook == nil {
		return
	}

	preset := strings.TrimPrefix(model, "openfusion/")
	if preset == model {
		preset = model
	}

	now := time.Now().UTC()
	query := types.ExtractLastUserMessage(req.Messages)
	if len(query) > 1000 {
		query = query[:1000]
	}

	entry := &logging.FusionLog{
		FusionID:        resp.ID,
		Timestamp:       now.Format(time.RFC3339),
		Preset:          preset,
		Query:           query,
		JudgeModel:      "",
		JudgeAnalysis:   "",
		FinalAnswer:     "",
		PromptTokens:    resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:     resp.Usage.TotalTokens,
		CostUSD:         resp.Usage.CostUSD,
		PanelErrors:     0,
	}

	// Fill panel data from response
	for i, pr := range resp.PanelResponses {
		switch i {
		case 0:
			entry.ModelAName = pr.Model
			entry.ModelAOutput = logging.SanitizeCSVField(pr.Content)
			entry.ModelAStatus = statusStr(pr.Error)
		case 1:
			entry.ModelANameB = pr.Model
			entry.ModelBOutput = logging.SanitizeCSVField(pr.Content)
			entry.ModelBStatus = statusStr(pr.Error)
		case 2:
			entry.ModelCName = pr.Model
			entry.ModelCOutput = logging.SanitizeCSVField(pr.Content)
			entry.ModelCStatus = statusStr(pr.Error)
		}
		if pr.Error != "" {
			entry.PanelErrors++
		}
	}

	// Fill analysis from response
	if resp.Analysis != nil {
		entry.JudgeAnalysis = logging.SanitizeCSVField(formatAnalysis(resp.Analysis))
	}

	// Fill final answer
	if len(resp.Choices) > 0 {
		entry.FinalAnswer = logging.SanitizeCSVField(resp.Choices[0].Message.Content)
	}

	s.hook.Log(entry)
}

func statusStr(err string) string {
	if err == "" {
		return "success"
	}
	return "error"
}

func formatAnalysis(a *types.FusionAnalysis) string {
	if a == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Consensus: ")
	writeStrings(&b, a.Consensus)
	b.WriteString(" | Contradictions: ")
	for i, c := range a.Contradictions {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(c.Issue)
	}
	b.WriteString(" | Unique: ")
	for i, u := range a.UniqueInsights {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(u.Model)
		b.WriteString(": ")
		b.WriteString(u.Insight)
	}
	b.WriteString(" | BlindSpots: ")
	writeStrings(&b, a.BlindSpots)
	return b.String()
}

func writeStrings(b *strings.Builder, items []string) {
	for i, s := range items {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(s)
	}
}

