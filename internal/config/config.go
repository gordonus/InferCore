package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/infercore/infercore/internal/fallback"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server          ServerConfig          `yaml:"server"`
	Telemetry       TelemetryConfig       `yaml:"telemetry"`
	SLO             SLOStoreConfig        `yaml:"slo"`
	Backends        []BackendConfig       `yaml:"backends"`
	Tenants         []TenantConfig        `yaml:"tenants"`
	Routing         RoutingConfig         `yaml:"routing"`
	Reliability     ReliabilityConfig     `yaml:"reliability"`
	Ledger          LedgerConfig          `yaml:"ledger"`
	Features        FeaturesConfig        `yaml:"features"`
	KnowledgeBases  []KnowledgeBaseConfig `yaml:"knowledge_bases"`
	PipelinesConfig PipelinesFileConfig   `yaml:"pipelines"`
	RAG             RAGConfig             `yaml:"rag"`
}

// RAGConfig holds optional RAG pipeline settings (rerank, etc.).
type RAGConfig struct {
	Rerank RerankConfig `yaml:"rerank"`
}

// RerankConfig selects rerank implementation (v1.5: noop only).
type RerankConfig struct {
	Type string `yaml:"type"` // noop | (future: cross_encoder, ...)
}

// FeaturesConfig toggles optional runtime surfaces (v1.5).
type FeaturesConfig struct {
	AgentEnabled bool `yaml:"agent_enabled"`
}

// LedgerConfig persists request ledger (SQLite, Postgres, or in-memory).
type LedgerConfig struct {
	Enabled bool   `yaml:"enabled"`
	Driver  string `yaml:"driver"` // memory | sqlite | postgres
	Path    string `yaml:"path"`   // sqlite file path when driver=sqlite
	DSN     string `yaml:"dsn"`    // postgres connection string when driver=postgres
}

// KnowledgeBaseConfig backs retrieval adapters (e.g. file-backed demo KB for RAG).
type KnowledgeBaseConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"` // file
	Path string `yaml:"path"`
}

// PipelinesFileConfig optional path to extra pipeline definitions (future); built-ins always exist.
type PipelinesFileConfig struct {
	ConfigPath string `yaml:"config_path"`
}

// SLOStoreConfig bounds in-memory SLO state (per request_id).
type SLOStoreConfig struct {
	MaxRecords int `yaml:"max_records"`
	MaxAgeMS   int `yaml:"max_age_ms"`
}

// ServerHTTPTimeouts maps to net/http.Server ReadTimeout, WriteTimeout, IdleTimeout.
// Zero values mean "derive from server.request_timeout_ms" (see server.HTTPLayerTimeouts).
type ServerHTTPTimeouts struct {
	ReadTimeoutMS  int `yaml:"read_timeout_ms"`
	WriteTimeoutMS int `yaml:"write_timeout_ms"`
	IdleTimeoutMS  int `yaml:"idle_timeout_ms"`
}

type ServerConfig struct {
	Host             string             `yaml:"host"`
	Port             int                `yaml:"port"`
	RequestTimeoutMS int                `yaml:"request_timeout_ms"`
	HTTP             ServerHTTPTimeouts `yaml:"http,omitempty"`
	HealthCacheTTLMS int                `yaml:"health_cache_ttl_ms"`
	HealthCheckPerMS int                `yaml:"health_check_per_backend_ms"`
	InfercoreAPIKey  string             `yaml:"infercore_api_key"`
}

type TelemetryConfig struct {
	MetricsEnabled bool   `yaml:"metrics_enabled"`
	TracingEnabled bool   `yaml:"tracing_enabled"`
	OTLPEndpoint   string `yaml:"otlp_endpoint"`
	OTLPTimeoutMS  int    `yaml:"otlp_timeout_ms"`
	OTLPRetries    int    `yaml:"otlp_retries"`
	OTLPBatchSize  int    `yaml:"otlp_batch_size"`
	OTLPFlushMS    int    `yaml:"otlp_flush_interval_ms"`
	LogLevel       string `yaml:"log_level"`
	Exporter       string `yaml:"exporter"`
}

