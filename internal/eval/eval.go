package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Item is one eval request (subset of /infer body).
type Item struct {
	TenantID        string         `json:"tenant_id"`
	TaskType        string         `json:"task_type"`
	Priority        string         `json:"priority,omitempty"`
	RequestType     string         `json:"request_type,omitempty"`
	PipelineRef     string         `json:"pipeline_ref,omitempty"`
	Context         map[string]any `json:"context,omitempty"`
	Input           map[string]any `json:"input"`
	Options         map[string]any `json:"options"`
	ExpectedBackend string         `json:"expected_backend,omitempty"`
}

// Result is one row outcome.
type Result struct {
	ItemIndex       int
	OK              bool
	StatusCode      int
	LatencyMs       int64
	SelectedBackend string
	ExpectedBackend string
	BackendMatch    bool
	Fallback        bool
	ErrorCode       string
}

// Run posts each item to POST {baseURL}/infer and prints a summary to w.
// defaultPipeline is applied when an item omits pipeline_ref.
// apiKey, if non-empty, is sent as X-InferCore-Api-Key (matches server infercore_api_key).
func Run(ctx context.Context, baseURL string, datasetPath string, w io.Writer, defaultPipeline, apiKey string) error {
	raw, err := os.ReadFile(datasetPath)
	if err != nil {
		return err
	}
	var items []Item
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("parse dataset: %w", err)
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	client := &http.Client{Timeout: 120 * time.Second}

	var results []Result
	for i, it := range items {
		body := map[string]any{
			"tenant_id": it.TenantID,
			"task_type": it.TaskType,
			"input":     it.Input,
			"options":   it.Options,
		}
		if it.Priority != "" {
			body["priority"] = it.Priority
		}
		if it.RequestType != "" {
			body["request_type"] = it.RequestType
		}
		pref := strings.TrimSpace(it.PipelineRef)
		if pref == "" {
			pref = strings.TrimSpace(defaultPipeline)
		}
		if pref != "" {
			body["pipeline_ref"] = pref
		}
		if it.Context != nil {
			body["context"] = it.Context
		}
		payload, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/infer", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if strings.TrimSpace(apiKey) != "" {
			req.Header.Set("X-InferCore-Api-Key", strings.TrimSpace(apiKey))
		}
		start := time.Now()
		resp, err := client.Do(req)
		lat := time.Since(start).Milliseconds()
		if err != nil {
			results = append(results, Result{ItemIndex: i, OK: false, LatencyMs: lat})
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		var parsed map[string]any
		_ = json.Unmarshal(b, &parsed)
		sel := ""
		if v, ok := parsed["selected_backend"].(string); ok {
			sel = v
		}
		fb := false
		if f, ok := parsed["fallback"].(map[string]any); ok {
			if t, ok := f["triggered"].(bool); ok {
				fb = t
			}
		}
		match := it.ExpectedBackend == "" || strings.EqualFold(it.ExpectedBackend, sel)
		ok := resp.StatusCode == http.StatusOK
		ec := ""
		if errObj, ok := parsed["error"].(map[string]any); ok {
			if c, ok := errObj["code"].(string); ok {
				ec = c
			}
		}
		results = append(results, Result{
			ItemIndex:       i,
			OK:              ok,
			StatusCode:      resp.StatusCode,
			LatencyMs:       lat,
			SelectedBackend: sel,
			ExpectedBackend: it.ExpectedBackend,
			BackendMatch:    match,
			Fallback:        fb,
			ErrorCode:       ec,
		})
	}

	fmt.Fprintf(w, "eval items=%d\n", len(items))
	okN, beN := 0, 0
	for _, r := range results {
		if r.OK {
			okN++
		}
		if r.BackendMatch {
			beN++
		}
		fmt.Fprintf(w, "  [%d] http=%d ok=%v latency_ms=%d backend=%q expected=%q backend_match=%v fallback=%v err=%s\n",
			r.ItemIndex, r.StatusCode, r.OK, r.LatencyMs, r.SelectedBackend, r.ExpectedBackend, r.BackendMatch, r.Fallback, r.ErrorCode)
	}
	fmt.Fprintf(w, "summary: success_rate=%.2f backend_match_rate=%.2f\n",
		float64(okN)/float64(max(1, len(results))),
		float64(beN)/float64(max(1, len(results))))
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
