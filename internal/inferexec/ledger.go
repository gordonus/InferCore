package inferexec

import (
	"context"
	"time"

	"github.com/infercore/infercore/internal/types"
)

// Ledger persists request lifecycle updates for the infer pipeline.
type Ledger interface {
	CreateRequest(ctx context.Context, traceID, requestID string, req types.AIRequest, now time.Time)
	Fail(ctx context.Context, requestID string)
	UpdatePolicy(ctx context.Context, requestID string, snap []byte, routeReason, backend *string)
	MarkSuccess(ctx context.Context, requestID, backend, routeReason string)
}
