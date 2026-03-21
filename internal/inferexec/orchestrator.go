package inferexec

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/execution"
	"github.com/infercore/infercore/internal/interfaces"
	"github.com/infercore/infercore/internal/types"
)

// Orchestrator runs the infer control-plane pipeline after HTTP JSON validation.
type Orchestrator struct {
	Policy         interfaces.PolicyEngine
	Router         interfaces.Router
	Reliability    interfaces.ReliabilityManager
	SLO            interfaces.SLOEngine
	Config         *config.Config
	Ledger         Ledger
	CachedHealth   func(ctx context.Context) map[string]bool
	InferInflight  func() int32
	BeginInferLoad func() (release func(), overloadDegrade, reject bool)
	RunRAG         func(ctx context.Context, sw *execution.StepWriter, req *types.AIRequest) error
	BuildFallback  func(primary string, health map[string]bool) []types.RouteDecision
	NoteTimeout    func(err error)
	ParseRAGError  func(err error) (trace string, httpStatus int, errCode, msg string, ok bool)
}

// RunInput carries mutable request state and observability hooks for one infer invocation.
type RunInput struct {
	TraceID          string
	RequestID        string
	ReceivedAtUnixMs int64
	Req              *types.AIRequest
	SW               *execution.StepWriter
	EmitRequestTrace func(backend, result string)
}

