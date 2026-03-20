package types

import "strings"

// NormalizeAIRequest fills defaults for extended AI Request fields.
func NormalizeAIRequest(req AIRequest) AIRequest {
	out := req
	rt := strings.ToLower(strings.TrimSpace(out.RequestType))
	switch rt {
	case "", RequestTypeInference:
		out.RequestType = RequestTypeInference
	case RequestTypeRAG, RequestTypeAgent:
		out.RequestType = rt
	default:
		out.RequestType = rt
	}
	if strings.TrimSpace(out.PipelineRef) == "" {
		switch out.RequestType {
		case RequestTypeRAG:
			out.PipelineRef = DefaultPipelineRAG
		case RequestTypeAgent:
			out.PipelineRef = DefaultPipelineAgent
		default:
			out.PipelineRef = DefaultPipelineInference
		}
	}
	if out.Context == nil {
		out.Context = map[string]any{}
	}
	return out
}
