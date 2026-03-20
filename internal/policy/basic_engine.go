package policy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

type BasicEngine struct {
	cfg *config.Config

	mu           sync.Mutex
	windowSecond int64
	tenantCounts map[string]int
}

func NewBasicEngine(cfg *config.Config) *BasicEngine {
	return &BasicEngine{
		cfg:          cfg,
		tenantCounts: map[string]int{},
	}
}

func (e *BasicEngine) Evaluate(_ context.Context, req types.AIRequest) (types.PolicyDecision, error) {
	tenant, ok := e.cfg.TenantByID(req.TenantID)
	if !ok {
		return types.PolicyDecision{
			Allowed: false,
			Reason:  "unknown tenant",
		}, nil
	}

	normalized := req
	if normalized.Priority == "" {
		normalized.Priority = tenant.Priority
	}
	normalized = types.NormalizeAIRequest(normalized)

	if strings.EqualFold(strings.TrimSpace(normalized.RequestType), types.RequestTypeAgent) && !e.cfg.Features.AgentEnabled {
		return types.PolicyDecision{
			Allowed:    false,
			Reason:     "agent requests are disabled (set features.agent_enabled=true to allow policy checks)",
			Normalized: normalized,
		}, nil
	}

	if err := validateAgentTenantLimits(normalized, tenant); err != nil {
		return types.PolicyDecision{
			Allowed:    false,
			Reason:     err.Error(),
			Normalized: normalized,
		}, nil
	}

	estimated := estimateBudgetConsumption(normalized)
	if tenant.BudgetPerRequest > 0 && estimated > tenant.BudgetPerRequest {
		return types.PolicyDecision{
			Allowed:    false,
			Reason:     fmt.Sprintf("budget exceeded: estimated=%.2f budget=%.2f", estimated, tenant.BudgetPerRequest),
			Normalized: normalized,
		}, nil
	}

	if !e.allowByRateLimit(tenant.ID, tenant.RateLimitRPS) {
		return types.PolicyDecision{
			Allowed:    false,
			Reason:     "rate limit exceeded",
			Normalized: normalized,
		}, nil
	}

	return types.PolicyDecision{
		Allowed:    true,
		Reason:     "allowed",
		Normalized: normalized,
	}, nil
}

func (e *BasicEngine) allowByRateLimit(tenantID string, limit int) bool {
	if limit <= 0 {
		return true
	}

	nowSec := time.Now().Unix()
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.windowSecond != nowSec {
		e.windowSecond = nowSec
		e.tenantCounts = map[string]int{}
	}

	current := e.tenantCounts[tenantID]
	if current >= limit {
		return false
	}

	e.tenantCounts[tenantID] = current + 1
	return true
}

func validateAgentTenantLimits(req types.AIRequest, tenant config.TenantConfig) error {
	if !strings.EqualFold(strings.TrimSpace(req.RequestType), types.RequestTypeAgent) {
		return nil
	}
	if req.Context == nil {
		return nil
	}
	if tenant.MaxSteps > 0 {
		if v, ok := toInt(req.Context["max_steps"]); ok && v > tenant.MaxSteps {
			return fmt.Errorf("agent max_steps %d exceeds tenant limit %d", v, tenant.MaxSteps)
		}
	}
	if tenant.MaxToolCalls > 0 {
		if v, ok := toInt(req.Context["max_tool_calls"]); ok && v > tenant.MaxToolCalls {
			return fmt.Errorf("agent max_tool_calls %d exceeds tenant limit %d", v, tenant.MaxToolCalls)
		}
	}
	if tenant.MaxAgentCost > 0 {
		if v, ok := toFloat(req.Context["estimated_agent_cost"]); ok && v > tenant.MaxAgentCost {
			return fmt.Errorf("estimated agent cost %.2f exceeds tenant limit %.2f", v, tenant.MaxAgentCost)
		}
	}
	if len(tenant.AllowedTools) > 0 {
		raw, ok := req.Context["tools"].([]any)
		if ok && len(raw) > 0 {
			allowed := make(map[string]struct{}, len(tenant.AllowedTools))
			for _, t := range tenant.AllowedTools {
				allowed[strings.TrimSpace(t)] = struct{}{}
			}
			for _, x := range raw {
				s, _ := x.(string)
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if _, ok := allowed[s]; !ok {
					return fmt.Errorf("tool %q is not in tenant allowed_tools", s)
				}
			}
		}
	}
	if tenant.AgentTimeoutMS > 0 {
		if v, ok := toInt(req.Context["agent_timeout_ms"]); ok && v > tenant.AgentTimeoutMS {
			return fmt.Errorf("agent_timeout_ms %d exceeds tenant limit %d", v, tenant.AgentTimeoutMS)
		}
	}
	return nil
}

func toInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	default:
		return 0, false
	}
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	default:
		return 0, false
	}
}

func estimateBudgetConsumption(req types.AIRequest) float64 {
	maxTokens := req.Options.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 256
	}
	return 1.0 + float64(maxTokens)/256.0
}
