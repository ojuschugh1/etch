package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ojuschugh1/etch/internal/config"
	"pgregory.net/rapid"
)

// If there's no API key, NewLLMClient should return nil and no HTTP
// requests should ever be made. We throw a bunch of random configs at
// it (all with empty keys) and make sure nothing leaks out.
func TestNoLLMCallsWithoutAPIKey(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Start a test server that records whether it was called
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(chatResponse{
				Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: "summary"}}},
			})
		}))
		defer srv.Close()

		// Generate configs with empty or unset API keys
		useNilConfig := rapid.Bool().Draw(rt, "useNilConfig")
		endpoint := rapid.SampledFrom([]string{srv.URL, "http://localhost:9999/v1/chat/completions", ""}).Draw(rt, "endpoint")
		model := rapid.SampledFrom([]string{"gpt-4o-mini", "gpt-3.5-turbo", ""}).Draw(rt, "model")
		apiKey := rapid.SampledFrom([]string{"", ""}).Draw(rt, "apiKey") // always empty

		var cfg *config.LLMConfig
		if !useNilConfig {
			cfg = &config.LLMConfig{
				Endpoint: endpoint,
				APIKey:   apiKey,
				Model:    model,
			}
		}
		// cfg is either nil or has an empty API key

		client := NewLLMClient(cfg)

		// NewLLMClient must return nil for empty/unset API key
		if client != nil {
			rt.Fatalf("expected nil LLMClient for empty API key, got non-nil (nilConfig=%v, apiKey=%q)", useNilConfig, apiKey)
		}

		// Since client is nil, no Summarize call can be made, so no HTTP request occurs
		if called {
			rt.Fatalf("HTTP server was called despite empty/unset API key")
		}
	})
}

func TestSummarize_Success(t *testing.T) {
	// Mock HTTP server returning a valid chat completion response
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request structure
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type, got %s", r.Header.Get("Content-Type"))
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if req.Model != "gpt-4o-mini" {
			t.Errorf("expected model gpt-4o-mini, got %s", req.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []chatChoice{
				{Message: chatMessage{Role: "assistant", Content: "The status code changed from 200 to 500."}},
			},
		})
	}))
	defer srv.Close()

	client := NewLLMClient(&config.LLMConfig{
		Endpoint: srv.URL,
		APIKey:   "test-key",
		Model:    "gpt-4o-mini",
	})
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	summary, err := client.Summarize("status_code: 200 -> 500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "The status code changed from 200 to 500." {
		t.Errorf("unexpected summary: %s", summary)
	}
}

func TestSummarize_TimeoutReturnsError(t *testing.T) {
	// Mock server that delays longer than the client timeout
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewLLMClient(&config.LLMConfig{
		Endpoint: srv.URL,
		APIKey:   "test-key",
		Model:    "gpt-4o-mini",
	})
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	// Override timeout to a very short duration for testing
	client.Timeout = 50 * time.Millisecond

	_, err := client.Summarize("some diff content")
	if err == nil {
		t.Fatal("expected error due to timeout, got nil")
	}
}

func TestNewLLMClient_EmptyAPIKeyReturnsNil(t *testing.T) {
	// Empty API key
	client := NewLLMClient(&config.LLMConfig{
		Endpoint: "https://api.openai.com/v1/chat/completions",
		APIKey:   "",
		Model:    "gpt-4o-mini",
	})
	if client != nil {
		t.Error("expected nil client for empty API key")
	}

	// Nil config
	client = NewLLMClient(nil)
	if client != nil {
		t.Error("expected nil client for nil config")
	}
}

func TestSummarize_NonOKStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer srv.Close()

	client := NewLLMClient(&config.LLMConfig{
		Endpoint: srv.URL,
		APIKey:   "test-key",
		Model:    "gpt-4o-mini",
	})

	_, err := client.Summarize("diff content")
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestSummarize_EmptyChoicesReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatResponse{Choices: []chatChoice{}})
	}))
	defer srv.Close()

	client := NewLLMClient(&config.LLMConfig{
		Endpoint: srv.URL,
		APIKey:   "test-key",
		Model:    "gpt-4o-mini",
	})

	_, err := client.Summarize("diff content")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}
