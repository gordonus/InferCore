package retrieval

import (
	"strings"

	"github.com/infercore/infercore/internal/types"
)

// MergeRetrievalIntoInput writes retrieval/rerank output into req.Input (retrieved_context, retrieved_chunks).
func MergeRetrievalIntoInput(req *types.AIRequest, res types.RetrievalResult) {
	var parts []string
	var metas []any
	for _, c := range res.Chunks {
		parts = append(parts, c.Text)
		metas = append(metas, map[string]any{"text": c.Text, "source": c.Source, "score": c.Score})
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}
	req.Input["retrieved_context"] = strings.Join(parts, "\n\n")
	req.Input["retrieved_chunks"] = metas
}
