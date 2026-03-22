package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

func TestAdapter_InvokeNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1beta/models":
			if r.Header.Get("x-goog-api-key") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":generateContent"):
			if r.Header.Get("x-goog-api-key") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]any{{"text": "hello gemini"}},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	a := New(config.BackendConfig{
		Name:         "g",
		Type:         "gemini",
		Endpoint:     srv.URL,
		TimeoutMS:    5000,
		APIKey:       "k",
		DefaultModel: "gemini-2.0-flash",
	})

	resp, err := a.Invoke(context.Background(), types.BackendRequest{
		AIRequest: types.AIRequest{
			Input: map[string]any{"text": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	txt, _ := resp.Output["text"].(string)
	if txt != "hello gemini" {
		t.Fatalf("text: %q", txt)
	}
}

func TestAdapter_InvokeVertexNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/prj/locations/us-east1":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, ":generateContent"):
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]any{{"text": "vertex ok"}},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	a := New(config.BackendConfig{
		Name:           "g",
		Type:           "gemini_vertex",
		Endpoint:       srv.URL,
		TimeoutMS:      5000,
		APIKey:         "tok",
		VertexProject:  "prj",
		VertexLocation: "us-east1",
		DefaultModel:   "gemini-2.0-flash",
	})

	resp, err := a.Invoke(context.Background(), types.BackendRequest{
		AIRequest: types.AIRequest{
			Input: map[string]any{"text": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	txt, _ := resp.Output["text"].(string)
	if txt != "vertex ok" {
		t.Fatalf("text: %q", txt)
	}
}

func TestAdapter_Health(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1beta/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-goog-api-key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	a := New(config.BackendConfig{
		Name:         "g",
		Type:         "gemini",
		Endpoint:     srv.URL,
		TimeoutMS:    5000,
		APIKey:       "secret",
		DefaultModel: "gemini-2.0-flash",
	})
	if err := a.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}
