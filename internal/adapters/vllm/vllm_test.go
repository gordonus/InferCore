package vllm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
	"github.com/infercore/infercore/internal/upstream"
)

func TestInvoke_StreamRequestedDegradedWhenUpstreamReturnsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "ok"}},
			},
		})
	}))
	defer srv.Close()

	a := New(config.BackendConfig{Name: "v", Type: "vllm", Endpoint: srv.URL, TimeoutMS: 200})
	resp, err := a.Invoke(context.Background(), types.BackendRequest{
		AIRequest: types.AIRequest{
			Input:   map[string]any{"text": "hi"},
			Options: types.RequestOptions{Stream: true, MaxTokens: 32},
		},
	})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if resp.Output["stream_degraded"] != true {
		t.Fatalf("expected stream_degraded=true")
	}
	if resp.Timing == nil || resp.Timing.Streamed {
		t.Fatalf("expected non-stream timing for JSON fallback, got %+v", resp.Timing)
	}
}

func TestInvoke_StreamSSEAccumulates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	a := New(config.BackendConfig{Name: "v", Type: "vllm", Endpoint: srv.URL, TimeoutMS: 2000})
	resp, err := a.Invoke(context.Background(), types.BackendRequest{
		AIRequest: types.AIRequest{
			Input:   map[string]any{"text": "hi"},
			Options: types.RequestOptions{Stream: true, MaxTokens: 32},
		},
	})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if resp.Output["text"] != "Hello" {
		t.Fatalf("text=%v", resp.Output["text"])
	}
	if resp.Output["stream_degraded"] == true {
		t.Fatalf("unexpected stream_degraded")
	}
	if resp.Timing == nil || !resp.Timing.Streamed {
		t.Fatalf("expected streamed timing, got %+v", resp.Timing)
	}
	if resp.Timing.TTFTMs < 0 {
		t.Fatalf("ttft")
	}
}

func TestInvoke_SendsBearerWhenAPIKeySet(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.Header.Get("X-Extra") != "yes" {
			t.Error("missing X-Extra header")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "ok"}},
			},
		})
	}))
	defer srv.Close()

	a := New(config.BackendConfig{
		Name: "v", Type: "openai_compatible", Endpoint: srv.URL, TimeoutMS: 200,
		APIKey: "sk-secret", Headers: map[string]string{"X-Extra": "yes"},
	})
	_, err := a.Invoke(context.Background(), types.BackendRequest{
		AIRequest: types.AIRequest{Input: map[string]any{"text": "hi"}},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if sawAuth != "Bearer sk-secret" {
		t.Fatalf("Authorization = %q", sawAuth)
	}
}

func TestInvoke_CustomAuthHeaderRawKey(t *testing.T) {
	var saw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = r.Header.Get("api-key")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "x"}},
			},
		})
	}))
	defer srv.Close()

	a := New(config.BackendConfig{
		Name: "v", Type: "vllm", Endpoint: srv.URL, TimeoutMS: 200,
		APIKey: "raw-key-value", AuthHeaderName: "api-key",
	})
	_, err := a.Invoke(context.Background(), types.BackendRequest{
		AIRequest: types.AIRequest{Input: map[string]any{"text": "hi"}},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if saw != "raw-key-value" {
		t.Fatalf("api-key header = %q", saw)
	}
}

func TestHealth_UsesConfiguredPathAndAuth(t *testing.T) {
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := New(config.BackendConfig{
		Name: "v", Type: "vllm", Endpoint: srv.URL, TimeoutMS: 200,
		HealthPath: "v1/models",
		APIKey:     "k",
	})
	if err := a.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
	if path != "/v1/models" {
		t.Fatalf("path = %q", path)
	}
	if auth != "Bearer k" {
		t.Fatalf("auth = %q", auth)
	}
}

func TestInvoke_Upstream5xxClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	a := New(config.BackendConfig{Name: "v", Type: "vllm", Endpoint: srv.URL, TimeoutMS: 200})
	_, err := a.Invoke(context.Background(), types.BackendRequest{
		AIRequest: types.AIRequest{Input: map[string]any{"text": "hi"}},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	upErr, ok := err.(*upstream.Error)
	if !ok || upErr.Kind != upstream.KindUpstream5xx {
		t.Fatalf("expected upstream 5xx error, got %T %v", err, err)
	}
}
