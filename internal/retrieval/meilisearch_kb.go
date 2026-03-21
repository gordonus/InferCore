package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

// MeilisearchKB uses the Meilisearch search API (GET /indexes/{uid}/search).
type MeilisearchKB struct {
	name   string
	cfg    config.KnowledgeBaseConfig
	client *http.Client
}

// NewMeilisearchKB builds an adapter for knowledge_bases type "meilisearch".
func NewMeilisearchKB(kb config.KnowledgeBaseConfig) (*MeilisearchKB, error) {
	if strings.TrimSpace(kb.Endpoint) == "" || strings.TrimSpace(kb.Index) == "" || strings.TrimSpace(kb.APIKey) == "" {
		return nil, fmt.Errorf("meilisearch kb %q: endpoint, index, and api_key required", kb.Name)
	}
	ms := httpClientTimeoutMS(kb)
	return &MeilisearchKB{
		name: kb.Name,
		cfg:  kb,
		client: &http.Client{
			Timeout: time.Duration(ms) * time.Millisecond,
		},
	}, nil
}

func (m *MeilisearchKB) Name() string { return m.name }

func (m *MeilisearchKB) Retrieve(ctx context.Context, query string, opts map[string]any) (types.RetrievalResult, error) {
	topK := effectiveTopK(m.cfg, opts, 8)
	base := strings.TrimRight(strings.TrimSpace(m.cfg.Endpoint), "/")
	uid := url.PathEscape(strings.TrimSpace(m.cfg.Index))
	u, err := url.Parse(base + "/indexes/" + uid + "/search")
	if err != nil {
		return types.RetrievalResult{}, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", fmt.Sprintf("%d", topK))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return types.RetrievalResult{}, err
	}
	for k, v := range m.cfg.Headers {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			req.Header.Set(k, v)
		}
	}
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(m.cfg.APIKey))
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return types.RetrievalResult{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.RetrievalResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return types.RetrievalResult{}, fmt.Errorf("meilisearch %s: %s: %s", m.name, resp.Status, truncateForErr(b, 512))
	}
	return parseMeilisearchHits(b)
}

func parseMeilisearchHits(b []byte) (types.RetrievalResult, error) {
	var root struct {
		Hits []json.RawMessage `json:"hits"`
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return types.RetrievalResult{}, fmt.Errorf("meilisearch decode: %w", err)
	}
	var out []types.RetrievalChunk
	for i, raw := range root.Hits {
		var hit map[string]any
		if err := json.Unmarshal(raw, &hit); err != nil {
			continue
		}
		// Meilisearch returns document fields at top level; optional _formatted
		text := firstString(hit, "text", "content", "body", "passage")
		if text == "" {
			if fmted, ok := hit["_formatted"].(map[string]any); ok {
				text = firstString(fmted, "text", "content", "body", "passage")
			}
		}
		if text == "" {
			continue
		}
		var score float64
		if s, ok := hit["_rankingScore"].(float64); ok {
			score = s
		}
		src := fmt.Sprintf("hit:%d", i)
		if id, ok := hit["id"].(string); ok && id != "" {
			src = id
		}
		out = append(out, types.RetrievalChunk{Text: text, Source: src, Score: score})
	}
	return types.RetrievalResult{Chunks: out}, nil
}
