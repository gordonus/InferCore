package reliability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/fallback"
	"github.com/infercore/infercore/internal/interfaces"
	"github.com/infercore/infercore/internal/types"
	"github.com/infercore/infercore/internal/upstream"
)

var _ interfaces.ReliabilityManager = (*Manager)(nil)

type Manager struct {
	cfg      *config.Config
	adapters map[string]interfaces.BackendAdapter
}

func NewManager(cfg *config.Config, adapters map[string]interfaces.BackendAdapter) *Manager {
	return &Manager{
		cfg:      cfg,
		adapters: adapters,
	}
}

func (m *Manager) ExecuteWithFallback(
	ctx context.Context,
	req types.AIRequest,
	primary types.RouteDecision,
	fallback []types.RouteDecision,
) (types.ExecutionResult, error) {
	return m.ExecuteWithFallbackOpts(ctx, req, primary, fallback, types.ReliabilityExecuteOptions{})
}

func (m *Manager) ExecuteWithFallbackOpts(
	ctx context.Context,
	req types.AIRequest,
	primary types.RouteDecision,
	fallback []types.RouteDecision,
	opts types.ReliabilityExecuteOptions,
) (types.ExecutionResult, error) {
	if name := strings.TrimSpace(opts.ForcePrimaryBackend); name != "" {
		primary = types.RouteDecision{
			BackendName:   name,
			Reason:        "forced_primary",
			EstimatedCost: primary.EstimatedCost,
			FallbackChain: nil,
		}
		if opts.NoFallback {
			fallback = nil
		}
	}
	candidates := make([]types.RouteDecision, 0, 1+len(fallback))
	candidates = append(candidates, primary)
	candidates = append(candidates, fallback...)

	var lastErr error

	for i, decision := range candidates {
		adapter, ok := m.adapters[decision.BackendName]
		if !ok {
			lastErr = fmt.Errorf("adapter %q not found", decision.BackendName)
			continue
		}

		backendCfg, ok := m.cfg.BackendByName(decision.BackendName)
		if !ok {
			lastErr = fmt.Errorf("backend config %q not found", decision.BackendName)
			continue
		}

		invokeReq := req
		if i > 0 && !m.cfg.Reliability.StreamFallbackEnabled && req.Options.Stream {
			invokeReq = cloneAINonStream(req)
		}

		invokeCtx, cancel := context.WithTimeout(ctx, time.Duration(backendCfg.TimeoutMS)*time.Millisecond)
		resp, err := adapter.Invoke(invokeCtx, types.BackendRequest{AIRequest: invokeReq})
		cancel()
		if err == nil {
			return types.ExecutionResult{
				Status:       "success",
				BackendName:  decision.BackendName,
				Output:       resp.Output,
				UsedFallback: i > 0,
				Error:        nil,
				Timing:       resp.Timing,
			}, nil
		}

		lastErr = err
		if !m.shouldFallback(decision.BackendName, err) {
			break
		}
	}

	if lastErr == nil {
		lastErr = errors.New("request failed without a concrete error")
	}

	return types.ExecutionResult{
		Status:       "failed",
		BackendName:  primary.BackendName,
		Output:       nil,
		UsedFallback: len(fallback) > 0,
		Error:        lastErr,
	}, lastErr
}

func (m *Manager) shouldFallback(fromBackend string, err error) bool {
	if !m.cfg.Reliability.FallbackEnabled {
		return false
	}

	trigger := classifyError(err)
	for _, rule := range m.cfg.Reliability.FallbackRules {
		if rule.FromBackend != fromBackend {
			continue
		}
		for _, on := range rule.On {
			if on == trigger {
				return true
			}
		}
	}
	return false
}

func classifyError(err error) string {
	var upstreamErr *upstream.Error
	if errors.As(err, &upstreamErr) {
		switch upstreamErr.Kind {
		case upstream.KindTimeout:
			return fallback.TriggerTimeout
		case upstream.KindBackendUnhealthy:
			return fallback.TriggerBackendUnhealthy
		case upstream.KindUpstream4xx:
			return fallback.TriggerUpstream4xx
		case upstream.KindUpstream5xx:
			return fallback.TriggerUpstream5xx
		default:
			return fallback.TriggerBackendError
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return fallback.TriggerTimeout
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unhealthy") {
		return fallback.TriggerBackendUnhealthy
	}
	return fallback.TriggerBackendError
}

func cloneAINonStream(req types.AIRequest) types.AIRequest {
	out := req
	out.Options.Stream = false
	return out
}
