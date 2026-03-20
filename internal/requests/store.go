package requests

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("request not found")

// Store persists requests and steps (ledger).
type Store interface {
	CreateRequest(ctx context.Context, row RequestRow) error
	UpdateRequest(ctx context.Context, requestID string, patch RequestPatch) error
	AppendStep(ctx context.Context, step StepRow) error
	GetRequest(ctx context.Context, requestID string) (RequestRow, error)
	ListSteps(ctx context.Context, requestID string) ([]StepRow, error)
	Close() error
}

// RequestPatch updates mutable request fields.
type RequestPatch struct {
	Status          *string
	SelectedBackend *string
	RouteReason     *string
	PolicySnapshot  []byte
	UpdatedAt       time.Time
}
