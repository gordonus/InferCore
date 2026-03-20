package requests

import (
	"context"
	"errors"
	"slices"
	"sync"
)

type memoryStore struct {
	mu       sync.RWMutex
	requests map[string]RequestRow
	steps    map[string][]StepRow // request_id -> ordered steps
}

// NewMemoryStore returns an in-process ledger (non-durable).
func NewMemoryStore() Store {
	return &memoryStore{
		requests: map[string]RequestRow{},
		steps:    map[string][]StepRow{},
	}
}

func (m *memoryStore) CreateRequest(_ context.Context, row RequestRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.requests[row.RequestID]; ok {
		return errors.New("request already exists")
	}
	m.requests[row.RequestID] = row
	m.steps[row.RequestID] = nil
	return nil
}

func (m *memoryStore) UpdateRequest(_ context.Context, requestID string, patch RequestPatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.requests[requestID]
	if !ok {
		return ErrNotFound
	}
	if patch.Status != nil {
		r.Status = *patch.Status
	}
	if patch.SelectedBackend != nil {
		r.SelectedBackend = *patch.SelectedBackend
	}
	if patch.RouteReason != nil {
		r.RouteReason = *patch.RouteReason
	}
	if patch.PolicySnapshot != nil {
		r.PolicySnapshot = append([]byte(nil), patch.PolicySnapshot...)
	}
	if !patch.UpdatedAt.IsZero() {
		r.UpdatedAt = patch.UpdatedAt
	}
	m.requests[requestID] = r
	return nil
}

func (m *memoryStore) AppendStep(_ context.Context, step StepRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.requests[step.RequestID]; !ok {
		return ErrNotFound
	}
	m.steps[step.RequestID] = append(m.steps[step.RequestID], step)
	return nil
}

func (m *memoryStore) GetRequest(_ context.Context, requestID string) (RequestRow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.requests[requestID]
	if !ok {
		return RequestRow{}, ErrNotFound
	}
	return cloneRequestRow(r), nil
}

func (m *memoryStore) ListSteps(_ context.Context, requestID string) ([]StepRow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.requests[requestID]; !ok {
		return nil, ErrNotFound
	}
	out := slices.Clone(m.steps[requestID])
	return out, nil
}

func (m *memoryStore) Close() error { return nil }

func cloneRequestRow(r RequestRow) RequestRow {
	r.InputJSON = append([]byte(nil), r.InputJSON...)
	r.ContextJSON = append([]byte(nil), r.ContextJSON...)
	r.AIRequestJSON = append([]byte(nil), r.AIRequestJSON...)
	r.PolicySnapshot = append([]byte(nil), r.PolicySnapshot...)
	return r
}
