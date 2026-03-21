package server

import (
	"context"
	"errors"
	"time"

	"github.com/infercore/infercore/internal/execution"
	"github.com/infercore/infercore/internal/inferexec"
	"github.com/infercore/infercore/internal/types"
)

type serverLedger struct{ s *Server }

func (l serverLedger) CreateRequest(ctx context.Context, traceID, requestID string, req types.AIRequest, now time.Time) {
	l.s.createLedgerRequest(ctx, traceID, requestID, req, now)
}

func (l serverLedger) Fail(ctx context.Context, requestID string) {
	l.s.updateLedgerFailed(ctx, requestID)
}

func (l serverLedger) UpdatePolicy(ctx context.Context, requestID string, snap []byte, routeReason, backend *string) {
	l.s.updateLedgerPolicy(ctx, requestID, snap, routeReason, backend)
}

func (l serverLedger) MarkSuccess(ctx context.Context, requestID, backend, routeReason string) {
	l.s.markLedgerSuccess(ctx, requestID, backend, routeReason)
}

func (s *Server) inferOrchestrator() *inferexec.Orchestrator {
	var ledger inferexec.Ledger
	if s.ledger != nil {
		ledger = serverLedger{s}
	}
	return &inferexec.Orchestrator{
		Policy:         s.policy,
		Router:         s.router,
		Reliability:    s.reliability,
		SLO:            s.sloEngine,
		Ledger:         ledger,
		CachedHealth:   s.cachedBackendHealth,
		InferInflight:  func() int32 { return s.inferInflight.Load() },
		BeginInferLoad: s.beginInferLoad,
		RunRAG: func(ctx context.Context, sw *execution.StepWriter, req *types.AIRequest) error {
			return s.runRAGPipeline(ctx, sw, req)
		},
		BuildFallback: s.buildFallback,
		NoteTimeout:   s.noteTimeoutForScaling,
		ParseRAGError: func(err error) (trace string, httpStatus int, errCode, msg string, ok bool) {
			var pe *ragPipelineError
			if errors.As(err, &pe) {
				return pe.trace, pe.httpStatus, pe.errCode, pe.msg, true
			}
			return "", 0, "", "", false
		},
	}
}
