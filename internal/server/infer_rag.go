package server

import (
	"context"
	"net/http"

	"github.com/infercore/infercore/internal/execution"
	"github.com/infercore/infercore/internal/retrieval"
	"github.com/infercore/infercore/internal/types"
)

// ragPipelineError carries HTTP mapping for RAG pipeline failures (config vs execution).
type ragPipelineError struct {
	trace      string
	httpStatus int
	errCode    string
	msg        string
}

func (e *ragPipelineError) Error() string {
	if e == nil {
		return ""
	}
	return e.msg
}

// runRAGPipeline runs retrieve + rerank for request_type=rag. req is updated in place (merged chunks).
func (s *Server) runRAGPipeline(ctx context.Context, sw *execution.StepWriter, req *types.AIRequest) error {
	kb := kbName(*req, s.cfg)
	if kb == "" || len(s.retrieval) == 0 {
		return &ragPipelineError{trace: "rag_not_configured", httpStatus: http.StatusBadRequest, errCode: errCodeRAGNotConfigured, msg: "configure knowledge_bases and set context.knowledge_base or use the first KB"}
	}
	ad, ok := s.retrieval[kb]
	if !ok {
		return &ragPipelineError{trace: "rag_not_configured", httpStatus: http.StatusBadRequest, errCode: errCodeRAGNotConfigured, msg: "unknown knowledge_base " + kb}
	}
	q := ragQuery(*req)
	if q == "" {
		return &ragPipelineError{trace: "rag_not_configured", httpStatus: http.StatusBadRequest, errCode: errCodeInvalidRequest, msg: "rag requires input.text or context.query"}
	}
	var retrieveRes types.RetrievalResult
	err := sw.Run(ctx, execution.StepRetrieve, ad.Name(), map[string]any{"query": q, "kb": kb}, func() (map[string]any, error) {
		res, e := ad.Retrieve(ctx, q, nil)
		if e != nil {
			return nil, e
		}
		retrieveRes = res
		return map[string]any{"chunks": len(res.Chunks)}, nil
	})
	if err != nil {
		return &ragPipelineError{trace: "retrieve_failed", httpStatus: http.StatusBadGateway, errCode: errCodeExecutionFailed, msg: err.Error()}
	}
	err = sw.Run(ctx, execution.StepRerank, s.rerank.Name(), map[string]any{"query": q, "chunks_in": len(retrieveRes.Chunks)}, func() (map[string]any, error) {
		out, e := s.rerank.Rerank(ctx, q, retrieveRes.Chunks, nil)
		if e != nil {
			return nil, e
		}
		retrieval.MergeRetrievalIntoInput(req, out)
		return map[string]any{"chunks_out": len(out.Chunks)}, nil
	})
	if err != nil {
		return &ragPipelineError{trace: "rerank_failed", httpStatus: http.StatusBadGateway, errCode: errCodeExecutionFailed, msg: err.Error()}
	}
	return nil
}
