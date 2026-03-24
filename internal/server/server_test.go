package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/interfaces"
	"github.com/infercore/infercore/internal/slo"
	"github.com/infercore/infercore/internal/types"
	"github.com/infercore/infercore/internal/upstream"
)

func TestInfer_SkipsUnhealthyRuleBackend(t *testing.T) {
	cfg := testConfigTwoBackends(100, true)
	srv := New(cfg)
	srv.adapters["large-model"] = &alwaysUnhealthyAdapter{name: "large-model"}

	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input": map[string]any{
			"text": "hello",
		},
		"options": map[string]any{
			"stream":     false,
			"max_tokens": 128,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", status, string(body))
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if parsed["selected_backend"] != "small-model" {
		t.Fatalf("expected small-model, got %v", parsed["selected_backend"])
	}
}

func TestInfer_AllBackendsUnhealthyRouteError(t *testing.T) {
	cfg := testConfigTwoBackends(100, true)
	srv := New(cfg)
	srv.adapters["small-model"] = &alwaysUnhealthyAdapter{name: "small-model"}
	srv.adapters["large-model"] = &alwaysUnhealthyAdapter{name: "large-model"}

	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input": map[string]any{
			"text": "hello",
		},
		"options": map[string]any{
			"stream":     false,
			"max_tokens": 128,
		},
	})
	if status != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", status, string(body))
	}
	assertStructuredError(t, body, errCodeRouteError)
}

func TestInfer_UnauthorizedWhenAPIKeyConfigured(t *testing.T) {
	cfg := testConfig(100, true)
	cfg.Server.InfercoreAPIKey = "unit-test-secret"
	srv := New(cfg)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input":     map[string]any{"text": "hello"},
		"options":   map[string]any{"stream": false, "max_tokens": 128},
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", status, string(body))
	}
	assertStructuredError(t, body, errCodeUnauthorized)

	// /health stays public
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status %d", resp.StatusCode)
	}
}

func TestInfer_OverloadRejectsSecondRequest(t *testing.T) {
	cfg := testConfig(8000, true)
	cfg.Reliability.Overload.QueueLimit = 1
	cfg.Reliability.Overload.Action = "reject"
	srv := New(cfg)
	started := make(chan struct{})
	release := make(chan struct{})
	srv.adapters["small-model"] = &blockingInferAdapter{
		started: started,
		release: release,
		name:    "small-model",
	}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		st, b := postInfer(t, ts.URL, map[string]any{
			"tenant_id": "team-a",
			"task_type": "simple",
			"input":     map[string]any{"text": "first"},
			"options":   map[string]any{"stream": false, "max_tokens": 128},
		})
		if st != http.StatusOK {
			t.Errorf("first request expected 200, got %d %s", st, string(b))
		}
	}()

	<-started
	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input":     map[string]any{"text": "second"},
		"options":   map[string]any{"stream": false, "max_tokens": 128},
	})
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", status, string(body))
	}
	assertStructuredError(t, body, errCodeOverload)
	close(release)
	wg.Wait()
}

func TestInfer_OKWithInfercoreAPIKeyHeader(t *testing.T) {
	cfg := testConfig(100, true)
	cfg.Server.InfercoreAPIKey = "unit-test-secret"
	srv := New(cfg)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	payload, _ := json.Marshal(map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input":     map[string]any{"text": "hello"},
		"options":   map[string]any{"stream": false, "max_tokens": 128},
	})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/infer", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InferCore-Api-Key", "unit-test-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d %s", resp.StatusCode, string(body))
	}
}

