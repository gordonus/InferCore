package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/cost"
	"github.com/infercore/infercore/internal/interfaces"
	"github.com/infercore/infercore/internal/policy"
	"github.com/infercore/infercore/internal/reliability"
	"github.com/infercore/infercore/internal/requests"
	"github.com/infercore/infercore/internal/retrieval"
	"github.com/infercore/infercore/internal/router"
	"github.com/infercore/infercore/internal/types"
)

// Mode selects replay strategy.
type Mode string

const (
	ModeExact   Mode = "exact"
	ModeCurrent Mode = "current"
)

// Dependencies wires control-plane components for offline replay (no HTTP server).
type Dependencies struct {
	Adapters    map[string]interfaces.BackendAdapter
	Retrieval   map[string]interfaces.RetrievalAdapter
	Rerank      interfaces.RerankAdapter
	Policy      interfaces.PolicyEngine
	Router      interfaces.Router
	Reliability interfaces.ReliabilityManager
}

// Replay reconstructs an AI request from the ledger and re-executes it.
func Replay(ctx context.Context, cfg *config.Config, store requests.Store, requestID string, mode Mode, deps Dependencies) (types.AIResponse, error) {
	if store == nil {
		return types.AIResponse{}, errors.New("ledger store is nil")
	}
	row, err := store.GetRequest(ctx, requestID)
	if err != nil {
		return types.AIResponse{}, err
	}
	var req types.AIRequest
	if len(row.AIRequestJSON) > 0 {
		if err := json.Unmarshal(row.AIRequestJSON, &req); err != nil {
			return types.AIResponse{}, fmt.Errorf("decode ai_request_json: %w", err)
		}
	} else {
		if err := json.Unmarshal(row.InputJSON, &req.Input); err != nil {
			return types.AIResponse{}, fmt.Errorf("decode input: %w", err)
		}
		if err := json.Unmarshal(row.ContextJSON, &req.Context); err != nil {
			return types.AIResponse{}, fmt.Errorf("decode context: %w", err)
		}
	}
	req.RequestID = row.RequestID
	req.TenantID = row.TenantID
	req.TaskType = row.TaskType
	req.Priority = row.Priority
	req.RequestType = row.RequestType
	req.PipelineRef = row.PipelineRef
	req = types.NormalizeAIRequest(req)

	switch mode {
	case ModeExact:
		return replayExact(ctx, cfg, req, row, deps)
	case ModeCurrent:
		return replayCurrent(ctx, cfg, req, deps)
	default:
		return types.AIResponse{}, fmt.Errorf("unknown replay mode %q", mode)
	}
}

func replayExact(ctx context.Context, cfg *config.Config, req types.AIRequest, row requests.RequestRow, deps Dependencies) (types.AIResponse, error) {
	var snap types.PolicySnapshot
	if len(row.PolicySnapshot) > 0 {
		_ = json.Unmarshal(row.PolicySnapshot, &snap)
	}
	backend := strings.TrimSpace(snap.PrimaryBackend)
	if backend == "" {
		backend = strings.TrimSpace(row.SelectedBackend)
	}
	if backend == "" {
		return types.AIResponse{}, errors.New("exact replay: missing primary backend in ledger snapshot")
	}

	if req.RequestType == types.RequestTypeRAG {
		if err := runRAGRetrieve(ctx, cfg, &req, deps.Retrieval, deps.Rerank); err != nil {
			return types.AIResponse{}, err
		}
	}

	primary := types.RouteDecision{
		BackendName:   backend,
		Reason:        "replay_exact",
		EstimatedCost: snap.EstimatedCost,
	}
	execRes, err := deps.Reliability.ExecuteWithFallbackOpts(ctx, req, primary, nil, types.ReliabilityExecuteOptions{
		ForcePrimaryBackend: backend,
		NoFallback:          true,
	})
	if err != nil {
		return types.AIResponse{}, err
	}
	return buildResponse(cfg, req, "replay_exact", primary, execRes, row.RequestID, row.TraceID), nil
}

func replayCurrent(ctx context.Context, cfg *config.Config, req types.AIRequest, deps Dependencies) (types.AIResponse, error) {
	pol, err := deps.Policy.Evaluate(ctx, req)
	if err != nil {
		return types.AIResponse{}, err
	}
	if !pol.Allowed {
		return types.AIResponse{}, fmt.Errorf("policy rejected: %s", pol.Reason)
	}
	req = pol.Normalized
	if req.RequestType == types.RequestTypeAgent {
		return types.AIResponse{}, errors.New("agent execution not implemented")
	}

	health := allHealthy(cfg)
	primary, err := deps.Router.SelectRoute(ctx, req, types.RuntimeState{
		QueueDepth:      0,
		BackendHealth:   health,
		OverloadDegrade: false,
	})
	if err != nil {
		return types.AIResponse{}, err
	}

	if req.RequestType == types.RequestTypeRAG {
		if err := runRAGRetrieve(ctx, cfg, &req, deps.Retrieval, deps.Rerank); err != nil {
			return types.AIResponse{}, err
		}
	}

	fallback := buildFallbackList(cfg, primary.BackendName, health)
	execRes, err := deps.Reliability.ExecuteWithFallback(ctx, req, primary, fallback)
	if err != nil {
		return types.AIResponse{}, err
	}
	return buildResponse(cfg, req, pol.Reason, primary, execRes, req.RequestID, ""), nil
}

