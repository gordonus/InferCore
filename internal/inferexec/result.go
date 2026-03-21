package inferexec

import (
	"time"

	"github.com/infercore/infercore/internal/types"
)

// Result is the outcome of the infer execution pipeline (after JSON validation).
type Result struct {
	Success *Success
	Failure *Failure
}

// Success carries data needed to build the HTTP 200 AIResponse.
type Success struct {
	PolicyDecision  types.PolicyDecision
	Primary         types.RouteDecision
	ExecRes         types.ExecutionResult
	Req             types.AIRequest
	OverloadDegrade bool
	ExecStart       time.Time
}

// Failure carries HTTP error mapping and telemetry label.
type Failure struct {
	HTTPStatus  int
	ErrorCode   string
	Message     string
	TraceResult string
	Backend     string // for trace when relevant (e.g. primary on execution failure)
}
