package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultA2ACard(t *testing.T) {
	card := defaultA2ACard("http://localhost:8090")

	if card.Name != "ui-agent" {
		t.Errorf("expected name=ui-agent, got %s", card.Name)
	}
	if card.Version != "dev" {
		t.Errorf("expected version=dev, got %s", card.Version)
	}
	if len(card.Capabilities) != 6 {
		t.Errorf("expected 6 capabilities, got %d", len(card.Capabilities))
	}

	expectedCaps := map[string]bool{
		"browser_automation":      true,
		"dom_self_healing":        true,
		"vlm_visual_verification": true,
		"pattern_tracking":        true,
		"drift_detection":         true,
		"multi_model_routing":     true,
	}
	for _, c := range card.Capabilities {
		if !expectedCaps[c] {
			t.Errorf("unexpected capability: %s", c)
		}
	}
}

func TestA2ACardEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	card := defaultA2ACard("http://localhost:8090")

	mux.HandleFunc("/.well-known/a2a-card", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(card)
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/a2a-card", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got A2ACard
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if got.Name != "ui-agent" {
		t.Errorf("expected name=ui-agent, got %s", got.Name)
	}
}

func TestHealthEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"version": version,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}
}

func TestHealEndpointMethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/heal", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/heal", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHealEndpointBadBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/heal", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			PageURL string `json:"page_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/heal", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRunTaskEndpointMethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/run-task", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/run-task", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestStatusCommandCard(t *testing.T) {
	card := defaultA2ACard("http://localhost:8090")
	data, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(data), "ui-agent") {
		t.Error("expected card to contain 'ui-agent'")
	}
	if !strings.Contains(string(data), "dom_self_healing") {
		t.Error("expected card to contain 'dom_self_healing'")
	}
}