func testConfigWithFileKB(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.txt")
	content := "InferCore provides routing and fallback for AI requests.\n\nSecond chunk about observability."
	if err := os.WriteFile(doc, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(100, true)
	cfg.KnowledgeBases = []config.KnowledgeBaseConfig{
		{Name: "kb1", Type: "file", Path: dir},
	}
	return cfg
}

func TestInfer_RAG_WithFileKB_Success200(t *testing.T) {
	cfg := testConfigWithFileKB(t)
	srv := New(cfg)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"request_type": "rag",
		"tenant_id":    "team-a",
		"task_type":    "simple",
		"priority":     "high",
		"context": map[string]any{
			"knowledge_base": "kb1",
			"query":          "routing fallback infercore",
		},
		"input": map[string]any{
			"text": "routing fallback infercore",
		},
		"options": map[string]any{"stream": false, "max_tokens": 128},
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", status, string(body))
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed["selected_backend"] != "small-model" {
		t.Fatalf("backend=%v", parsed["selected_backend"])
	}
	result, ok := parsed["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %s", string(body))
	}
	text, _ := result["text"].(string)
	if !strings.Contains(text, "[small-model]") {
		t.Fatalf("expected mock prefix in result text, got %q", text)
	}
}

func TestInfer_RAG_NoKnowledgeBasesConfigured400(t *testing.T) {
	cfg := testConfig(100, true)
	cfg.KnowledgeBases = nil
	srv := New(cfg)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"request_type": "rag",
		"tenant_id":    "team-a",
		"task_type":    "simple",
		"priority":     "high",
		"input": map[string]any{
			"text": "hello",
		},
		"options": map[string]any{"stream": false, "max_tokens": 128},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", status, string(body))
	}
	assertStructuredError(t, body, errCodeRAGNotConfigured)
}

func TestInfer_RAG_UnknownKnowledgeBase400(t *testing.T) {
	cfg := testConfigWithFileKB(t)
	srv := New(cfg)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"request_type": "rag",
		"tenant_id":    "team-a",
		"task_type":    "simple",
		"priority":     "high",
		"context": map[string]any{
			"knowledge_base": "does-not-exist",
			"query":          "hello",
		},
		"input":   map[string]any{"text": "hello"},
		"options": map[string]any{"stream": false, "max_tokens": 128},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", status, string(body))
	}
	assertStructuredError(t, body, errCodeRAGNotConfigured)
}

func TestInfer_RAG_EmptyQuery400(t *testing.T) {
	cfg := testConfigWithFileKB(t)
	srv := New(cfg)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"request_type": "rag",
		"tenant_id":    "team-a",
		"task_type":    "simple",
		"priority":     "high",
		"context": map[string]any{
			"knowledge_base": "kb1",
		},
		"input":   map[string]any{"text": ""},
		"options": map[string]any{"stream": false, "max_tokens": 128},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", status, string(body))
	}
	assertStructuredError(t, body, errCodeInvalidRequest)
}

func testConfigOpenAIEndpoint(t *testing.T, endpoint string) *config.Config {
	t.Helper()
	cfg := testConfig(100, true)
	cfg.Backends[0].Type = "openai_compatible"
	cfg.Backends[0].Endpoint = endpoint
	return cfg
}

func TestInfer_StreamUpstreamJSON_DegradeFlagInResponse(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": "aggregated"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)

	cfg := testConfigOpenAIEndpoint(t, up.URL)
	srv := New(cfg)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"priority":  "high",
		"input":     map[string]any{"text": "hi"},
		"options":   map[string]any{"stream": true, "max_tokens": 128},
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", status, string(body))
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	degrade, ok := parsed["degrade"].(map[string]any)
	if !ok {
		t.Fatalf("missing degrade: %s", string(body))
	}
	if triggered, _ := degrade["triggered"].(bool); !triggered {
		t.Fatalf("expected degrade.triggered=true for stream_degraded, got %+v", degrade)
	}
}

func TestInfer_StreamUpstreamSSE_SuccessNoDegrade(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"X\"}}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)

	cfg := testConfigOpenAIEndpoint(t, up.URL)
	srv := New(cfg)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"priority":  "high",
		"input":     map[string]any{"text": "hi"},
		"options":   map[string]any{"stream": true, "max_tokens": 128},
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", status, string(body))
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	degrade, ok := parsed["degrade"].(map[string]any)
	if !ok {
		t.Fatalf("missing degrade: %s", string(body))
	}
	if triggered, _ := degrade["triggered"].(bool); triggered {
		t.Fatalf("expected no degrade for SSE stream, got %+v", degrade)
	}
}

