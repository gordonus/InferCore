// Package gemini implements the Google Generative Language API (Gemini) REST client:
// generateContent and streamGenerateContent (SSE). Uses API key auth (x-goog-api-key).
// For OpenAI-shaped proxies in front of Gemini, use type openai_compatible instead.
package gemini

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

const defaultGeminiEndpoint = "https://generativelanguage.googleapis.com"

type gemPart struct {
	Text string `json:"text"`
}

type gemContent struct {
	Parts []gemPart `json:"parts"`
}

type gemCandidate struct {
	Content gemContent `json:"content"`
}

type geminiGenerateResponse struct {
	Candidates []gemCandidate `json:"candidates"`
}

func geminiCandidatesText(candidates []gemCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

type Adapter struct {
	cfg        config.BackendConfig
	baseURL    string
	httpClient *http.Client
	vertex     bool // Vertex AI Gemini (Bearer token + regional aiplatform host)
}

func New(cfg config.BackendConfig) *Adapter {
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	vertex := cfg.Type == "gemini_vertex"
	base := strings.TrimSpace(cfg.Endpoint)
	if base == "" {
		if vertex {
			loc := strings.TrimSpace(cfg.VertexLocation)
			if loc == "" {
				loc = "us-central1"
			}
			base = fmt.Sprintf("https://%s-aiplatform.googleapis.com", loc)
		} else {
			base = defaultGeminiEndpoint
		}
	}
	return &Adapter{
		cfg:     cfg,
		vertex:  vertex,
		baseURL: strings.TrimRight(base, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (a *Adapter) Name() string {
	return a.cfg.Name
}

func (a *Adapter) modelID() string {
	m := strings.TrimSpace(a.cfg.DefaultModel)
	m = strings.TrimPrefix(m, "models/")
	return strings.TrimSpace(m)
}

func (a *Adapter) apiKey() string {
	return strings.TrimSpace(a.cfg.APIKey)
}

// actionPath returns URL path for generateContent / streamGenerateContent (Vertex vs AI Studio).
func (a *Adapter) actionPath(model, action string) string {
	model = strings.TrimPrefix(model, "models/")
	model = strings.TrimSpace(model)
	if a.vertex {
		proj := strings.TrimSpace(a.cfg.VertexProject)
		loc := strings.TrimSpace(a.cfg.VertexLocation)
		return fmt.Sprintf("/v1/projects/%s/locations/%s/publishers/google/models/%s:%s", proj, loc, model, action)
	}
	return fmt.Sprintf("/v1beta/models/%s:%s", model, action)
}

func (a *Adapter) Invoke(ctx context.Context, req types.BackendRequest) (types.BackendResponse, error) {
	text, _ := req.Input["text"].(string)
	if text == "" {
		text = "No text provided."
	}
	model := a.modelID()
	if model == "" {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, "gemini backend requires default_model")
	}
	if a.apiKey() == "" {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, "gemini backend requires api_key")
	}

	if req.Options.Stream {
		return a.invokeStream(ctx, text, model)
	}
	return a.invokeNonStream(ctx, text, model)
}

func (a *Adapter) invokeNonStream(ctx context.Context, text, model string) (types.BackendResponse, error) {
	t0 := time.Now()
	payload := map[string]any{
		"contents": []map[string]any{
			{
				"role":  "user",
				"parts": []map[string]string{{"text": text}},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return types.BackendResponse{}, err
	}
	url := a.baseURL + a.actionPath(model, "generateContent")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return types.BackendResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	a.applyAPIKey(httpReq)
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
			return types.BackendResponse{}, upstream.New(upstream.KindUpstream5xx, fmt.Sprintf("gemini generateContent failed status=%d", resp.StatusCode))
		}
		return types.BackendResponse{}, upstream.New(upstream.KindUpstream4xx, fmt.Sprintf("gemini generateContent failed status=%d", resp.StatusCode))
	}

	var parsed geminiGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, err.Error())
	}
	out := geminiCandidatesText(parsed.Candidates)
	if out == "" {
		out = "[gemini] empty response"
	}
	t1 := time.Now()
	lat := t1.Sub(t0).Milliseconds()
	return types.BackendResponse{
		Output: map[string]any{
			"text":             out,
			"backend":          a.cfg.Name,
			"stream_requested": false,
			"stream_degraded":  false,
		},
		Timing: &types.BackendTiming{
			TTFTMs:              lat,
			CompletionLatencyMs: lat,
			Streamed:            false,
		},
	}, nil
}

func (a *Adapter) invokeStream(ctx context.Context, text, model string) (types.BackendResponse, error) {
	t0 := time.Now()
	payload := map[string]any{
		"contents": []map[string]any{
			{
				"role":  "user",
				"parts": []map[string]string{{"text": text}},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return types.BackendResponse{}, err
	}
	url := a.baseURL + a.actionPath(model, "streamGenerateContent") + "?alt=sse"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return types.BackendResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	a.applyAPIKey(httpReq)
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
			return types.BackendResponse{}, upstream.New(upstream.KindUpstream5xx, fmt.Sprintf("gemini stream failed status=%d", resp.StatusCode))
		}
		return types.BackendResponse{}, upstream.New(upstream.KindUpstream4xx, fmt.Sprintf("gemini stream failed status=%d", resp.StatusCode))
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "event-stream") && !strings.Contains(ct, "text/plain") {
		// Fall back to single JSON body (non-SSE).
		var parsed geminiGenerateResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return types.BackendResponse{}, upstream.New(upstream.KindBackendError, err.Error())
		}
		out := geminiCandidatesText(parsed.Candidates)
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

		var chunk geminiGenerateResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		part := geminiCandidatesText(chunk.Candidates)
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
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, "empty gemini stream")
	}

	tEnd := time.Now()
	out := b.String()
	if out == "" {
		out = "[gemini] empty stream"
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
		if a.vertex {
			proj := strings.TrimSpace(a.cfg.VertexProject)
			loc := strings.TrimSpace(a.cfg.VertexLocation)
			path = fmt.Sprintf("/v1/projects/%s/locations/%s", proj, loc)
		} else {
			path = "/v1beta/models"
		}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := a.baseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	a.applyAPIKey(httpReq)
	a.applyExtraHeaders(httpReq)
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return upstream.New(upstream.KindBackendUnhealthy, err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstream.New(upstream.KindBackendUnhealthy, fmt.Sprintf("gemini health failed status=%d path=%s", resp.StatusCode, path))
	}
	return nil
}

func (a *Adapter) applyAPIKey(req *http.Request) {
	if k := a.apiKey(); k != "" {
		if a.vertex {
			req.Header.Set("Authorization", "Bearer "+k)
		} else {
			req.Header.Set("x-goog-api-key", k)
		}
	}
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