type CostConfig struct {
	Unit     float64 `yaml:"unit"`
	Currency string  `yaml:"currency"`
}

type BackendConfig struct {
	Name           string            `yaml:"name"`
	Type           string            `yaml:"type"`
	Endpoint       string            `yaml:"endpoint"`
	TimeoutMS      int               `yaml:"timeout_ms"`
	MaxConcurrency int               `yaml:"max_concurrency"`
	Cost           CostConfig        `yaml:"cost"`
	Capabilities   []string          `yaml:"capabilities"`
	APIKey         string            `yaml:"api_key"`
	AuthHeaderName string            `yaml:"auth_header_name"`
	HealthPath     string            `yaml:"health_path"`
	DefaultModel   string            `yaml:"default_model"`
	Headers        map[string]string `yaml:"headers"`
}

type TenantConfig struct {
	ID               string  `yaml:"id"`
	Class            string  `yaml:"class"`
	Priority         string  `yaml:"priority"`
	BudgetPerRequest float64 `yaml:"budget_per_request"`
	RateLimitRPS     int     `yaml:"rate_limit_rps"`
	// Agent limits (v1.5 preview; enforced when request_type=agent).
	MaxSteps       int      `yaml:"max_steps"`
	MaxToolCalls   int      `yaml:"max_tool_calls"`
	MaxAgentCost   float64  `yaml:"max_agent_cost"`
	AllowedTools   []string `yaml:"allowed_tools"`
	AgentTimeoutMS int      `yaml:"agent_timeout_ms"`
}

type RouteWhen struct {
	TenantClass string `yaml:"tenant_class"`
	TaskType    string `yaml:"task_type"`
	Priority    string `yaml:"priority"`
}

type RouteRule struct {
	Name       string    `yaml:"name"`
	When       RouteWhen `yaml:"when"`
	UseBackend string    `yaml:"use_backend"`
}

type RoutingConfig struct {
	DefaultBackend string      `yaml:"default_backend"`
	Rules          []RouteRule `yaml:"rules"`
}

type FallbackRule struct {
	FromBackend string   `yaml:"from_backend"`
	On          []string `yaml:"on"`
	FallbackTo  string   `yaml:"fallback_to"`
}

type OverloadConfig struct {
	QueueLimit int    `yaml:"queue_limit"`
	Action     string `yaml:"action"`
}

