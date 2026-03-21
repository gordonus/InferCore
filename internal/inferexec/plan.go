package inferexec

import (
	"github.com/infercore/infercore/internal/execution"
	"github.com/infercore/infercore/internal/types"
)

// ExecutionPhase is a logical pipeline phase aligned with ledger step names.
type ExecutionPhase string

const (
	PhaseNormalize   ExecutionPhase = execution.StepNormalize
	PhasePolicy      ExecutionPhase = execution.StepPolicyCheck
	PhaseAdmission   ExecutionPhase = execution.StepAdmission
	PhaseRoute       ExecutionPhase = execution.StepRoute
	PhaseRetrieve    ExecutionPhase = execution.StepRetrieve
	PhaseRerank      ExecutionPhase = execution.StepRerank
	PhaseBackendCall ExecutionPhase = execution.StepBackendCall
	PhaseFinalize    ExecutionPhase = execution.StepFinalize
	PhaseAgentStub   ExecutionPhase = execution.StepAgentStub
)

// ExecutionPlanForRequestType returns the ordered phases for a request type after policy normalization.
// Agent stops after stub; inference skips retrieve/rerank; RAG inserts retrieve+rerank before backend_call.
func ExecutionPlanForRequestType(requestType string) []ExecutionPhase {
	switch requestType {
	case types.RequestTypeAgent:
		return []ExecutionPhase{PhaseNormalize, PhasePolicy, PhaseAgentStub}
	case types.RequestTypeRAG:
		return []ExecutionPhase{
			PhaseNormalize, PhasePolicy, PhaseAdmission, PhaseRoute,
			PhaseRetrieve, PhaseRerank, PhaseBackendCall, PhaseFinalize,
		}
	default:
		return []ExecutionPhase{
			PhaseNormalize, PhasePolicy, PhaseAdmission, PhaseRoute,
			PhaseBackendCall, PhaseFinalize,
		}
	}
}
