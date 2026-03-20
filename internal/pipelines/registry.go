package pipelines

import (
	"strings"

	"github.com/infercore/infercore/internal/types"
)

// Ref constants for built-in pipelines.
const (
	InferenceBasicV1 = types.DefaultPipelineInference
	RAGBasicV1       = types.DefaultPipelineRAG
	AgentBasicV0     = types.DefaultPipelineAgent
)

// Kind describes a coarse pipeline category.
type Kind string

const (
	KindInference Kind = "inference"
	KindRAG       Kind = "rag"
	KindAgent     Kind = "agent"
)

// Classify returns pipeline kind from ref or request type.
func Classify(requestType, ref string) Kind {
	rt := strings.ToLower(strings.TrimSpace(requestType))
	switch rt {
	case types.RequestTypeRAG:
		return KindRAG
	case types.RequestTypeAgent:
		return KindAgent
	default:
		r := strings.ToLower(strings.TrimSpace(ref))
		if strings.Contains(r, "rag/") {
			return KindRAG
		}
		if strings.Contains(r, "agent/") {
			return KindAgent
		}
		return KindInference
	}
}