type ReliabilityConfig struct {
	FallbackEnabled       bool           `yaml:"fallback_enabled"`
	FallbackRules         []FallbackRule `yaml:"fallback_rules"`
	Overload              OverloadConfig `yaml:"overload"`
	StreamFallbackEnabled bool           `yaml:"stream_fallback_enabled"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.RequestTimeoutMS <= 0 {
		cfg.Server.RequestTimeoutMS = 3000
	}
	if cfg.Server.HealthCacheTTLMS <= 0 {
		cfg.Server.HealthCacheTTLMS = 2000
	}
	if cfg.Server.HealthCheckPerMS <= 0 {
		cfg.Server.HealthCheckPerMS = 1500
	}
	if cfg.Telemetry.LogLevel == "" {
		cfg.Telemetry.LogLevel = "info"
	}
	if cfg.Telemetry.Exporter == "" {
		if strings.TrimSpace(cfg.Telemetry.OTLPEndpoint) != "" {
			cfg.Telemetry.Exporter = "otlp-http"
		} else {
			cfg.Telemetry.Exporter = "log"
		}
	}
	if cfg.Telemetry.OTLPTimeoutMS <= 0 {
		cfg.Telemetry.OTLPTimeoutMS = 1000
	}
	if cfg.Telemetry.OTLPRetries < 0 {
		cfg.Telemetry.OTLPRetries = 0
	}
	if cfg.Telemetry.OTLPBatchSize <= 0 {
		cfg.Telemetry.OTLPBatchSize = 10
	}
	if cfg.Telemetry.OTLPFlushMS <= 0 {
		cfg.Telemetry.OTLPFlushMS = 1000
	}
	if cfg.Routing.DefaultBackend == "" && len(cfg.Backends) > 0 {
		cfg.Routing.DefaultBackend = cfg.Backends[0].Name
	}
	if cfg.Reliability.Overload.QueueLimit <= 0 {
		cfg.Reliability.Overload.QueueLimit = 200
	}
	if cfg.Reliability.Overload.Action == "" {
		cfg.Reliability.Overload.Action = "degrade"
	}
	if cfg.SLO.MaxRecords <= 0 {
		cfg.SLO.MaxRecords = 10000
	}
	if cfg.SLO.MaxAgeMS <= 0 {
		cfg.SLO.MaxAgeMS = 600000
	}
	if strings.TrimSpace(cfg.Ledger.Driver) == "" {
		cfg.Ledger.Driver = "memory"
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

const maxHTTPTimeoutMS = 86400000 // 24h

func (c *Config) validateServerHTTPTimeouts() error {
	check := func(field string, ms int) error {
		if ms < 0 {
			return fmt.Errorf("config validation: %s cannot be negative", field)
		}
		if ms > maxHTTPTimeoutMS {
			return fmt.Errorf("config validation: %s exceeds 24h", field)
		}
		return nil
	}
	if err := check("server.http.read_timeout_ms", c.Server.HTTP.ReadTimeoutMS); err != nil {
		return err
	}
	if err := check("server.http.write_timeout_ms", c.Server.HTTP.WriteTimeoutMS); err != nil {
		return err
	}
	return check("server.http.idle_timeout_ms", c.Server.HTTP.IdleTimeoutMS)
}

func (c *Config) validate() error {
	if err := c.validateServerHTTPTimeouts(); err != nil {
		return err
	}
	if len(c.Backends) == 0 {
		return fmt.Errorf("config validation: at least one backend is required")
	}

	backendNames := make(map[string]struct{}, len(c.Backends))
	for _, b := range c.Backends {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			return fmt.Errorf("config validation: backend name cannot be empty")
		}
		switch b.Type {
		case "mock":
		case "vllm", "openai", "openai_compatible":
			if strings.TrimSpace(b.Endpoint) == "" {
				return fmt.Errorf("config validation: backend %q type %q requires endpoint", b.Name, b.Type)
			}
		case "gemini":
			// endpoint optional; empty uses https://generativelanguage.googleapis.com in adapter
			if strings.TrimSpace(b.APIKey) == "" {
				return fmt.Errorf("config validation: backend %q type gemini requires api_key", b.Name)
			}
			if strings.TrimSpace(b.DefaultModel) == "" {
				return fmt.Errorf("config validation: backend %q type gemini requires default_model", b.Name)
			}
		default:
			return fmt.Errorf("config validation: unsupported backend type %q for backend %q", b.Type, b.Name)
		}
		if _, exists := backendNames[name]; exists {
			return fmt.Errorf("config validation: duplicate backend name %q", name)
		}
		backendNames[name] = struct{}{}
	}

	tenantIDs := make(map[string]struct{}, len(c.Tenants))
	for _, t := range c.Tenants {
		id := strings.TrimSpace(t.ID)
		if id == "" {
			return fmt.Errorf("config validation: tenant id cannot be empty")
		}
		if _, exists := tenantIDs[id]; exists {
			return fmt.Errorf("config validation: duplicate tenant id %q", id)
		}
		tenantIDs[id] = struct{}{}
	}

	if _, ok := backendNames[c.Routing.DefaultBackend]; !ok {
		return fmt.Errorf("config validation: default backend %q not found", c.Routing.DefaultBackend)
	}

	ruleNames := make(map[string]struct{}, len(c.Routing.Rules))
	for _, rule := range c.Routing.Rules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("config validation: routing rule name cannot be empty")
		}
		if _, exists := ruleNames[rule.Name]; exists {
			return fmt.Errorf("config validation: duplicate routing rule name %q", rule.Name)
		}
		ruleNames[rule.Name] = struct{}{}
		if _, ok := backendNames[rule.UseBackend]; !ok {
			return fmt.Errorf("config validation: routing rule %q references unknown backend %q", rule.Name, rule.UseBackend)
		}
	}

	act := strings.ToLower(strings.TrimSpace(c.Reliability.Overload.Action))
	if act == "" {
		act = "degrade"
	}
	if act != "reject" && act != "degrade" {
		return fmt.Errorf("config validation: reliability.overload.action must be reject or degrade, got %q", c.Reliability.Overload.Action)
	}

	for _, rule := range c.Reliability.FallbackRules {
		if _, ok := backendNames[rule.FromBackend]; !ok {
			return fmt.Errorf("config validation: fallback from_backend %q not found", rule.FromBackend)
		}
		if _, ok := backendNames[rule.FallbackTo]; !ok {
			return fmt.Errorf("config validation: fallback_to backend %q not found", rule.FallbackTo)
		}
		if len(rule.On) == 0 {
			return fmt.Errorf("config validation: fallback rule from %q to %q must define at least one trigger", rule.FromBackend, rule.FallbackTo)
		}
		for _, trigger := range rule.On {
			if !fallback.IsValidTrigger(trigger) {
				return fmt.Errorf("config validation: fallback rule from %q has invalid trigger %q", rule.FromBackend, trigger)
			}
		}
	}

	switch c.Telemetry.Exporter {
	case "log", "otlp-http-stub", "otlp-http", "otlp-http-json":
		// supported
	default:
		return fmt.Errorf("config validation: unsupported telemetry exporter %q", c.Telemetry.Exporter)
	}
	if (c.Telemetry.Exporter == "otlp-http" || c.Telemetry.Exporter == "otlp-http-json") && strings.TrimSpace(c.Telemetry.OTLPEndpoint) == "" {
		return fmt.Errorf("config validation: otlp_endpoint is required for telemetry exporter %q", c.Telemetry.Exporter)
	}

	drv := strings.ToLower(strings.TrimSpace(c.Ledger.Driver))
	switch drv {
	case "memory", "sqlite", "postgres":
	default:
		return fmt.Errorf("config validation: ledger.driver must be memory, sqlite, or postgres, got %q", c.Ledger.Driver)
	}
	if c.Ledger.Enabled && drv == "sqlite" && strings.TrimSpace(c.Ledger.Path) == "" {
		return fmt.Errorf("config validation: ledger.path is required when ledger.enabled=true and driver=sqlite")
	}
	if c.Ledger.Enabled && drv == "postgres" && strings.TrimSpace(c.Ledger.DSN) == "" {
		return fmt.Errorf("config validation: ledger.dsn is required when ledger.enabled=true and driver=postgres")
	}

	for _, kb := range c.KnowledgeBases {
		if strings.TrimSpace(kb.Name) == "" {
			return fmt.Errorf("config validation: knowledge_bases entry name cannot be empty")
		}
		typ := strings.ToLower(strings.TrimSpace(kb.Type))
		if typ != "file" {
			return fmt.Errorf("config validation: knowledge base %q unsupported type %q (only file)", kb.Name, kb.Type)
		}
		if strings.TrimSpace(kb.Path) == "" {
			return fmt.Errorf("config validation: knowledge base %q requires path", kb.Name)
		}
	}

	return nil
}

func (c *Config) TenantByID(id string) (TenantConfig, bool) {
	for _, t := range c.Tenants {
		if t.ID == id {
			return t, true
		}
	}
	return TenantConfig{}, false
}

func (c *Config) BackendByName(name string) (BackendConfig, bool) {
	for _, b := range c.Backends {
		if b.Name == name {
			return b, true
		}
	}
	return BackendConfig{}, false
}

// KnowledgeBaseByName returns a configured KB by name.
func (c *Config) KnowledgeBaseByName(name string) (KnowledgeBaseConfig, bool) {
	for _, kb := range c.KnowledgeBases {
		if kb.Name == name {
			return kb, true
		}
	}
	return KnowledgeBaseConfig{}, false
}
