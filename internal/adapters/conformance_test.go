package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infercore/infercore/internal/adapters/gemini"
	"github.com/infercore/infercore/internal/adapters/mock"
	"github.com/infercore/infercore/internal/adapters/vllm"
	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/interfaces"
	"github.com/infercore/infercore/internal/types"
)

func TestAdapterConformance_Mock(t *testing.T) {
	adapter := mock.New(config.BackendConfig{
		Name:         "mock-b",
		Type:         "mock",
		TimeoutMS:    100,
		Capabilities: []string{"chat"},
		Cost:         config.CostConfig{Unit: 1},
	})
	assertAdapterConformance(t, adapter)
}

func TestAdapterConformance_VLLM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": "hello from vllm"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := vllm.New(config.BackendConfig{
		Name:         "vllm-b",
		Type:         "vllm",
		Endpoint:     srv.URL,
		TimeoutMS:    200,
		Capabilities: []string{"chat"},
		Cost:         config.CostConfig{Unit: 2},
	})
	assertAdapterConformance(t, adapter)
}

func TestAdapterConformance_Gemini(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1beta/models":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":generateContent"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]any{{"text": "hello from gemini"}},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := gemini.New(config.BackendConfig{
		Name:         "gem-b",
		Type:         "gemini",
		Endpoint:     srv.URL,
		TimeoutMS:    200,
		APIKey:       "test-key",
		DefaultModel: "gemini-2.0-flash",
		Capabilities: []string{"chat"},
		Cost:         config.CostConfig{Unit: 2},
	})
	assertAdapterConformance(t, adapter)
}

func assertAdapterConformance(t *testing.T, adapter interfaces.BackendAdapter) {
	t.Helper()

	if adapter.Name() == "" {
		t.Fatalf("adapter name must not be empty")
	}
	if err := adapter.Health(context.Background()); err != nil {
		t.Fatalf("health should pass in conformance test: %v", err)
	}
	resp, err := adapter.Invoke(context.Background(), types.BackendRequest{
		AIRequest: types.AIRequest{
			Input: map[string]any{"text": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("invoke should succeed: %v", err)
	}
	if resp.Output == nil {
		t.Fatalf("response output should not be nil")
	}
	md := adapter.Metadata()
	if md.Name == "" || md.Type == "" {
		t.Fatalf("metadata should include name and type")
	}
}
