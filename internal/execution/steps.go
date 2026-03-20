package execution

import (
	"context"
	"encoding/json"
	"time"

	"github.com/infercore/infercore/internal/requests"
)

// Step names for ledger and tracing (v1.5).
const (
	StepNormalize   = "normalize"
	StepPolicyCheck = "policy_check"
	StepAdmission   = "admission"
	StepRoute       = "route"
	StepRetrieve    = "retrieve"
	StepRerank      = "rerank"
	StepBackendCall = "backend_call"
	StepFinalize    = "finalize"
	StepAgentStub   = "agent_stub"
)

// StepWriter records execution steps to the request ledger.
type StepWriter struct {
	Store     requests.Store
	RequestID string
	TenantID  string
	// OnStep is called after each persisted step (success or failure). Optional.
	OnStep func(ctx context.Context, stepType, backend, status string, start time.Time, latencyMs int64)
	next   int
}

// Run executes fn and persists one ledger step when Store is non-nil.
// When ledger is disabled (Store nil), fn still runs; OnStep is invoked if set.
func (w *StepWriter) Run(ctx context.Context, stepType, backend string, input map[string]any, fn func() (output map[string]any, err error)) error {
	if w == nil {
		_, err := fn()
		return err
	}
	t0 := time.Now()
	inBytes, _ := json.Marshal(input)
	out, err := fn()
	lat := time.Since(t0).Milliseconds()
	status := "success"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}
	if w.OnStep != nil {
		w.OnStep(ctx, stepType, backend, status, t0, lat)
	}
	if w.Store == nil {
		return err
	}
	if out == nil {
		out = map[string]any{}
	}
	outBytes, _ := json.Marshal(out)
	meta := map[string]any{"step": stepType}
	if err != nil && stepType == StepPolicyCheck {
		meta["policy_rejected"] = true
	}
	metaBytes, _ := json.Marshal(meta)
	_ = w.Store.AppendStep(ctx, requests.StepRow{
		RequestID:    w.RequestID,
		StepIndex:    w.next,
		StepType:     stepType,
		InputJSON:    inBytes,
		OutputJSON:   outBytes,
		Backend:      backend,
		Status:       status,
		Error:        errMsg,
		LatencyMs:    lat,
		MetadataJSON: metaBytes,
	})
	w.next++
	return err
}

// Index returns the next step index without incrementing (for branching).
func (w *StepWriter) Index() int {
	if w == nil {
		return 0
	}
	return w.next
}