func TestInfer_Agent_NotImplemented501(t *testing.T) {
	cfg := testConfig(100, true)
	cfg.Features.AgentEnabled = true
	srv := New(cfg)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"request_type": "agent",
		"tenant_id":    "team-a",
		"task_type":    "simple",
		"priority":     "high",
		"input":        map[string]any{"text": "task"},
		"options":      map[string]any{"stream": false, "max_tokens": 128},
	})
	if status != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d body=%s", status, string(body))
	}
	assertStructuredError(t, body, errCodeAgentNotImplemented)
}

func TestInfer_Success200(t *testing.T) {
	srv := New(testConfig(100, true))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input": map[string]any{
			"text": "hello",
		},
		"options": map[string]any{
			"stream":     false,
			"max_tokens": 128,
		},
	})

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", status, string(body))
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("response must be valid json: %v", err)
	}
	if _, ok := parsed["policy_reason"]; !ok {
		t.Fatalf("missing policy_reason")
	}
	if _, ok := parsed["effective_priority"]; !ok {
		t.Fatalf("missing effective_priority")
	}
	if _, ok := parsed["trace_id"]; !ok {
		t.Fatalf("missing trace_id")
	}
	if _, ok := parsed["degrade"]; !ok {
		t.Fatalf("missing degrade state")
	}
}

func TestInfer_PolicyRejected429(t *testing.T) {
	srv := New(testConfig(100, true))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "unknown-tenant",
		"task_type": "simple",
		"input": map[string]any{
			"text": "hello",
		},
		"options": map[string]any{
			"stream":     false,
			"max_tokens": 128,
		},
	})

	if status != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", status)
	}
	assertStructuredError(t, body, errCodePolicyRejected)
}

func TestInfer_InvalidInput400(t *testing.T) {
	srv := New(testConfig(100, true))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"options": map[string]any{
			"stream":     false,
			"max_tokens": 128,
		},
	})

	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
	assertStructuredError(t, body, errCodeInvalidRequest)
}

func TestInfer_InvalidJSON400(t *testing.T) {
	srv := New(testConfig(100, true))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/infer", bytes.NewBufferString("{"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	assertStructuredError(t, raw, errCodeInvalidRequest)
}

func TestInfer_MethodNotAllowed405(t *testing.T) {
	srv := New(testConfig(100, true))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/infer")
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
	assertStructuredError(t, raw, errCodeMethodNotAllowed)
}

func TestInfer_InvalidMaxTokens400(t *testing.T) {
	srv := New(testConfig(100, true))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input": map[string]any{
			"text": "hello",
		},
		"options": map[string]any{
			"stream":     false,
			"max_tokens": 0,
		},
	})

	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
	assertStructuredError(t, body, errCodeInvalidOptions)
}

func TestHTTPLayerTimeouts_Overrides(t *testing.T) {
	cfg := testConfig(100, true)
	cfg.Server.HTTP.ReadTimeoutMS = 5000
	cfg.Server.HTTP.WriteTimeoutMS = 6000
	cfg.Server.HTTP.IdleTimeoutMS = 7000
	r, w, i := HTTPLayerTimeouts(cfg)
	if r != 5*time.Second || w != 6*time.Second || i != 7*time.Second {
		t.Fatalf("got read=%v write=%v idle=%v", r, w, i)
	}
}

