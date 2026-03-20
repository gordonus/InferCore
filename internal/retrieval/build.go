package retrieval

import (
	"log"
	"strings"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/interfaces"
)

// FromConfig builds retrieval adapters from knowledge_bases (file-backed demo).
func FromConfig(cfg *config.Config) map[string]interfaces.RetrievalAdapter {
	out := make(map[string]interfaces.RetrievalAdapter)
	if cfg == nil {
		return out
	}
	for _, kb := range cfg.KnowledgeBases {
		if strings.ToLower(strings.TrimSpace(kb.Type)) != "file" {
			continue
		}
		a, err := NewFileKB(kb.Name, kb.Path)
		if err != nil {
			log.Printf("event=retrieval_init_failed name=%s err=%v", kb.Name, err)
			continue
		}
		out[kb.Name] = a
	}
	return out
}
