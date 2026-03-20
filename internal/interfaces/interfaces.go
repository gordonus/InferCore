package interfaces

import (
	"context"
	"time"

	"github.com/infercore/infercore/internal/types"
)

type Router interface {
	SelectRoute(ctx context.Context, req types.AIRequest, state types.RuntimeState) (types.RouteDecision, error)
}

type PolicyEngine interface {
	Evaluate(ctx context.Context, req types.AIRequest) (types.PolicyDecision, error)
}

type BackendAdapter interface {
	Name() string
	Invoke(ctx context.Context, req types.BackendRequest) (types.BackendResponse, error)
	Health(ctx context.Context) error
	Metadata() types.BackendMetadata
}

type ReliabilityManager interface {
	ExecuteWithFallback(
		ctx context.Context,
		req types.AIRequest,
		primary types.RouteDecision,
		fallback []types.RouteDecision,
	) (types.ExecutionResult, error)
	ExecuteWithFallbackOpts(
		ctx context.Context,
		req types.AIRequest,
		primary types.RouteDecision,
		fallback []types.RouteDecision,
		opts types.ReliabilityExecuteOptions,
	) (types.ExecutionResult, error)
}

// RetrievalAdapter performs retrieval for RAG (v1.5).
type RetrievalAdapter interface {
	Name() string
	Retrieve(ctx context.Context, query string, opts map[string]any) (types.RetrievalResult, error)
}

// RerankAdapter reorders or filters retrieval chunks before the model call (optional RAG step).
type RerankAdapter interface {
	Name() string
	Rerank(ctx context.Context, query string, chunks []types.RetrievalChunk, opts map[string]any) (types.RetrievalResult, error)
}

type CostEngine interface {
	Estimate(req types.AIRequest, backend types.BackendMetadata) types.CostEstimate
}

type SLOEngine interface {
	RecordStart(requestID string)
	RecordFirstToken(requestID string, ts time.Time)
	RecordCompletion(requestID string, ts time.Time)
	RecordFallback(requestID string, reason string)
	Snapshot(requestID string) types.SLOSnapshot
}

type TelemetryExporter interface {
	EmitMetric(name string, value float64, labels map[string]string)
	EmitEvent(event types.Event)
	EmitTrace(trace types.TraceRecord)
}

type ScalingSignalProvider interface {
	CurrentSignals() types.ScalingSignals
}