func allHealthy(cfg *config.Config) map[string]bool {
	out := make(map[string]bool, len(cfg.Backends))
	for _, b := range cfg.Backends {
		out[b.Name] = true
	}
	return out
}

func buildFallbackList(cfg *config.Config, primary string, health map[string]bool) []types.RouteDecision {
	// Mirror server.buildFallback logic via router package helper would duplicate; inline minimal.
	var out []types.RouteDecision
	for _, rule := range cfg.Reliability.FallbackRules {
		if rule.FromBackend != primary {
			continue
		}
		backendCfg, ok := cfg.BackendByName(rule.FallbackTo)
		if !ok {
			continue
		}
		if !router.BackendHealthOK(health, backendCfg.Name) {
			continue
		}
		out = append(out, types.RouteDecision{
			BackendName:   backendCfg.Name,
			Reason:        "fallback-from-" + primary,
			EstimatedCost: backendCfg.Cost.Unit,
		})
	}
	return out
}

func runRAGRetrieve(ctx context.Context, cfg *config.Config, req *types.AIRequest, ret map[string]interfaces.RetrievalAdapter, rerank interfaces.RerankAdapter) error {
	kb := ""
	if req.Context != nil {
		if v, ok := req.Context["knowledge_base"].(string); ok {
			kb = strings.TrimSpace(v)
		}
	}
	if kb == "" && cfg != nil && len(cfg.KnowledgeBases) > 0 {
		kb = cfg.KnowledgeBases[0].Name
	}
	ad, ok := ret[kb]
	if !ok || kb == "" {
		return errors.New("rag replay: knowledge base not available")
	}
	q := ""
	if req.Context != nil {
		if v, ok := req.Context["query"].(string); ok {
			q = strings.TrimSpace(v)
		}
	}
	if q == "" && req.Input != nil {
		if t, ok := req.Input["text"].(string); ok {
			q = strings.TrimSpace(t)
		}
	}
	if q == "" {
		return errors.New("rag replay: missing query")
	}
	res, err := ad.Retrieve(ctx, q, nil)
	if err != nil {
		return err
	}
	if rerank == nil {
		rerank = retrieval.NewRerankFromConfig(nil)
	}
	out, err := rerank.Rerank(ctx, q, res.Chunks, nil)
	if err != nil {
		return err
	}
	retrieval.MergeRetrievalIntoInput(req, out)
	return nil
}

func buildResponse(cfg *config.Config, req types.AIRequest, policyReason string, primary types.RouteDecision, exec types.ExecutionResult, requestID, traceID string) types.AIResponse {
	chosenBackendCfg, _ := cfg.BackendByName(exec.BackendName)
	latency := int64(0)
	return types.AIResponse{
		RequestID:         requestID,
		TraceID:           traceID,
		RequestType:       req.RequestType,
		PipelineRef:       req.PipelineRef,
		SelectedBackend:   exec.BackendName,
		RouteReason:       primary.Reason,
		PolicyReason:      policyReason,
		EffectivePriority: req.Priority,
		Status:            exec.Status,
		Result:            exec.Output,
		Metrics: types.AIMetrics{
			CompletionLatencyMs:  latency,
			EstimatedCost:        chosenBackendCfg.Cost.Unit,
			ServerReceivedAtUnix: time.Now().UnixMilli(),
		},
		Fallback: types.FallbackState{Triggered: exec.UsedFallback},
		Degrade:  types.DegradeState{},
	}
}

// NewDependenciesFromConfig builds policy/router/reliability for CLI replay.
func NewDependenciesFromConfig(cfg *config.Config, adapters map[string]interfaces.BackendAdapter, retrievalAdapters map[string]interfaces.RetrievalAdapter) Dependencies {
	ce := cost.NewSimpleEngine()
	return Dependencies{
		Adapters:    adapters,
		Retrieval:   retrievalAdapters,
		Rerank:      retrieval.NewRerankFromConfig(cfg),
		Policy:      policy.NewBasicEngine(cfg),
		Router:      router.NewRuleRouter(cfg, ce),
		Reliability: reliability.NewManager(cfg, adapters),
	}
}
