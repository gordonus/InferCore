package retrieval

import (
	"context"
	"testing"

	"github.com/infercore/infercore/internal/types"
)

func TestNoopRerank_Passthrough(t *testing.T) {
	var r NoopRerank
	chunks := []types.RetrievalChunk{
		{Text: "a", Source: "s1", Score: 0.9},
		{Text: "b", Source: "s2", Score: 0.1},
	}
	out, err := r.Rerank(context.Background(), "q", chunks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Chunks) != 2 {
		t.Fatalf("chunks: got %d want 2", len(out.Chunks))
	}
	if out.Chunks[0].Text != "a" || out.Chunks[1].Text != "b" {
		t.Fatalf("order changed: %+v", out.Chunks)
	}
	if r.Name() != "noop" {
		t.Fatalf("name: %q", r.Name())
	}
}
