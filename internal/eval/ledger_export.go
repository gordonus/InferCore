package eval

import (
	"encoding/json"
	"fmt"

	"github.com/infercore/infercore/internal/requests"
	"github.com/infercore/infercore/internal/types"
)

// ItemFromRequestRow builds an eval Item from a ledger row (requires ai_request_json).
// If the row has selected_backend (successful completion), it is copied to expected_backend for routing regression.
func ItemFromRequestRow(row requests.RequestRow) (Item, error) {
	if len(row.AIRequestJSON) == 0 {
		return Item{}, fmt.Errorf("request %s: missing ai_request_json", row.RequestID)
	}
	var req types.AIRequest
	if err := json.Unmarshal(row.AIRequestJSON, &req); err != nil {
		return Item{}, fmt.Errorf("request %s: %w", row.RequestID, err)
	}
	it := Item{
		TenantID:    req.TenantID,
		TaskType:    req.TaskType,
		Priority:    req.Priority,
		RequestType: req.RequestType,
		PipelineRef: req.PipelineRef,
		Context:     req.Context,
		Input:       req.Input,
		Options: map[string]any{
			"stream":     req.Options.Stream,
			"max_tokens": req.Options.MaxTokens,
		},
	}
	if row.SelectedBackend != "" {
		it.ExpectedBackend = row.SelectedBackend
	}
	return it, nil
}

// ItemsFromRequestRows converts ledger rows to eval items, skipping rows that fail conversion.
func ItemsFromRequestRows(rows []requests.RequestRow) ([]Item, []error) {
	var items []Item
	var errs []error
	for _, row := range rows {
		it, err := ItemFromRequestRow(row)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		items = append(items, it)
	}
	return items, errs
}