func TestInfer_GlobalDeadline504(t *testing.T) {
	cfg := testConfig(10_000, false)
	cfg.Server.RequestTimeoutMS = 80
	srv := New(cfg)
	srv.adapters["small-model"] = &blockingInferAdapter{
		started: make(chan struct{}),
		release: make(chan struct{}),
		name:    "small-model",
	}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input":     map[string]any{"text": "hello"},
		"options":   map[string]any{"stream": false, "max_tokens": 128},
	})
	if status != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d body=%s", status, string(body))
	}
	assertStructuredError(t, body, errCodeGatewayTimeout)
}

func TestInfer_ExecutionFailed502(t *testing.T) {
	// mock adapter sleeps ~20ms; timeout 1ms forces deadline exceeded.
	srv := New(testConfig(1, false))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input": map[string]any{
			"text": "hello",
		},
		"options": map[string]any{
			"stream":     false,
			"max_tokens": 128,
		},
	})

	if status != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", status)
	}
	assertStructuredError(t, body, errCodeExecutionFailed)
}

func TestInfer_FallbackSkipsUnhealthyBackend(t *testing.T) {
	cfg := testConfigTwoBackends(100, true)
	// Force primary route to small-model (no premium→large rule for this scenario).
	cfg.Routing.Rules = nil
	cfg.Reliability.FallbackRules = []config.FallbackRule{
		{FromBackend: "small-model", On: []string{"timeout"}, FallbackTo: "large-model"},
	}
	srv := New(cfg)
	srv.adapters["small-model"] = &timeoutErrorAdapter{name: "small-model"}
	srv.adapters["large-model"] = &alwaysUnhealthyAdapter{name: "large-model"}

	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, body := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input":     map[string]any{"text": "hello"},
		"options":   map[string]any{"stream": false, "max_tokens": 128},
	})
	if status != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", status, string(body))
	}
	assertStructuredError(t, body, errCodeExecutionFailed)
}

func TestStatus_Success200(t *testing.T) {
	srv := New(testConfig(100, true))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatalf("status request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read status body: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("status response invalid json: %v", err)
	}

	if parsed["service"] != "infercore" {
		t.Fatalf("unexpected service value: %v", parsed["service"])
	}
	if _, ok := parsed["telemetry"]; !ok {
		t.Fatalf("expected telemetry status summary")
	}
	if _, ok := parsed["scaling_signals"]; !ok {
		t.Fatalf("expected scaling_signals in status payload")
	}
	backends, ok := parsed["backends"].([]any)
	if !ok || len(backends) == 0 {
		t.Fatalf("expected non-empty backends list")
	}
}

func TestHealth_Success200(t *testing.T) {
	srv := New(testConfig(100, true))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read health body: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("health response invalid json: %v", err)
	}
	if parsed["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", parsed["status"])
	}
}

func TestMetrics_Success200(t *testing.T) {
	srv := New(testConfig(100, true))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// Generate one non-metrics request so labeled HTTP counter is populated.
	_, _ = http.Get(ts.URL + "/health")

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("expected text/plain content type, got %s", resp.Header.Get("Content-Type"))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	metricsBody := string(raw)
	if !strings.Contains(metricsBody, "infercore_requests_total") {
		t.Fatalf("expected infercore_requests_total metric")
	}
	if !strings.Contains(metricsBody, "infercore_http_requests_total") {
		t.Fatalf("expected infercore_http_requests_total metric")
	}
	if !strings.Contains(metricsBody, "infercore_scaling_ttft_degradation_ratio") {
		t.Fatalf("expected infercore_scaling_ttft_degradation_ratio metric")
	}
	if !strings.Contains(metricsBody, "path=\"/health\"") {
		t.Fatalf("expected /health labeled metric entry")
	}
	if !strings.Contains(metricsBody, "method=\"GET\"") {
		t.Fatalf("expected GET method labeled metric entry")
	}
}

