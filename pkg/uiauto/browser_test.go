package uiauto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserAgent(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!DOCTYPE html><html><head><title>Test</title></head><body><h1 id='title'>Hello World</h1><input type='text' id='input'><button id='btn'>Click</button></body></html>`))
	}))
	defer ts.Close()

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping test, could not start browser: %v", err)
	}
	defer agent.Close()

	err = agent.Navigate(ts.URL)
	if err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}

	html, err := agent.CaptureDOM()
	if err != nil {
		t.Fatalf("Failed to capture DOM: %v", err)
	}
	if !strings.Contains(html, "Hello World") {
		t.Errorf("DOM does not contain expected text")
	}

	screenshot, err := agent.CaptureScreenshot()
	if err != nil {
		t.Fatalf("Failed to capture screenshot: %v", err)
	}
	if len(screenshot) == 0 {
		t.Errorf("Screenshot is empty")
	}
}

func TestNewBrowserAgentWithRemote_UnreachableURL(t *testing.T) {
	_, err := NewBrowserAgentWithRemote("http://127.0.0.1:19999")
	if err == nil {
		t.Fatal("expected error for unreachable debug URL")
	}
}

func TestEnsureCDPTab_CreatesTab(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	})
	mux.HandleFunc("/json/new", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]string{
			"id":                   "FAKE-TAB-ID",
			"type":                 "page",
			"url":                  "about:blank",
			"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/page/FAKE-TAB-ID",
		}
		json.NewEncoder(w).Encode(resp)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	err := ensureCDPTab(ts.URL)
	if err != nil {
		t.Fatalf("ensureCDPTab failed: %v", err)
	}
}

func TestEnsureCDPTab_SkipsWhenTabExists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":"existing","type":"page","url":"https://example.com"}]`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	err := ensureCDPTab(ts.URL)
	if err != nil {
		t.Fatalf("ensureCDPTab should skip when tab exists: %v", err)
	}
}

func TestEnsureCDPTab_ErrorOnUnreachable(t *testing.T) {
	err := ensureCDPTab("http://127.0.0.1:19999")
	if err == nil {
		t.Fatal("expected error for unreachable CDP")
	}
}
