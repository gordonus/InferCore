package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

// OpenSearchKB runs a multi_match query against an OpenSearch or Elasticsearch index.
type OpenSearchKB struct {
	name   string
	cfg    config.KnowledgeBaseConfig
	client *http.Client
	fields []string
}

// NewOpenSearchKB builds an adapter for knowledge_bases type "opensearch" or "elasticsearch".
func NewOpenSearchKB(kb config.KnowledgeBaseConfig) (*OpenSearchKB, error) {
	if strings.TrimSpace(kb.Endpoint) == "" || strings.TrimSpace(kb.Index) == "" {
		return nil, fmt.Errorf("opensearch kb %q: endpoint and index required", kb.Name)
	}
	fields := kb.SearchFields
	if len(fields) == 0 {
		fields = []string{"text", "content", "body"}
	}
	ms := httpClientTimeoutMS(kb)
	return &OpenSearchKB{
		name:   kb.Name,
		cfg:    kb,
		fields: fields,
		client: &http.Client{Timeout: time.Duration(ms) * time.Millisecond},
	}, nil
}

func (o *OpenSearchKB) Name() string { return o.name }

func (o *OpenSearchKB) Retrieve(ctx context.Context, query string, opts map[string]any) (types.RetrievalResult, error) {
	topK := effectiveTopK(o.cfg, opts, 8)
	q := map[string]any{
		"size": topK,
		"query": map[string]any{
			"multi_match": map[string]any{
				"query":  query,
				"fields": o.fields,
				"type":   "best_fields",
			},
		},
		"_source": true,
	}
	raw, err := json.Marshal(q)
	if err != nil {
		return types.RetrievalResult{}, err
	}
	base := strings.TrimRight(strings.TrimSpace(o.cfg.Endpoint), "/")
	index := strings.Trim(strings.TrimSpace(o.cfg.Index), "/")
	url := base + "/" + index + "/_search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return types.RetrievalResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range o.cfg.Headers {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			req.Header.Set(k, v)
		}
	}
	if req.Header.Get("Authorization") == "" {
		if k := strings.TrimSpace(o.cfg.APIKey); k != "" {
			switch {
			case strings.HasPrefix(k, "Basic "), strings.HasPrefix(k, "Bearer "), strings.HasPrefix(k, "ApiKey "):
				req.Header.Set("Authorization", k)
			default:
				req.Header.Set("Authorization", "ApiKey "+k)
			}
		}
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return types.RetrievalResult{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.RetrievalResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return types.RetrievalResult{}, fmt.Errorf("opensearch %s: %s: %s", o.name, resp.Status, truncateForErr(b, 512))
	}
	return parseOpenSearchHits(b)
}

func parseOpenSearchHits(b []byte) (types.RetrievalResult, error) {
	var root struct {
		Hits struct {
			Hits []struct {
				ID     string          `json:"_id"`
				Index  string          `json:"_index"`
				Score  float64         `json:"_score"`
				Source json.RawMessage `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return types.RetrievalResult{}, fmt.Errorf("opensearch decode: %w", err)
	}
	var out []types.RetrievalChunk
	for _, h := range root.Hits.Hits {
		var src map[string]any
		if err := json.Unmarshal(h.Source, &src); err != nil {
			continue
		}
		text := firstString(src, "text", "content", "body", "passage", "chunk")
		if text == "" {
			continue
		}
		srcPath := h.Index + "/" + h.ID
		out = append(out, types.RetrievalChunk{
			Text:   text,
			Source: srcPath,
			Score:  h.Score,
		})
	}
	return types.RetrievalResult{Chunks: out}, nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if s := strings.TrimSpace(t); s != "" {
					return s
				}
			}
		}
	}
	return ""
}
