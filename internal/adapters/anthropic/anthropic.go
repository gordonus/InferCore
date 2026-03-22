// Package anthropic implements the Anthropic Messages API (Claude) over HTTP.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
	"github.com/infercore/infercore/internal/upstream"
)

const (
	defaultEndpoint      = "https://api.anthropic.com"
	anthropicAPIVersion  = "2023-06-01"
	defaultMaxTokens     = 4096
)

type Adapter struct {
	cfg        config.BackendConfig
	baseURL    string
	httpClient *http.Client
}

func New(cfg config.BackendConfig) *Adapter {
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	base := strings.TrimSpace(cfg.Endpoint)
	if base == "" {
		base = defaultEndpoint
	}
	return &Adapter{
		cfg:     cfg,
		baseURL: strings.TrimRight(base, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (a *Adapter) Name() string {
	return a.cfg.Name
}

func (a *Adapter) model() string {
	return strings.TrimSpace(a.cfg.DefaultModel)
}

func (a *Adapter) maxTokens(req types.BackendRequest) int {
	if req.Options.MaxTokens > 0 {
		return req.Options.MaxTokens
	}
	return defaultMaxTokens
}

func (a *Adapter) Invoke(ctx context.Context, r types.BackendRequest) (types.BackendResponse, error) {
	text, _ := r.Input["text"].(string)
	if text == "" {
		text = "No text provided."
	}
	model := a.model()
	if model == "" {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, "anthropic backend requires default_model")
	}
	if strings.TrimSpace(a.cfg.APIKey) == "" {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, "anthropic backend requires api_key")
	}

	if r.Options.Stream {
		return a.invokeStream(ctx, text, model, a.maxTokens(r))
	}
	return a.invokeNonStream(ctx, text, model, a.maxTokens(r))
}

func (a *Adapter) invokeNonStream(ctx context.Context, text, model string, maxTok int) (types.BackendResponse, error) {
	t0 := time.Now()
	payload := map[string]any{
		"model":       model,
		"max_tokens":  maxTok,
		"messages": []map[string]any{
			{"role": "user", "content": text},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return types.BackendResponse{}, err
	}
	url := a.baseURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return types.BackendResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	httpReq.Header.Set("x-api-key", strings.TrimSpace(a.cfg.APIKey))
	a.applyExtraHeaders(httpReq)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return types.BackendResponse{}, upstream.New(upstream.KindTimeout, err.Error())
		}
		return types.BackendResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 500 {
			return types.BackendResponse{}, upstream.New(upstream.KindUpstream5xx, fmt.Sprintf("anthropic messages failed status=%d", resp.StatusCode))
		}
		return types.BackendResponse{}, upstream.New(upstream.KindUpstream4xx, fmt.Sprintf("anthropic messages failed status=%d", resp.StatusCode))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, err.Error())
	}
	out := ""
	for _, b := range parsed.Content {
		if b.Type == "text" {
			out += b.Text
		}
	}
	if out == "" {
		out = "[anthropic] empty response"
	}
	t1 := time.Now()
	lat := t1.Sub(t0).Milliseconds()
	return types.BackendResponse{
		Output: map[string]any{
			"text":    out,
			"backend": a.cfg.Name,
		},
		Timing: &types.BackendTiming{
			TTFTMs:              lat,
			CompletionLatencyMs: lat,
			Streamed:            false,
		},
	}, nil
}

func (a *Adapter) invokeStream(ctx context.Context, text, model string, maxTok int) (types.BackendResponse, error) {
	t0 := time.Now()
	payload := map[string]any{
		"model":       model,
		"max_tokens":  maxTok,
		"stream":      true,
		"messages": []map[string]any{
			{"role": "user", "content": text},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return types.BackendResponse{}, err
	}
	url := a.baseURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return types.BackendResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	httpReq.Header.Set("x-api-key", strings.TrimSpace(a.cfg.APIKey))
	a.applyExtraHeaders(httpReq)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return types.BackendResponse{}, upstream.New(upstream.KindTimeout, err.Error())
		}
		return types.BackendResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 500 {
			return types.BackendResponse{}, upstream.New(upstream.KindUpstream5xx, fmt.Sprintf("anthropic stream failed status=%d", resp.StatusCode))
		}
		return types.BackendResponse{}, upstream.New(upstream.KindUpstream4xx, fmt.Sprintf("anthropic stream failed status=%d", resp.StatusCode))
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var b strings.Builder
	var firstToken time.Time
	var sawData bool

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		sawData = true

		var ev struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if ev.Type != "content_block_delta" || ev.Delta.Type != "text_delta" {
			continue
		}
		part := ev.Delta.Text
		if part == "" {
			continue
		}
		if firstToken.IsZero() {
			firstToken = time.Now()
		}
		b.WriteString(part)
	}
	if err := scanner.Err(); err != nil {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, err.Error())
	}
	if !sawData && b.Len() == 0 {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, "empty anthropic stream")
	}

	tEnd := time.Now()
	out := b.String()
	if out == "" {
		out = "[anthropic] empty stream"
	}
	var ttft int64
	if !firstToken.IsZero() {
		ttft = firstToken.Sub(t0).Milliseconds()
		if ttft < 0 {
			ttft = 0
		}
	}
	total := tEnd.Sub(t0).Milliseconds()
	tokens := int64(utf8.RuneCountInString(out))
	if tokens < 1 {
		tokens = 1
	}
	var tpot int64
	if !firstToken.IsZero() {
		post := tEnd.Sub(firstToken).Milliseconds()
		if post > 0 {
			tpot = post / tokens
			if tpot <= 0 {
				tpot = 1
			}
		}
	}

	return types.BackendResponse{
		Output: map[string]any{
			"text":             out,
			"backend":          a.cfg.Name,
			"stream_requested": true,
			"stream_degraded":  false,
		},
		Timing: &types.BackendTiming{
			TTFTMs:              ttft,
			CompletionLatencyMs: total,
			TPOTMs:              tpot,
			Streamed:            true,
		},
	}, nil
}

func (a *Adapter) Health(ctx context.Context) error {
	// Anthropic has no public lightweight health; verify API key with a minimal request would cost $.
	// GET /v1/models may exist — use optional health_path or skip to TCP reachability via HEAD on base.
	path := strings.TrimSpace(a.cfg.HealthPath)
	if path == "" {
		path = "/v1/models"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := a.baseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("x-api-key", strings.TrimSpace(a.cfg.APIKey))
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	a.applyExtraHeaders(httpReq)
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return upstream.New(upstream.KindBackendUnhealthy, err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstream.New(upstream.KindBackendUnhealthy, fmt.Sprintf("anthropic health failed status=%d path=%s", resp.StatusCode, path))
	}
	return nil
}

func (a *Adapter) applyExtraHeaders(req *http.Request) {
	for hk, v := range a.cfg.Headers {
		hk = strings.TrimSpace(hk)
		if hk == "" {
			continue
		}
		req.Header.Set(hk, v)
	}
}

func (a *Adapter) Metadata() types.BackendMetadata {
	return types.BackendMetadata{
		Name:           a.cfg.Name,
		Type:           a.cfg.Type,
		Capabilities:   a.cfg.Capabilities,
		CostUnit:       a.cfg.Cost.Unit,
		MaxConcurrency: a.cfg.MaxConcurrency,
	}
}