func TestInfer_EmitsTraceRecord(t *testing.T) {
	exporter := &captureTelemetryExporter{}
	var sloEngine interfaces.SLOEngine = slo.NewMemoryEngine()
	srv := NewWithDependencies(testConfig(100, true), sloEngine, exporter)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, _ := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input": map[string]any{
			"text": "hello",
		},
		"options": map[string]any{
			"stream":     false,
			"max_tokens": 128,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	traces := exporter.Traces()
	if len(traces) == 0 {
		t.Fatalf("expected at least one emitted trace record")
	}
	last := traces[len(traces)-1]
	if last.Name != "infer_request" {
		t.Fatalf("unexpected trace name: %s", last.Name)
	}
	if last.TraceID == "" || last.SpanID == "" {
		t.Fatalf("trace and span id must be set")
	}
	if last.Labels["result"] != "success" {
		t.Fatalf("expected success trace label, got %q", last.Labels["result"])
	}
}

func TestInfer_TracingDisabledDoesNotEmitTrace(t *testing.T) {
	cfg := testConfig(100, true)
	cfg.Telemetry.TracingEnabled = false

	exporter := &captureTelemetryExporter{}
	var sloEngine interfaces.SLOEngine = slo.NewMemoryEngine()
	srv := NewWithDependencies(cfg, sloEngine, exporter)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	status, _ := postInfer(t, ts.URL, map[string]any{
		"tenant_id": "team-a",
		"task_type": "simple",
		"input": map[string]any{
			"text": "hello",
		},
		"options": map[string]any{
			"stream":     false,
			"max_tokens": 128,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if len(exporter.Traces()) != 0 {
		t.Fatalf("expected no traces when tracing disabled")
	}
}

func TestMetrics_DisabledMessage(t *testing.T) {
	cfg := testConfig(100, true)
	cfg.Telemetry.MetricsEnabled = false
	srv := New(cfg)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), "metrics disabled") {
		t.Fatalf("expected metrics disabled message")
	}
}

func postInfer(t *testing.T, baseURL string, payload map[string]any) (int, []byte) {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/infer", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, raw
}

func assertStructuredError(t *testing.T, body []byte, expectedCode string) {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("response must be valid json: %v", err)
	}

	if parsed["status"] != "error" {
		t.Fatalf("expected status=error, got %v", parsed["status"])
	}
	errorObj, ok := parsed["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object in response")
	}
	if errorObj["code"] != expectedCode {
		t.Fatalf("expected error.code=%s, got %v", expectedCode, errorObj["code"])
	}
}

type blockingInferAdapter struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
	name    string
}

func (b *blockingInferAdapter) Name() string { return b.name }

func (b *blockingInferAdapter) Invoke(ctx context.Context, req types.BackendRequest) (types.BackendResponse, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
	case <-ctx.Done():
		return types.BackendResponse{}, ctx.Err()
	}
	return types.BackendResponse{
		Output: map[string]any{"text": "ok", "backend": b.name},
		Timing: &types.BackendTiming{TTFTMs: 1, CompletionLatencyMs: 2},
	}, nil
}

func (b *blockingInferAdapter) Health(context.Context) error { return nil }

func (b *blockingInferAdapter) Metadata() types.BackendMetadata {
	return types.BackendMetadata{Name: b.name, Type: "mock"}
}

type alwaysUnhealthyAdapter struct {
	name string
}

func (a *alwaysUnhealthyAdapter) Name() string { return a.name }

func (a *alwaysUnhealthyAdapter) Invoke(context.Context, types.BackendRequest) (types.BackendResponse, error) {
	return types.BackendResponse{}, fmt.Errorf("unhealthy adapter should not be invoked in this test")
}

func (a *alwaysUnhealthyAdapter) Health(context.Context) error {
	return fmt.Errorf("unhealthy")
}

func (a *alwaysUnhealthyAdapter) Metadata() types.BackendMetadata {
	return types.BackendMetadata{Name: a.name, Type: "mock"}
}

type timeoutErrorAdapter struct {
	name string
}

func (a *timeoutErrorAdapter) Name() string { return a.name }

func (a *timeoutErrorAdapter) Invoke(context.Context, types.BackendRequest) (types.BackendResponse, error) {
	return types.BackendResponse{}, upstream.New(upstream.KindTimeout, "stub timeout")
}