// Run executes normalize → policy → (agent stub?) → admission → route → (RAG?) → backend → finalize.
func (o *Orchestrator) Run(ctx context.Context, in RunInput) *Result {
	_ = ExecutionPlanForRequestType(in.Req.RequestType)

	req := in.Req
	requestID := in.RequestID
	sw := in.SW
	emit := in.EmitRequestTrace
	if emit == nil {
		emit = func(string, string) {}
	}

	ledgerAt := time.Now()
	if o.Ledger != nil {
		o.Ledger.CreateRequest(ctx, in.TraceID, requestID, *req, ledgerAt)
	}

	_ = sw.Run(ctx, execution.StepNormalize, "", map[string]any{
		"request_type": req.RequestType, "pipeline_ref": req.PipelineRef,
	}, func() (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})

	policyDecision, err := o.Policy.Evaluate(ctx, *req)
	if err != nil {
		o.failLedger(ctx, requestID)
		if st, ec, ok := InferBudgetHTTPStatus(ctx, err); ok {
			if o.NoteTimeout != nil {
				o.NoteTimeout(err)
			}
			emit("", "gateway_timeout")
			return &Result{Failure: &Failure{HTTPStatus: st, ErrorCode: ec, Message: "inference request deadline exceeded", TraceResult: "gateway_timeout"}}
		}
		emit("", "policy_error")
		return &Result{Failure: &Failure{HTTPStatus: http.StatusInternalServerError, ErrorCode: "policy_error", Message: err.Error(), TraceResult: "policy_error"}}
	}

	if err := sw.Run(ctx, execution.StepPolicyCheck, "", map[string]any{"tenant_id": req.TenantID}, func() (map[string]any, error) {
		if !policyDecision.Allowed {
			return map[string]any{"allowed": false, "reason": policyDecision.Reason}, fmt.Errorf("policy rejected: %s", policyDecision.Reason)
		}
		return map[string]any{"allowed": true, "reason": policyDecision.Reason}, nil
	}); err != nil {
		log.Printf("event=policy_rejected request_id=%s tenant_id=%s reason=%q", requestID, req.TenantID, policyDecision.Reason)
		o.failLedger(ctx, requestID)
		emit("", "policy_rejected")
		return &Result{Failure: &Failure{HTTPStatus: http.StatusTooManyRequests, ErrorCode: "policy_rejected", Message: policyDecision.Reason, TraceResult: "policy_rejected"}}
	}
	*req = policyDecision.Normalized

	if req.RequestType == types.RequestTypeAgent {
		_ = sw.Run(ctx, execution.StepAgentStub, "", map[string]any{"pipeline": req.PipelineRef}, func() (map[string]any, error) {
			return nil, fmt.Errorf("agent execution not implemented")
		})
		o.failLedger(ctx, requestID)
		emit("", "agent_not_implemented")
		return &Result{Failure: &Failure{HTTPStatus: http.StatusNotImplemented, ErrorCode: "agent_not_implemented", Message: "agent execution is not implemented in this release", TraceResult: "agent_not_implemented"}}
	}

	if o.BeginInferLoad == nil || o.CachedHealth == nil || o.BuildFallback == nil {
		return &Result{Failure: &Failure{HTTPStatus: http.StatusInternalServerError, ErrorCode: "route_error", Message: "orchestrator misconfigured", TraceResult: "route_error"}}
	}

	release, overloadDegrade, rejectOverload := o.BeginInferLoad()
	if rejectOverload {
		log.Printf("event=overload_rejected request_id=%s tenant_id=%s", requestID, req.TenantID)
		o.failLedger(ctx, requestID)
		emit("", "overload")
		return &Result{Failure: &Failure{HTTPStatus: http.StatusServiceUnavailable, ErrorCode: "overload", Message: "inference concurrency limit exceeded", TraceResult: "overload"}}
	}
	defer release()

	_ = sw.Run(ctx, execution.StepAdmission, "", map[string]any{"queue_depth": o.InferInflight()}, func() (map[string]any, error) {
		return map[string]any{"overload_degrade": overloadDegrade}, nil
	})

	health := o.CachedHealth(ctx)
	primary, err := o.Router.SelectRoute(ctx, *req, types.RuntimeState{
		QueueDepth:      int(o.InferInflight()),
		BackendHealth:   health,
		OverloadDegrade: overloadDegrade,
	})
	if err != nil {
		o.failLedger(ctx, requestID)
		if st, ec, ok := InferBudgetHTTPStatus(ctx, err); ok {
			if o.NoteTimeout != nil {
				o.NoteTimeout(err)
			}
			emit("", "gateway_timeout")
			return &Result{Failure: &Failure{HTTPStatus: st, ErrorCode: ec, Message: "inference request deadline exceeded", TraceResult: "gateway_timeout"}}
		}
		emit("", "route_error")
		return &Result{Failure: &Failure{HTTPStatus: http.StatusInternalServerError, ErrorCode: "route_error", Message: err.Error(), TraceResult: "route_error"}}
	}

	_ = sw.Run(ctx, execution.StepRoute, primary.BackendName, map[string]any{"reason": primary.Reason}, func() (map[string]any, error) {
		return map[string]any{"backend": primary.BackendName, "estimated_cost": primary.EstimatedCost}, nil
	})

	snap := types.PolicySnapshot{
		PolicyReason:       policyDecision.Reason,
		PrimaryBackend:     primary.BackendName,
		PrimaryRouteReason: primary.Reason,
		EstimatedCost:      primary.EstimatedCost,
		OverloadDegrade:    overloadDegrade,
		QueueDepth:         int(o.InferInflight()),
	}
	snapBytes, _ := json.Marshal(snap)
	rr := primary.Reason
	sb := primary.BackendName
	if o.Ledger != nil {
		o.Ledger.UpdatePolicy(ctx, requestID, snapBytes, &rr, &sb)
	}

	if req.RequestType == types.RequestTypeRAG && o.RunRAG != nil {
		if ragErr := o.RunRAG(ctx, sw, req); ragErr != nil {
			o.failLedger(ctx, requestID)
			if o.ParseRAGError != nil {
				if trace, st, code, msg, ok := o.ParseRAGError(ragErr); ok {
					emit("", trace)
					return &Result{Failure: &Failure{HTTPStatus: st, ErrorCode: code, Message: msg, TraceResult: trace}}
				}
			}
			emit("", "execution_failed")
			return &Result{Failure: &Failure{HTTPStatus: http.StatusBadGateway, ErrorCode: "execution_failed", Message: ragErr.Error(), TraceResult: "execution_failed"}}
		}
	}

	fallback := o.BuildFallback(primary.BackendName, health)
	start := time.Now()
	if o.SLO != nil {
		o.SLO.RecordStart(requestID)
	}
	var execRes types.ExecutionResult
	err = sw.Run(ctx, execution.StepBackendCall, primary.BackendName, map[string]any{"primary": primary.BackendName}, func() (map[string]any, error) {
		var e error
		execRes, e = o.Reliability.ExecuteWithFallback(ctx, *req, primary, fallback)
		if e != nil {
			return map[string]any{"status": execRes.Status}, e
		}
		return map[string]any{"status": execRes.Status, "backend": execRes.BackendName}, nil
	})
	if err != nil {
		if o.NoteTimeout != nil {
			o.NoteTimeout(err)
		}
		o.failLedger(ctx, requestID)
		if st, ec, ok := InferBudgetHTTPStatus(ctx, err); ok {
			log.Printf("event=infer_deadline request_id=%s tenant_id=%s backend=%s", requestID, req.TenantID, primary.BackendName)
			emit(primary.BackendName, "gateway_timeout")
			return &Result{Failure: &Failure{HTTPStatus: st, ErrorCode: ec, Message: "inference request deadline exceeded", TraceResult: "gateway_timeout", Backend: primary.BackendName}}
		}
		log.Printf("event=execution_failed request_id=%s tenant_id=%s backend=%s error=%q", requestID, req.TenantID, primary.BackendName, err.Error())
		emit(primary.BackendName, "execution_failed")
		return &Result{Failure: &Failure{HTTPStatus: http.StatusBadGateway, ErrorCode: "execution_failed", Message: err.Error(), TraceResult: "execution_failed", Backend: primary.BackendName}}
	}

	_ = sw.Run(ctx, execution.StepFinalize, execRes.BackendName, map[string]any{"status": execRes.Status}, func() (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})
	if o.Ledger != nil {
		o.Ledger.MarkSuccess(ctx, requestID, execRes.BackendName, primary.Reason)
	}

	return &Result{Success: &Success{
		PolicyDecision:  policyDecision,
		Primary:         primary,
		ExecRes:         execRes,
		Req:             *req,
		OverloadDegrade: overloadDegrade,
		ExecStart:       start,
	}}
}

func (o *Orchestrator) failLedger(ctx context.Context, requestID string) {
	if o.Ledger != nil {
		o.Ledger.Fail(ctx, requestID)
	}
}
