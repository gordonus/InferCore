package requests

import (
	"encoding/json"
	"time"
)

// RequestRow is a persisted AI request (ledger).
type RequestRow struct {
	RequestID   string
	TraceID     string
	RequestType string
	TenantID    string
	TaskType    string
	Priority    string
	PipelineRef string
	InputJSON   []byte
	ContextJSON []byte
	// AIRequestJSON is the full normalized AIRequest body at ledger create time (replay/audit).
	AIRequestJSON   []byte
	PolicySnapshot  []byte
	Status          string
	SelectedBackend string
	RouteReason     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// StepRow is one execution step for a request.
type StepRow struct {
	RequestID    string
	StepIndex    int
	StepType     string
	InputJSON    []byte
	OutputJSON   []byte
	Backend      string
	Status       string
	Error        string
	LatencyMs    int64
	MetadataJSON []byte
}

// MarshalJSONMap encodes map[string]any to JSON bytes.
func MarshalJSONMap(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}
