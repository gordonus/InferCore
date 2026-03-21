package retrieval

import (
	"log"
	"strings"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/interfaces"
)

// FromConfig builds retrieval adapters from knowledge_bases.
func FromConfig(cfg *config.Config) map[string]interfaces.RetrievalAdapter {
	out := make(map[string]interfaces.RetrievalAdapter)
	if cfg == nil {
		return out
	}
	for _, kb := range cfg.KnowledgeBases {
		typ := strings.ToLower(strings.TrimSpace(kb.Type))
		switch typ {
		case "file":
			a, err := NewFileKB(kb.Name, kb.Path)
			if err != nil {
				log.Printf("event=retrieval_init_failed name=%s type=file err=%v", kb.Name, err)
				continue
			}
			out[kb.Name] = a
		case "http":
			a, err := NewHTTPJSONKB(kb)
			if err != nil {
				log.Printf("event=retrieval_init_failed name=%s type=http err=%v", kb.Name, err)
				continue
			}
			out[kb.Name] = a
		case "opensearch", "elasticsearch":
			a, err := NewOpenSearchKB(kb)
			if err != nil {
				log.Printf("event=retrieval_init_failed name=%s type=%s err=%v", kb.Name, typ, err)
				continue
			}
			out[kb.Name] = a
		case "meilisearch":
			a, err := NewMeilisearchKB(kb)
			if err != nil {
				log.Printf("event=retrieval_init_failed name=%s type=meilisearch err=%v", kb.Name, err)
				continue
			}
			out[kb.Name] = a
		default:
			log.Printf("event=retrieval_init_skipped name=%s type=%s reason=%q", kb.Name, kb.Type, "unknown type at runtime (validate config)")
		}
	}
	return out
}
