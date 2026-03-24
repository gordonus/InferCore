package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_LoadtestProfileYAML(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "infercore.loadtest.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load infercore.loadtest.yaml: %v", err)
	}
	if cfg.Routing.DefaultBackend != "small-model" {
		t.Fatalf("unexpected default backend: %s", cfg.Routing.DefaultBackend)
	}
	if cfg.Tenants[0].RateLimitRPS != 0 {
		t.Fatalf("loadtest profile expects rate_limit_rps 0, got %d", cfg.Tenants[0].RateLimitRPS)
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	cfgPath := writeTempConfig(t, `
server:
  port: 8080
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
    capabilities: [chat]
tenants:
  - id: t1
    class: premium
    priority: high
routing:
  default_backend: b1
reliability:
  fallback_enabled: true
  fallback_rules: []
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("expected config to load, got error: %v", err)
	}
	if cfg.Routing.DefaultBackend != "b1" {
		t.Fatalf("unexpected default backend: %s", cfg.Routing.DefaultBackend)
	}
	if cfg.Telemetry.Exporter != "log" {
		t.Fatalf("expected default telemetry exporter log, got %s", cfg.Telemetry.Exporter)
	}
	if cfg.Server.HealthCacheTTLMS != 2000 {
		t.Fatalf("expected default health_cache_ttl_ms 2000, got %d", cfg.Server.HealthCacheTTLMS)
	}
	if cfg.Server.HealthCheckPerMS != 1500 {
		t.Fatalf("expected default health_check_per_backend_ms 1500, got %d", cfg.Server.HealthCheckPerMS)
	}
}

func TestLoad_DuplicateBackendName(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: false
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "duplicate backend name") {
		t.Fatalf("expected duplicate backend validation error, got: %v", err)
	}
}

func TestLoad_DefaultBackendNotFound(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: missing
reliability:
  fallback_enabled: false
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "default backend") {
		t.Fatalf("expected default backend validation error, got: %v", err)
	}
}

func TestLoad_FallbackTargetNotFound(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: true
  fallback_rules:
    - from_backend: b1
      on: [timeout]
      fallback_to: missing
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "fallback_to backend") {
		t.Fatalf("expected fallback target validation error, got: %v", err)
	}
}

func TestLoad_DuplicateRoutingRuleName(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
  rules:
    - name: same-rule
      use_backend: b1
    - name: same-rule
      use_backend: b1
reliability:
  fallback_enabled: false
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "duplicate routing rule name") {
		t.Fatalf("expected duplicate routing rule name validation error, got: %v", err)
	}
}

func TestLoad_FallbackRuleRequiresTrigger(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: true
  fallback_rules:
    - from_backend: b1
      fallback_to: b1
      on: []
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "must define at least one trigger") {
		t.Fatalf("expected fallback trigger validation error, got: %v", err)
	}
}

func TestLoad_FallbackRuleInvalidTrigger(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: true
  fallback_rules:
    - from_backend: b1
      fallback_to: b1
      on: [not_a_real_trigger]
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "invalid trigger") {
		t.Fatalf("expected invalid fallback trigger validation error, got: %v", err)
	}
}

func TestLoad_UnsupportedTelemetryExporter(t *testing.T) {
	cfgPath := writeTempConfig(t, `
telemetry:
  exporter: unsupported-kind
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: false
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "unsupported telemetry exporter") {
		t.Fatalf("expected unsupported telemetry exporter error, got: %v", err)
	}
}

func TestLoad_OTLPHTTPRequiresEndpoint(t *testing.T) {
	cfgPath := writeTempConfig(t, `
telemetry:
  exporter: otlp-http
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: false
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "otlp_endpoint is required") {
		t.Fatalf("expected otlp endpoint validation error, got: %v", err)
	}
}

func TestLoad_DefaultExporterFromOTLPEndpoint(t *testing.T) {
	cfgPath := writeTempConfig(t, `
telemetry:
  otlp_endpoint: http://localhost:4318/v1/traces
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: false
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Telemetry.Exporter != "otlp-http" {
		t.Fatalf("expected default exporter otlp-http, got %s", cfg.Telemetry.Exporter)
	}
}

func TestLoad_InvalidOverloadAction(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: false
  overload:
    queue_limit: 10
    action: not-a-valid-action
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "overload.action") {
		t.Fatalf("expected overload.action validation error, got: %v", err)
	}
}

func TestLoad_UnsupportedBackendType(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: unknown_backend
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: false
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "unsupported backend type") {
		t.Fatalf("expected unsupported backend type error, got: %v", err)
	}
}

