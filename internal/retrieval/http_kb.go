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

// HTTPJSONKB calls a remote retrieval service over HTTP POST with a small JSON contract.
//
// Request body: {"query":"<q>","top_k":N}
// Response body: {"chunks":[{"text":"...","source":"...","score":1.0}, ...]}
// Alternative key "results" is also accepted with the same element shape.
type HTTPJSONKB struct {
	name   string
	cfg    config.KnowledgeBaseConfig
	client *http.Client
}

// NewHTTPJSONKB builds an adapter for knowledge_bases type "http".
func NewHTTPJSONKB(kb config.KnowledgeBaseConfig) (*HTTPJSONKB, error) {
	if strings.TrimSpace(kb.Endpoint) == "" {
		return nil, fmt.Errorf("http kb %q: empty endpoint", kb.Name)
	}
	ms := httpClientTimeoutMS(kb)
	return &HTTPJSONKB{
		name: kb.Name,
		cfg:  kb,
		client: &http.Client{
			Timeout: time.Duration(ms) * time.Millisecond,
		},
	}, nil
}

func (h *HTTPJSONKB) Name() string { return h.name }

func (h *HTTPJSONKB) Retrieve(ctx context.Context, query string, opts map[string]any) (types.RetrievalResult, error) {
	topK := effectiveTopK(h.cfg, opts, 8)
	body := map[string]any{
		"query": query,
		"top_k": topK,
		"kb":    h.name,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.cfg.Endpoint, "/"), bytes.NewReader(raw))
	if err != nil {
		return types.RetrievalResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.cfg.Headers {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			req.Header.Set(k, v)
		}
	}
	if req.Header.Get("Authorization") == "" && strings.TrimSpace(h.cfg.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(h.cfg.APIKey))
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return types.RetrievalResult{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.RetrievalResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return types.RetrievalResult{}, fmt.Errorf("http retrieval %s: status %d: %s", h.name, resp.StatusCode, truncateForErr(b, 512))
	}
	return parseHTTPChunksJSON(b)
}

func truncateForErr(b []byte, max int) string {
	s := string(b)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

type httpChunkEl struct {
	Text    string  `json:"text"`
	Content string  `json:"content"`
	Source  string  `json:"source"`
	Score   float64 `json:"score"`
}

type httpChunksEnvelope struct {
	Chunks  []httpChunkEl `json:"chunks"`
	Results []httpChunkEl `json:"results"`
}

func parseHTTPChunksJSON(b []byte) (types.RetrievalResult, error) {
	var env httpChunksEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return types.RetrievalResult{}, fmt.Errorf("decode retrieval json: %w", err)
	}
	list := env.Chunks
	if len(list) == 0 {
		list = env.Results
	}
	out := make([]types.RetrievalChunk, 0, len(list))
	for _, el := range list {
		text := strings.TrimSpace(el.Text)
		if text == "" {
			text = strings.TrimSpace(el.Content)
		}
		if text == "" {
			continue
		}
		out = append(out, types.RetrievalChunk{
			Text:   text,
			Source: el.Source,
			Score:  el.Score,
		})
	}
	return types.RetrievalResult{Chunks: out}, nil
}
