package mock

import (
	"context"
	"fmt"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

type Adapter struct {
	cfg config.BackendConfig
}

func New(cfg config.BackendConfig) *Adapter {
	return &Adapter{cfg: cfg}
}

func (a *Adapter) Name() string {
	return a.cfg.Name
}

func (a *Adapter) Invoke(ctx context.Context, req types.BackendRequest) (types.BackendResponse, error) {
	start := time.Now()
	// Simulate small processing latency.
	delay := 20 * time.Millisecond
	select {
	case <-ctx.Done():
		return types.BackendResponse{}, ctx.Err()
	case <-time.After(delay):
	}

	text, _ := req.Input["text"].(string)
	if text == "" {
		text = "No text provided."
	}

	end := time.Now()
	latency := end.Sub(start).Milliseconds()
	return types.BackendResponse{
		Output: map[string]any{
			"text":    fmt.Sprintf("[%s] %s", a.cfg.Name, text),
			"backend": a.cfg.Name,
		},
		Timing: &types.BackendTiming{
			TTFTMs:              latency,
			CompletionLatencyMs: latency,
			Streamed:            false,
		},
	}, nil
}

func (a *Adapter) Health(_ context.Context) error {
	return nil
}

func (a *Adapter) Metadata() types.BackendMetadata {
	return types.BackendMetadata{
		Name:           a.cfg.Name,
		Type:           a.cfg.Type,
		Capabilities:   a.cfg.Capabilities,
		CostUnit:       a.cfg.Cost.Unit,
		MaxConcurrency: a.cfg.MaxConcurrency,
	}
}
