package types

// AI request classification (v1.5). Empty RequestType defaults to inference.
const (
	RequestTypeInference = "inference"
	RequestTypeRAG       = "rag"
	RequestTypeAgent     = "agent"
)

// Default pipeline refs when the client omits pipeline_ref.
const (
	DefaultPipelineInference = "inference/basic:v1"
	DefaultPipelineRAG       = "rag/basic:v1"
	DefaultPipelineAgent     = "agent/basic:v0"
)

type RequestOptions struct {
	Stream    bool `json:"stream"`
	MaxTokens int  `json:"max_tokens"`
}

// AIRequest is the unified ingress body for inference, RAG, and agent (preview) flows on POST /infer.
type AIRequest struct {
	RequestID   string         `json:"request_id,omitempty"`
	RequestType string         `json:"request_type,omitempty"` // inference | rag | agent
	PipelineRef string         `json:"pipeline_ref,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
	TenantID    string         `json:"tenant_id"`
	TaskType    string         `json:"task_type"`
	Priority    string         `json:"priority"`
	Input       map[string]any `json:"input"`
	Options     RequestOptions `json:"options"`
}

// PolicySnapshot captures routing/policy inputs at decision time for replay and audit.
type PolicySnapshot struct {
	PolicyReason       string  `json:"policy_reason"`
	PrimaryBackend     string  `json:"primary_backend"`
	PrimaryRouteReason string  `json:"primary_route_reason"`
	EstimatedCost      float64 `json:"estimated_cost"`
	OverloadDegrade    bool    `json:"overload_degrade"`
	QueueDepth         int     `json:"queue_depth"`
}

type RouteDecision struct {
	BackendName   string   `json:"backend_name"`
	Reason        string   `json:"reason"`
	EstimatedCost float64  `json:"estimated_cost"`
	FallbackChain []string `json:"fallback_chain"`
}

type ExecutionResult struct {
	Status       string         `json:"status"`
	BackendName  string         `json:"backend_name"`
	Output       map[string]any `json:"output"`
	UsedFallback bool           `json:"used_fallback"`
	Error        error          `json:"error,omitempty"`
	Timing       *BackendTiming `json:"timing,omitempty"`
}

// BackendTiming is measured inside the adapter (wall clock from invoke start).
type BackendTiming struct {
	TTFTMs              int64 `json:"ttft_ms"`
	CompletionLatencyMs int64 `json:"completion_latency_ms"`
	TPOTMs              int64 `json:"tpot_ms"`
	Streamed            bool  `json:"streamed"`
}

type SLOSnapshot struct {
	TTFTMs              int64 `json:"ttft_ms"`
	TPOTMs              int64 `json:"tpot_ms"`
	CompletionLatencyMs int64 `json:"completion_latency_ms"`
	FallbackTriggered   bool  `json:"fallback_triggered"`
}

// AIMetrics are returned on successful AIRequest handling.
type AIMetrics struct {
	TTFTMs               int64   `json:"ttft_ms"`
	TPOTMs               int64   `json:"tpot_ms"`
	CompletionLatencyMs  int64   `json:"completion_latency_ms"`
	EstimatedCost        float64 `json:"estimated_cost"`
	QueueWaitTimeMs      int64   `json:"queue_wait_time_ms"`
	ServerReceivedAtUnix int64   `json:"server_received_at_unix_ms"`
}

type FallbackState struct {
	Triggered bool   `json:"triggered"`
	Reason    string `json:"reason,omitempty"`
}

type DegradeState struct {
	Triggered bool   `json:"triggered"`
	Reason    string `json:"reason,omitempty"`
}

// AIResponse is the success JSON body for POST /infer.
type AIResponse struct {
	RequestID         string         `json:"request_id"`
	TraceID           string         `json:"trace_id,omitempty"`
	RequestType       string         `json:"request_type,omitempty"`
	PipelineRef       string         `json:"pipeline_ref,omitempty"`
	SelectedBackend   string         `json:"selected_backend"`
	RouteReason       string         `json:"route_reason"`
	PolicyReason      string         `json:"policy_reason,omitempty"`
	EffectivePriority string         `json:"effective_priority,omitempty"`
	Status            string         `json:"status"`
	Result            map[string]any `json:"result"`
	Metrics           AIMetrics      `json:"metrics"`
	Fallback          FallbackState  `json:"fallback"`
	Degrade           DegradeState   `json:"degrade"`
}

type PolicyDecision struct {
	Allowed    bool
	Reason     string
	Normalized AIRequest
}

type RuntimeState struct {
	QueueDepth         int
	BackendHealth      map[string]bool
	RecentTimeoutSpike bool
	// OverloadDegrade is set when infer concurrency is at/above configured limit and action is "degrade" (router may skip cost optimization).
	OverloadDegrade bool
}

// BackendRequest wraps AIRequest for model adapters (embedding exposes Input, Options, etc.).
type BackendRequest struct {
	AIRequest
}

type BackendResponse struct {
	Output map[string]any
	Timing *BackendTiming
}

type BackendMetadata struct {
	Name           string
	Type           string
	Capabilities   []string
	CostUnit       float64
	MaxConcurrency int
}

type CostEstimate struct {
	UnitCost       float64
	EstimatedTotal float64
	BudgetFit      bool
}

type Event struct {
	Name      string
	Timestamp int64
	Labels    map[string]string
}

type TraceRecord struct {
	TraceID   string
	SpanID    string
	Name      string
	StartUnix int64
	EndUnix   int64
	Labels    map[string]string
}

type ScalingSignals struct {
	QueueDepth             int      `json:"queue_depth"`
	TimeoutSpike           bool     `json:"timeout_spike"`
	TTFTDegradationRatio   float64  `json:"ttft_degradation_ratio"`
	RecentFallbackRate     float64  `json:"recent_fallback_rate"`
	BackendSaturationHints []string `json:"backend_saturation_hints"`
}

// ReliabilityExecuteOptions tweaks backend execution (e.g. replay-exact forcing).
type ReliabilityExecuteOptions struct {
	ForcePrimaryBackend string
	NoFallback          bool
}

// RetrievalChunk is one retrieved passage for RAG.
type RetrievalChunk struct {
	Text   string            `json:"text"`
	Source string            `json:"source,omitempty"`
	Score  float64           `json:"score,omitempty"`
	Extra  map[string]string `json:"extra,omitempty"`
}

// RetrievalResult is the output of a retrieval adapter.
type RetrievalResult struct {
	Chunks []RetrievalChunk `json:"chunks"`
}
