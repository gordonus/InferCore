// Package vllm implements an OpenAI-compatible Chat Completions HTTP client.
// Config types "vllm", "openai", and "openai_compatible" are equivalent; use "openai_compatible"
// for vendor-neutral documentation. vLLM defaults to GET /health; set health_path (e.g. /v1/models)
// for APIs without /health.
package vllm

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

type Adapter struct {
	cfg        config.BackendConfig
	httpClient *http.Client
}

func New(cfg config.BackendConfig) *Adapter {
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	return &Adapter{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (a *Adapter) Name() string {
	return a.cfg.Name
}

func (a *Adapter) Invoke(ctx context.Context, req types.BackendRequest) (types.BackendResponse, error) {
	text, _ := req.Input["text"].(string)
	if text == "" {
		text = "No text provided."
	}
	streamRequested := req.Options.Stream

	model := strings.TrimSpace(a.cfg.DefaultModel)
	if model == "" {
		model = "infercore-default"
	}

	if streamRequested {
		return a.invokeStream(ctx, text, model)
	}
	return a.invokeNonStream(ctx, text, model, streamRequested)
}

func (a *Adapter) invokeNonStream(ctx context.Context, text, model string, streamRequested bool) (types.BackendResponse, error) {
	t0 := time.Now()
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": text},
		},
		"stream": false,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return types.BackendResponse{}, err
	}

	url := joinURL(a.cfg.Endpoint, "/v1/chat/completions")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return types.BackendResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	a.applyHeaders(httpReq)

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
			return types.BackendResponse{}, upstream.New(upstream.KindUpstream5xx, fmt.Sprintf("openai-compatible invoke failed status=%d", resp.StatusCode))
		}
		return types.BackendResponse{}, upstream.New(upstream.KindUpstream4xx, fmt.Sprintf("openai-compatible invoke failed status=%d", resp.StatusCode))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, err.Error())
	}

	output := ""
	if len(parsed.Choices) > 0 {
		if parsed.Choices[0].Message.Content != "" {
			output = parsed.Choices[0].Message.Content
		} else {
			output = parsed.Choices[0].Text
		}
	}
	if output == "" {
		output = "[openai-compatible] empty response"
	}

	t1 := time.Now()
	latency := t1.Sub(t0).Milliseconds()
	return types.BackendResponse{
		Output: map[string]any{
			"text":             output,
			"backend":          a.cfg.Name,
			"stream_requested": streamRequested,
			"stream_degraded":  false,
		},
		Timing: &types.BackendTiming{
			TTFTMs:              latency,
			CompletionLatencyMs: latency,
			Streamed:            false,
		},
	}, nil
}

func (a *Adapter) invokeStream(ctx context.Context, text, model string) (types.BackendResponse, error) {
	t0 := time.Now()
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": text},
		},
		"stream": true,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return types.BackendResponse{}, err
	}
	url := joinURL(a.cfg.Endpoint, "/v1/chat/completions")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return types.BackendResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	a.applyHeaders(httpReq)

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
			return types.BackendResponse{}, upstream.New(upstream.KindUpstream5xx, fmt.Sprintf("openai-compatible stream failed status=%d", resp.StatusCode))
		}
		return types.BackendResponse{}, upstream.New(upstream.KindUpstream4xx, fmt.Sprintf("openai-compatible stream failed status=%d", resp.StatusCode))
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "event-stream") && !strings.Contains(ct, "text/plain") {
		// Upstream ignored stream flag; parse as JSON completion.
		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return types.BackendResponse{}, upstream.New(upstream.KindBackendError, err.Error())
		}
		out := ""
		if len(parsed.Choices) > 0 {
			out = parsed.Choices[0].Message.Content
		}
		t1 := time.Now()
		lat := t1.Sub(t0).Milliseconds()
		return types.BackendResponse{
			Output: map[string]any{
				"text":             out,
				"backend":          a.cfg.Name,
				"stream_requested": true,
				"stream_degraded":  true,
			},
			Timing: &types.BackendTiming{
				TTFTMs:              lat,
				CompletionLatencyMs: lat,
				Streamed:            false,
			},
		}, nil
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var b strings.Builder
	var firstToken time.Time
	var sawData bool

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		sawData = true

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		part := chunk.Choices[0].Delta.Content
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
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, "empty stream response")
	}

	tEnd := time.Now()
	out := b.String()
	if out == "" {
		out = "[openai-compatible] empty stream"
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
	path := strings.TrimSpace(a.cfg.HealthPath)
	if path == "" {
		path = "/health"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := joinURL(a.cfg.Endpoint, path)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	a.applyHeaders(httpReq)
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return upstream.New(upstream.KindBackendUnhealthy, err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstream.New(upstream.KindBackendUnhealthy, fmt.Sprintf("health check failed status=%d path=%s", resp.StatusCode, path))
	}
	return nil
}

func (a *Adapter) applyHeaders(req *http.Request) {
	for k, v := range a.cfg.Headers {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	key := strings.TrimSpace(a.cfg.APIKey)
	if key == "" {
		return
	}
	name := strings.TrimSpace(a.cfg.AuthHeaderName)
	if name == "" {
		req.Header.Set("Authorization", "Bearer "+key)
		return
	}
	req.Header.Set(name, key)
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

func joinURL(base, suffix string) string {
	return strings.TrimRight(base, "/") + suffix
}
