package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPresetsCRUD(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "", ":8080", nil, nil)

	// List presets (should be empty initially)
	t.Run("list empty", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/v1/presets", nil)
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]any
		json.NewDecoder(rec.Body).Decode(&body)
		if body["total"].(float64) != 0 {
			t.Errorf("total = %v, want 0", body["total"])
		}
	})

	// Create a preset
	t.Run("create", func(t *testing.T) {
		payload := `{"name":"test-preset","panel":[{"provider":"p1","model":"m1"}],"judge":{"provider":"p1","model":"judge-m"}}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/presets", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
		}
	})

	// Get the preset
	t.Run("get", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/v1/presets/test-preset", nil)
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})

	// List again (should have 1)
	t.Run("list one", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/v1/presets", nil)
		srv.Handler().ServeHTTP(rec, req)
		var body map[string]any
		json.NewDecoder(rec.Body).Decode(&body)
		if body["total"].(float64) != 1 {
			t.Errorf("total = %v, want 1", body["total"])
		}
	})

	// Delete the preset
	t.Run("delete", func(t *testing.T) {
		rec := httptest.NewRecorder()
		url := fmt.Sprintf("/v1/presets/%s", "test-preset")
		req := httptest.NewRequest("DELETE", url, nil)
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("DELETE %s: status = %d, want 204. Body: %s", url, rec.Code, rec.Body.String())
		}
	})

	// Get deleted should 404
	t.Run("get deleted 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/v1/presets/test-preset", nil)
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})

	// Create with invalid body (empty panel)
	t.Run("create invalid", func(t *testing.T) {
		payload := `{"name":"bad","panel":[],"judge":{"provider":"p1","model":"m1"}}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/presets", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
	})
}
