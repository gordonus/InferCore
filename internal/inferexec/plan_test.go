package inferexec

import (
	"testing"

	"github.com/infercore/infercore/internal/types"
)

func TestExecutionPlanForRequestType(t *testing.T) {
	inf := ExecutionPlanForRequestType(types.RequestTypeInference)
	if len(inf) < 4 || inf[0] != PhaseNormalize || inf[len(inf)-1] != PhaseFinalize {
		t.Fatalf("inference plan: %v", inf)
	}
	rag := ExecutionPlanForRequestType(types.RequestTypeRAG)
	var hasRetrieve bool
	for _, p := range rag {
		if p == PhaseRetrieve {
			hasRetrieve = true
		}
	}
	if !hasRetrieve {
		t.Fatalf("rag plan missing retrieve: %v", rag)
	}
	ag := ExecutionPlanForRequestType(types.RequestTypeAgent)
	if len(ag) != 3 || ag[2] != PhaseAgentStub {
		t.Fatalf("agent plan: %v", ag)
	}
}
