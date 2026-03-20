package retrieval

import (
	"log"
	"strings"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/interfaces"
)

// NewRerankFromConfig returns the configured reranker. Only "noop" is implemented; unknown types fall back to noop.
func NewRerankFromConfig(cfg *config.Config) interfaces.RerankAdapter {
	if cfg == nil {
		return &NoopRerank{}
	}
	t := strings.ToLower(strings.TrimSpace(cfg.RAG.Rerank.Type))
	switch t {
	case "", "noop":
		return &NoopRerank{}
	default:
		log.Printf("event=rerank_unknown_type type=%q using noop", cfg.RAG.Rerank.Type)
		return &NoopRerank{}
	}
}
