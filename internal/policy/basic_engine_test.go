package policy

import (
	"context"
	"testing"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

func testConfig() *config.Config {
	return &config.Config{
		Tenants: []config.TenantConfig{
			{
				ID:               "team-a",
				Class:            "premium",
				Priority:         "high",
				BudgetPerRequest: 5,
				RateLimitRPS:     1,
			},
		},
	}
}

func TestBasicEngine_AgentRejectedWhenFeatureOff(t *testing.T) {
	engine := NewBasicEngine(testConfig())
	decision, err := engine.Evaluate(context.Background(), types.AIRequest{
		TenantID:    "team-a",
		TaskType:    "simple",
		RequestType: types.RequestTypeAgent,
		Input:       map[string]any{"text": "x"},
		Options:     types.RequestOptions{MaxTokens: 128},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected agent request to be rejected when features.agent_enabled is false")
	}
}

func TestBasicEngine_UnknownTenantRejected(t *testing.T) {
	engine := NewBasicEngine(testConfig())
	decision, err := engine.Evaluate(context.Background(), types.AIRequest{
		TenantID: "missing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected request to be rejected")
	}
}

func TestBasicEngine_PriorityNormalized(t *testing.T) {
	engine := NewBasicEngine(testConfig())
	decision, err := engine.Evaluate(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
		Options:  types.RequestOptions{MaxTokens: 128},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected request to be allowed")
	}
	if decision.Normalized.Priority != "high" {
		t.Fatalf("expected normalized priority high, got %q", decision.Normalized.Priority)
	}
}

func TestBasicEngine_RateLimit(t *testing.T) {
	engine := NewBasicEngine(testConfig())

	first, _ := engine.Evaluate(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
		Options:  types.RequestOptions{MaxTokens: 128},
	})
	second, _ := engine.Evaluate(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
		Options:  types.RequestOptions{MaxTokens: 128},
	})

	if !first.Allowed {
		t.Fatalf("first request should pass")
	}
	if second.Allowed {
		t.Fatalf("second request should be rate-limited")
	}
}

func TestBasicEngine_BudgetExceeded(t *testing.T) {
	engine := NewBasicEngine(testConfig())
	decision, err := engine.Evaluate(context.Background(), types.AIRequest{
		TenantID: "team-a",
		TaskType: "simple",
		Options:  types.RequestOptions{MaxTokens: 2000},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected budget rejection")
	}
}
