package reliability

import (
	"context"
	"testing"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/interfaces"
	"github.com/infercore/infercore/internal/types"
	"github.com/infercore/infercore/internal/upstream"
)

type timeoutAdapter struct{ name string }

func (a timeoutAdapter) Name() string { return a.name }

func (timeoutAdapter) Invoke(context.Context, types.BackendRequest) (types.BackendResponse, error) {
	return types.BackendResponse{}, upstream.New(upstream.KindTimeout, "simulated")
}

func (timeoutAdapter) Health(context.Context) error { return nil }

func (a timeoutAdapter) Metadata() types.BackendMetadata { return types.BackendMetadata{Name: a.name} }

type streamCaptureAdapter struct {
	name       string
	lastStream *bool
}

func (a *streamCaptureAdapter) Name() string { return a.name }

func (a *streamCaptureAdapter) Invoke(_ context.Context, req types.BackendRequest) (types.BackendResponse, error) {
	if a.lastStream != nil {
		*a.lastStream = req.Options.Stream
	}
	return types.BackendResponse{
		Output: map[string]any{"ok": true},
		Timing: &types.BackendTiming{TTFTMs: 1, CompletionLatencyMs: 2},
	}, nil
}

func (a *streamCaptureAdapter) Health(context.Context) error { return nil }

func (a *streamCaptureAdapter) Metadata() types.BackendMetadata {
	return types.BackendMetadata{Name: a.name}
}

func TestExecuteWithFallback_ForcesNonStreamOnFallbackWhenDisabled(t *testing.T) {
	cfg := &config.Config{
		Backends: []config.BackendConfig{
			{Name: "primary", Type: "mock", TimeoutMS: 5000, Cost: config.CostConfig{Unit: 1}},
			{Name: "backup", Type: "mock", TimeoutMS: 5000, Cost: config.CostConfig{Unit: 1}},
		},
		Routing: config.RoutingConfig{DefaultBackend: "primary"},
		Reliability: config.ReliabilityConfig{
			FallbackEnabled:       true,
			StreamFallbackEnabled: false,
			FallbackRules: []config.FallbackRule{
				{FromBackend: "primary", On: []string{"timeout"}, FallbackTo: "backup"},
			},
		},
	}
	var sawStream bool
	backup := &streamCaptureAdapter{name: "backup", lastStream: &sawStream}
	m := NewManager(cfg, map[string]interfaces.BackendAdapter{
		"primary": timeoutAdapter{name: "primary"},
		"backup":  backup,
	})

	_, err := m.ExecuteWithFallback(context.Background(), types.AIRequest{
		TenantID: "t",
		TaskType: "simple",
		Input:    map[string]any{"text": "x"},
		Options:  types.RequestOptions{Stream: true, MaxTokens: 10},
	}, types.RouteDecision{BackendName: "primary"},
		[]types.RouteDecision{{BackendName: "backup", Reason: "fb"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sawStream {
		t.Fatalf("expected stream=false on fallback when stream_fallback_enabled=false")
	}
}
