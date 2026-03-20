package retrieval

import (
	"context"

	"github.com/infercore/infercore/internal/types"
)

// NoopRerank is a pass-through reranker: returns chunks unchanged (default / placeholder).
type NoopRerank struct{}

// Name implements RerankAdapter.
func (NoopRerank) Name() string { return "noop" }

// Rerank implements RerankAdapter.
func (NoopRerank) Rerank(_ context.Context, _ string, chunks []types.RetrievalChunk, _ map[string]any) (types.RetrievalResult, error) {
	out := make([]types.RetrievalChunk, len(chunks))
	copy(out, chunks)
	return types.RetrievalResult{Chunks: out}, nil
}
