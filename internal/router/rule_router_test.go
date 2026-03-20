package router

import (
	"context"
	"testing"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/cost"
	"github.com/infercore/infercore/internal/types"
)

func testRouterConfig() *config.Config {
	return &config.Config{
		Backends: []config.BackendConfig{
			{
				Name:         "small-model",
				Cost:         config.CostConfig{Unit: 1},
				Capabilities: []string{"chat", "summarization"},
			},
			{
				Name:         "large-model",
				Cost:         config.CostConfig{Unit: 5},
				Capabilities: []string{"chat", "reasoning", "summarization"},
			},
		},
		Tenants: []config.TenantConfig{
			{ID: "team-a", Class: "premium", BudgetPerRequest: 10},
		},
		Routing: config.RoutingConfig{
			DefaultBackend: "small-model",
			Rules: []config.RouteRule{
				{
					Name: "premium-simple-expensive-route",
					When: config.RouteWhen{
						TenantClass: "premium",
						TaskType:    "simple",
					},
					UseBackend: "large-model",
				},
			},
		},
	}
}

func TestRuleRouter_UsesRuleMatch(t *testing.T) {
	r := NewRuleRouter(testRouterConfig(), nil)
	decision, err := r.SelectRoute(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
	}, types.RuntimeState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.BackendName != "large-model" {
		t.Fatalf("expected large-model, got %s", decision.BackendName)
	}
	if decision.Reason != "premium-simple-expensive-route" {
		t.Fatalf("unexpected reason: %s", decision.Reason)
	}
}

func TestRuleRouter_FallsBackToDefault(t *testing.T) {
	r := NewRuleRouter(testRouterConfig(), nil)
	decision, err := r.SelectRoute(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "translation",
	}, types.RuntimeState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.BackendName != "small-model" {
		t.Fatalf("expected small-model, got %s", decision.BackendName)
	}
}

func TestRuleRouter_CostOptimizationPrefersCheaperCompatibleBackend(t *testing.T) {
	r := NewRuleRouter(testRouterConfig(), cost.NewSimpleEngine())
	decision, err := r.SelectRoute(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
		Options:  types.RequestOptions{MaxTokens: 128},
	}, types.RuntimeState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both backends are considered compatible in current router logic for this scenario.
	if decision.BackendName != "small-model" {
		t.Fatalf("expected cost optimization to pick small-model, got %s", decision.BackendName)
	}
}

func TestRuleRouter_OverloadDegradeSkipsCostOptimization(t *testing.T) {
	r := NewRuleRouter(testRouterConfig(), cost.NewSimpleEngine())
	decision, err := r.SelectRoute(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
		Options:  types.RequestOptions{MaxTokens: 128},
	}, types.RuntimeState{OverloadDegrade: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.BackendName != "large-model" {
		t.Fatalf("expected rule-selected large-model when overload degrades routing, got %s", decision.BackendName)
	}
}

func TestRuleRouter_SkipsUnhealthyRuleBackendUsesDefault(t *testing.T) {
	r := NewRuleRouter(testRouterConfig(), nil)
	state := types.RuntimeState{
		BackendHealth: map[string]bool{
			"large-model": false,
			"small-model": true,
		},
	}
	decision, err := r.SelectRoute(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
	}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.BackendName != "small-model" {
		t.Fatalf("expected default small-model when rule backend unhealthy, got %s", decision.BackendName)
	}
	if decision.Reason != "default-backend" {
		t.Fatalf("expected default-backend reason, got %s", decision.Reason)
	}
}

func TestRuleRouter_DefaultUnhealthyPicksFirstHealthyInConfigOrder(t *testing.T) {
	cfg := testRouterConfig()
	cfg.Routing.DefaultBackend = "large-model"
	r := NewRuleRouter(cfg, nil)
	state := types.RuntimeState{
		BackendHealth: map[string]bool{
			"large-model": false,
			"small-model": true,
		},
	}
	decision, err := r.SelectRoute(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "translation",
	}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.BackendName != "small-model" {
		t.Fatalf("expected healthy small-model, got %s", decision.BackendName)
	}
	if decision.Reason != "healthy-fallback-order" {
		t.Fatalf("expected healthy-fallback-order, got %s", decision.Reason)
	}
}

func TestRuleRouter_AllUnhealthyReturnsError(t *testing.T) {
	r := NewRuleRouter(testRouterConfig(), nil)
	state := types.RuntimeState{
		BackendHealth: map[string]bool{
			"large-model": false,
			"small-model": false,
		},
	}
	_, err := r.SelectRoute(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
	}, state)
	if err == nil {
		t.Fatalf("expected error when no healthy backend")
	}
}

func TestRuleRouter_CostOptimizationSkipsUnhealthyCheaperBackend(t *testing.T) {
	r := NewRuleRouter(testRouterConfig(), cost.NewSimpleEngine())
	state := types.RuntimeState{
		BackendHealth: map[string]bool{
			"large-model": true,
			"small-model": false,
		},
	}
	decision, err := r.SelectRoute(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
		Options:  types.RequestOptions{MaxTokens: 128},
	}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.BackendName != "large-model" {
		t.Fatalf("expected large-model when small is unhealthy, got %s", decision.BackendName)
	}
}

func TestRuleRouter_ContextCanceled(t *testing.T) {
	r := NewRuleRouter(testRouterConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.SelectRoute(ctx, types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
	}, types.RuntimeState{})
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}