func (a *timeoutErrorAdapter) Health(context.Context) error { return nil }

func (a *timeoutErrorAdapter) Metadata() types.BackendMetadata {
	return types.BackendMetadata{Name: a.name, Type: "mock"}
}

func testConfigTwoBackends(timeoutMS int, fallbackEnabled bool) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host:             "127.0.0.1",
			Port:             0,
			RequestTimeoutMS: 3000,
		},
		Telemetry: config.TelemetryConfig{
			MetricsEnabled: true,
			TracingEnabled: true,
			Exporter:       "log",
		},
		Backends: []config.BackendConfig{
			{
				Name:      "small-model",
				Type:      "mock",
				TimeoutMS: timeoutMS,
				Cost:      config.CostConfig{Unit: 1},
				Capabilities: []string{
					"chat",
					"summarization",
				},
			},
			{
				Name:      "large-model",
				Type:      "mock",
				TimeoutMS: timeoutMS,
				Cost:      config.CostConfig{Unit: 5},
				Capabilities: []string{
					"chat",
					"reasoning",
					"summarization",
				},
			},
		},
		Tenants: []config.TenantConfig{
			{
				ID:               "team-a",
				Class:            "premium",
				Priority:         "high",
				BudgetPerRequest: 100,
				RateLimitRPS:     100,
			},
		},
		Routing: config.RoutingConfig{
			DefaultBackend: "small-model",
			Rules: []config.RouteRule{
				{
					Name: "premium-simple-expensive-route",
					When: config.RouteWhen{
						TenantClass: "premium",
						TaskType:    "simple",
					},
					UseBackend: "large-model",
				},
			},
		},
		Reliability: config.ReliabilityConfig{
			FallbackEnabled: fallbackEnabled,
			FallbackRules: []config.FallbackRule{
				{
					FromBackend: "small-model",
					On:          []string{"timeout"},
					FallbackTo:  "small-model",
				},
				{
					FromBackend: "large-model",
					On:          []string{"timeout"},
					FallbackTo:  "small-model",
				},
			},
		},
	}
}

func testConfig(timeoutMS int, fallbackEnabled bool) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host:             "127.0.0.1",
			Port:             0,
			RequestTimeoutMS: 3000,
		},
		Telemetry: config.TelemetryConfig{
			MetricsEnabled: true,
			TracingEnabled: true,
			Exporter:       "log",
		},
		Backends: []config.BackendConfig{
			{
				Name:      "small-model",
				Type:      "mock",
				TimeoutMS: timeoutMS,
				Cost:      config.CostConfig{Unit: 1},
				Capabilities: []string{
					"chat",
					"summarization",
				},
			},
		},
		Tenants: []config.TenantConfig{
			{
				ID:               "team-a",
				Class:            "premium",
				Priority:         "high",
				BudgetPerRequest: 100,
				RateLimitRPS:     100,
			},
		},
		Routing: config.RoutingConfig{
			DefaultBackend: "small-model",
		},
		Reliability: config.ReliabilityConfig{
			FallbackEnabled: fallbackEnabled,
			FallbackRules: []config.FallbackRule{
				{
					FromBackend: "small-model",
					On:          []string{"timeout"},
					FallbackTo:  "small-model",
				},
			},
		},
	}
}

type captureTelemetryExporter struct {
	mu     sync.Mutex
	traces []types.TraceRecord
}

func (e *captureTelemetryExporter) EmitMetric(_ string, _ float64, _ map[string]string) {}
func (e *captureTelemetryExporter) EmitEvent(_ types.Event)                             {}
func (e *captureTelemetryExporter) EmitTrace(trace types.TraceRecord) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.traces = append(e.traces, trace)
}

func (e *captureTelemetryExporter) Traces() []types.TraceRecord {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]types.TraceRecord, len(e.traces))
	copy(out, e.traces)
	return out
}
