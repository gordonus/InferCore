package retrieval

import "github.com/infercore/infercore/internal/config"

func effectiveTopK(kb config.KnowledgeBaseConfig, opts map[string]any, fallback int) int {
	if opts != nil {
		switch v := opts["top_k"].(type) {
		case int:
			if v > 0 {
				return v
			}
		case int64:
			if v > 0 {
				return int(v)
			}
		case float64:
			if v > 0 {
				return int(v)
			}
		}
	}
	if kb.TopK > 0 {
		return kb.TopK
	}
	return fallback
}

func httpClientTimeoutMS(kb config.KnowledgeBaseConfig) int {
	if kb.HTTPTimeoutMS > 0 {
		return kb.HTTPTimeoutMS
	}
	return 30000
}
