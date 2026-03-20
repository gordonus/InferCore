package cost

import (
	"github.com/infercore/infercore/internal/types"
)

type SimpleEngine struct{}

func NewSimpleEngine() *SimpleEngine {
	return &SimpleEngine{}
}

func (e *SimpleEngine) Estimate(req types.AIRequest, backend types.BackendMetadata) types.CostEstimate {
	maxTokens := req.Options.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 256
	}

	estimatedTotal := backend.CostUnit + (float64(maxTokens)/1000.0)*backend.CostUnit
	return types.CostEstimate{
		UnitCost:       backend.CostUnit,
		EstimatedTotal: estimatedTotal,
		BudgetFit:      true,
	}
}