func TestLoad_GeminiBackendMinimal(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: gem
    type: gemini
    timeout_ms: 100
    api_key: AIza-test
    default_model: gemini-2.0-flash
    cost: { unit: 1, currency: credit }
    capabilities: [chat]
tenants:
  - id: t1
routing:
  default_backend: gem
reliability:
  fallback_enabled: false
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("expected config to load: %v", err)
	}
	b := cfg.Backends[0]
	if b.Endpoint != "" {
		t.Fatalf("expected empty endpoint for default base URL, got %q", b.Endpoint)
	}
	if b.DefaultModel != "gemini-2.0-flash" {
		t.Fatalf("default_model: %q", b.DefaultModel)
	}
}

func TestLoad_GeminiBackendMissingAPIKey(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: gem
    type: gemini
    timeout_ms: 100
    default_model: gemini-2.0-flash
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: gem
reliability:
  fallback_enabled: false
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "requires api_key") {
		t.Fatalf("expected gemini api_key validation error, got: %v", err)
	}
}

func TestLoad_GeminiBackendMissingDefaultModel(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: gem
    type: gemini
    timeout_ms: 100
    api_key: k
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: gem
reliability:
  fallback_enabled: false
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "requires default_model") {
		t.Fatalf("expected gemini default_model validation error, got: %v", err)
	}
}

func TestLoad_OpenAICompatibleBackend(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: oa
    type: openai_compatible
    endpoint: https://api.example.com
    timeout_ms: 100
    api_key: sk-test
    auth_header_name: ""
    health_path: /v1/models
    default_model: gpt-4o-mini
    headers:
      X-Test: "1"
    cost: { unit: 1, currency: credit }
    capabilities: [chat]
tenants:
  - id: t1
routing:
  default_backend: oa
reliability:
  fallback_enabled: false
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("expected config to load: %v", err)
	}
	b := cfg.Backends[0]
	if b.APIKey != "sk-test" {
		t.Fatalf("api_key: %q", b.APIKey)
	}
	if b.HealthPath != "/v1/models" {
		t.Fatalf("health_path: %q", b.HealthPath)
	}
	if b.DefaultModel != "gpt-4o-mini" {
		t.Fatalf("default_model: %q", b.DefaultModel)
	}
	if b.Headers["X-Test"] != "1" {
		t.Fatalf("headers: %v", b.Headers)
	}
}

func TestLoad_ExpandsEnvVarsInYAML(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-from-env")
	cfgPath := writeTempConfig(t, `
server:
  port: 8080
backends:
  - name: oa
    type: openai_compatible
    endpoint: https://api.example.com
    timeout_ms: 100
    api_key: ${OPENAI_API_KEY}
    health_path: /v1/models
    default_model: gpt-4o-mini
    cost: { unit: 1, currency: credit }
    capabilities: [chat]
tenants:
  - id: t1
routing:
  default_backend: oa
reliability:
  fallback_enabled: false
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("expected config to load: %v", err)
	}
	if cfg.Backends[0].APIKey != "sk-from-env" {
		t.Fatalf("api_key after expand: %q", cfg.Backends[0].APIKey)
	}
}

func TestLoad_ServerHTTPTimeoutsNegative(t *testing.T) {
	cfgPath := writeTempConfig(t, `
server:
  http:
    read_timeout_ms: -1
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: false
`)
	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "read_timeout_ms") {
		t.Fatalf("expected read_timeout_ms validation error, got: %v", err)
	}
}

func TestLoad_VLLMBackendRequiresEndpoint(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: vllm-b
    type: vllm
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: vllm-b
reliability:
  fallback_enabled: false
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "requires endpoint") {
		t.Fatalf("expected vllm endpoint required error, got: %v", err)
	}
}

func TestLoad_KnowledgeBaseFileMissingPath(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: false
knowledge_bases:
  - name: kb1
    type: file
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), `knowledge base "kb1" type file requires path`) {
		t.Fatalf("expected file KB path validation error, got: %v", err)
	}
}

func TestLoad_KnowledgeBaseHTTPMissingEndpoint(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: false
knowledge_bases:
  - name: kb1
    type: http
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), `knowledge base "kb1" type http requires endpoint`) {
		t.Fatalf("expected http KB endpoint validation error, got: %v", err)
	}
}

func TestLoad_KnowledgeBaseUnsupportedType(t *testing.T) {
	cfgPath := writeTempConfig(t, `
backends:
  - name: b1
    type: mock
    timeout_ms: 100
    cost: { unit: 1, currency: credit }
tenants:
  - id: t1
routing:
  default_backend: b1
reliability:
  fallback_enabled: false
knowledge_bases:
  - name: kb1
    type: not-a-real-type
`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), `unsupported type`) {
		t.Fatalf("expected unsupported knowledge base type error, got: %v", err)
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}
