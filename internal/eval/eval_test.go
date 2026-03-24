package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_ItemHTTPNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "error",
			"error":  map[string]any{"code": "invalid_request", "message": "bad"},
		})
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	path := filepath.Join(dir, "dataset.json")
	raw := `[{"tenant_id":"team-a","task_type":"chat","input":{"text":"x"},"options":{"stream":false,"max_tokens":64}}]`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := Run(context.Background(), srv.URL, path, &buf, "inference/basic:v1", "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ok=false") || !strings.Contains(out, "http=400") {
		t.Fatalf("expected non-OK row in output:\n%s", out)
	}
	if !strings.Contains(out, "summary:") {
		t.Fatalf("missing summary:\n%s", out)
	}
}

func TestRun_SingleItemOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/infer" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"selected_backend": "small-model",
			"fallback":         map[string]any{"triggered": false},
		})
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	path := filepath.Join(dir, "dataset.json")
	raw := `[{"tenant_id":"team-a","task_type":"chat","input":{"text":"hi"},"options":{"stream":false,"max_tokens":64}}]`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := Run(context.Background(), srv.URL, path, &buf, "inference/basic:v1", "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "summary:") || !strings.Contains(out, "items=1") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}
