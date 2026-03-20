package router

import (
	"context"
	"fmt"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/interfaces"
	"github.com/infercore/infercore/internal/types"
)

type RuleRouter struct {
	cfg        *config.Config
	costEngine interfaces.CostEngine
}

func NewRuleRouter(cfg *config.Config, costEngine interfaces.CostEngine) *RuleRouter {
	return &RuleRouter{
		cfg:        cfg,
		costEngine: costEngine,
	}
}

func (r *RuleRouter) SelectRoute(ctx context.Context, req types.AIRequest, state types.RuntimeState) (types.RouteDecision, error) {
	if err := ctx.Err(); err != nil {
		return types.RouteDecision{}, err
	}
	tenant, _ := r.cfg.TenantByID(req.TenantID)
	baseDecision, err := r.selectByRules(tenant, req, state)
	if err != nil {
		return types.RouteDecision{}, err
	}

	optimized := r.optimizeByCost(tenant, req, baseDecision, state)
	return optimized, nil
}

// BackendHealthOK mirrors routing semantics: unknown backends are treated as selectable.
func BackendHealthOK(health map[string]bool, backendName string) bool {
	if health == nil {
		return true
	}
	if healthy, ok := health[backendName]; ok {
		return healthy
	}
	return true
}

func backendSelectable(state types.RuntimeState, backendName string) bool {
	return BackendHealthOK(state.BackendHealth, backendName)
}

func (r *RuleRouter) selectByRules(tenant config.TenantConfig, req types.AIRequest, state types.RuntimeState) (types.RouteDecision, error) {
	_ = tenant

	for _, rule := range r.cfg.Routing.Rules {
		if !matchesRule(rule, tenant, req) {
			continue
		}

		backend, ok := r.cfg.BackendByName(rule.UseBackend)
		if !ok {
			return types.RouteDecision{}, fmt.Errorf("rule %q references unknown backend %q", rule.Name, rule.UseBackend)
		}
		if !backendSelectable(state, backend.Name) {
			continue
		}

		return types.RouteDecision{
			BackendName:   backend.Name,
			Reason:        rule.Name,
			EstimatedCost: backend.Cost.Unit,
		}, nil
	}

	backend, ok := r.cfg.BackendByName(r.cfg.Routing.DefaultBackend)
	if !ok {
		return types.RouteDecision{}, fmt.Errorf("default backend %q not found", r.cfg.Routing.DefaultBackend)
	}
	if backendSelectable(state, backend.Name) {
		return types.RouteDecision{
			BackendName:   backend.Name,
			Reason:        "default-backend",
			EstimatedCost: backend.Cost.Unit,
		}, nil
	}

	if d, ok := r.firstHealthyBackend(state); ok {
		return d, nil
	}

	return types.RouteDecision{}, fmt.Errorf("no healthy backend available")
}

func (r *RuleRouter) firstHealthyBackend(state types.RuntimeState) (types.RouteDecision, bool) {
	for _, backend := range r.cfg.Backends {
		if !backendSelectable(state, backend.Name) {
			continue
		}
		return types.RouteDecision{
			BackendName:   backend.Name,
			Reason:        "healthy-fallback-order",
			EstimatedCost: backend.Cost.Unit,
		}, true
	}
	return types.RouteDecision{}, false
}

func (r *RuleRouter) optimizeByCost(tenant config.TenantConfig, req types.AIRequest, base types.RouteDecision, state types.RuntimeState) types.RouteDecision {
	if r.costEngine == nil {
		return base
	}
	if state.OverloadDegrade {
		return base
	}

	best := base
	bestCost := base.EstimatedCost

	for _, backend := range r.cfg.Backends {
		if !backendSelectable(state, backend.Name) {
			continue
		}
		if !supportsTask(backend, req.TaskType) {
			continue
		}

		estimate := r.costEngine.Estimate(req, types.BackendMetadata{
			Name:           backend.Name,
			Type:           backend.Type,
			Capabilities:   backend.Capabilities,
			CostUnit:       backend.Cost.Unit,
			MaxConcurrency: backend.MaxConcurrency,
		})
		if tenant.BudgetPerRequest > 0 && estimate.EstimatedTotal > tenant.BudgetPerRequest {
			continue
		}

		if estimate.EstimatedTotal < bestCost {
			best = types.RouteDecision{
				BackendName:   backend.Name,
				Reason:        "cost-optimized-from-" + base.Reason,
				EstimatedCost: estimate.EstimatedTotal,
			}
			bestCost = estimate.EstimatedTotal
		}
	}

	return best
}

func supportsTask(backend config.BackendConfig, taskType string) bool {
	if taskType == "" {
		return true
	}

	switch taskType {
	case "simple":
		return hasCapability(backend.Capabilities, "chat") || hasCapability(backend.Capabilities, "summarization")
	case "complex":
		return hasCapability(backend.Capabilities, "reasoning")
	default:
		return hasCapability(backend.Capabilities, taskType)
	}
}

func hasCapability(capabilities []string, expected string) bool {
	for _, c := range capabilities {
		if c == expected {
			return true
		}
	}
	return false
}

func matchesRule(rule config.RouteRule, tenant config.TenantConfig, req types.AIRequest) bool {
	if rule.When.TaskType != "" && rule.When.TaskType != req.TaskType {
		return false
	}
	if rule.When.Priority != "" && rule.When.Priority != req.Priority {
		return false
	}
	if rule.When.TenantClass != "" && rule.When.TenantClass != tenant.Class {
		return false
	}
	return true
}
