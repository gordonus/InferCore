package requests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

const postgresSchema = `
CREATE TABLE IF NOT EXISTS requests (
  request_id TEXT PRIMARY KEY,
  trace_id TEXT NOT NULL,
  request_type TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  task_type TEXT NOT NULL,
  priority TEXT NOT NULL,
  pipeline_ref TEXT NOT NULL,
  input_json TEXT NOT NULL,
  context_json TEXT NOT NULL,
  ai_request_json TEXT,
  policy_snapshot_json TEXT,
  status TEXT NOT NULL,
  selected_backend TEXT,
  route_reason TEXT,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_requests_trace ON requests(trace_id);
CREATE INDEX IF NOT EXISTS idx_requests_tenant ON requests(tenant_id);
CREATE INDEX IF NOT EXISTS idx_requests_created ON requests(created_at);

CREATE TABLE IF NOT EXISTS request_steps (
  request_id TEXT NOT NULL,
  step_index INTEGER NOT NULL,
  step_type TEXT NOT NULL,
  input_json TEXT,
  output_json TEXT,
  backend TEXT,
  status TEXT NOT NULL,
  error TEXT,
  latency_ms BIGINT NOT NULL,
  metadata_json TEXT,
  PRIMARY KEY (request_id, step_index),
  FOREIGN KEY (request_id) REFERENCES requests(request_id) ON DELETE CASCADE
);
`

type postgresStore struct {
	db *sql.DB
}

// NewPostgresStore opens a Postgres-backed ledger using lib/pq (connection string / DSN).
func NewPostgresStore(dsn string) (Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	if _, err := db.Exec(postgresSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres migrate: %w", err)
	}
	return &postgresStore{db: db}, nil
}

func (s *postgresStore) CreateRequest(ctx context.Context, row RequestRow) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO requests (
  request_id, trace_id, request_type, tenant_id, task_type, priority, pipeline_ref,
  input_json, context_json, ai_request_json, policy_snapshot_json, status, selected_backend, route_reason,
  created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		row.RequestID, row.TraceID, row.RequestType, row.TenantID, row.TaskType, row.Priority, row.PipelineRef,
		string(row.InputJSON), string(row.ContextJSON), nullIfEmpty(row.AIRequestJSON), string(row.PolicySnapshot), row.Status, row.SelectedBackend, row.RouteReason,
		row.CreatedAt.UnixMilli(), row.UpdatedAt.UnixMilli(),
	)
	return err
}

func nullIfEmpty(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func (s *postgresStore) UpdateRequest(ctx context.Context, requestID string, patch RequestPatch) error {
	r, err := s.GetRequest(ctx, requestID)
	if err != nil {
		return err
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
	_, err = s.db.ExecContext(ctx, `
UPDATE requests SET
  policy_snapshot_json = $1,
  status = $2,
  selected_backend = $3,
  route_reason = $4,
  updated_at = $5
WHERE request_id = $6`,
		string(r.PolicySnapshot), r.Status, r.SelectedBackend, r.RouteReason, r.UpdatedAt.UnixMilli(), requestID,
	)
	return err
}

func (s *postgresStore) AppendStep(ctx context.Context, step StepRow) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO request_steps (
  request_id, step_index, step_type, input_json, output_json, backend, status, error, latency_ms, metadata_json
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		step.RequestID, step.StepIndex, step.StepType, nullStringBytes(step.InputJSON), nullStringBytes(step.OutputJSON),
		step.Backend, step.Status, step.Error, step.LatencyMs, nullStringBytes(step.MetadataJSON),
	)
	return err
}

func nullStringBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func (s *postgresStore) GetRequest(ctx context.Context, requestID string) (RequestRow, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT request_id, trace_id, request_type, tenant_id, task_type, priority, pipeline_ref,
  input_json, context_json, ai_request_json, policy_snapshot_json, status, selected_backend, route_reason,
  created_at, updated_at
FROM requests WHERE request_id = $1`, requestID)
	var r RequestRow
	var createdMs, updatedMs int64
	var policySnap, aiReq sql.NullString
	if err := row.Scan(
		&r.RequestID, &r.TraceID, &r.RequestType, &r.TenantID, &r.TaskType, &r.Priority, &r.PipelineRef,
		&r.InputJSON, &r.ContextJSON, &aiReq, &policySnap, &r.Status, &r.SelectedBackend, &r.RouteReason,
		&createdMs, &updatedMs,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RequestRow{}, ErrNotFound
		}
		return RequestRow{}, err
	}
	if aiReq.Valid {
		r.AIRequestJSON = []byte(aiReq.String)
	}
	if policySnap.Valid {
		r.PolicySnapshot = []byte(policySnap.String)
	}
	r.CreatedAt = time.UnixMilli(createdMs)
	r.UpdatedAt = time.UnixMilli(updatedMs)
	return r, nil
}

func (s *postgresStore) ListSteps(ctx context.Context, requestID string) ([]StepRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT request_id, step_index, step_type, input_json, output_json, backend, status, error, latency_ms, metadata_json
FROM request_steps WHERE request_id = $1 ORDER BY step_index ASC`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StepRow
	for rows.Next() {
		var srow StepRow
		var inJ, outJ, metaJ sql.NullString
		if err := rows.Scan(&srow.RequestID, &srow.StepIndex, &srow.StepType, &inJ, &outJ, &srow.Backend, &srow.Status, &srow.Error, &srow.LatencyMs, &metaJ); err != nil {
			return nil, err
		}
		if inJ.Valid {
			srow.InputJSON = []byte(inJ.String)
		}
		if outJ.Valid {
			srow.OutputJSON = []byte(outJ.String)
		}
		if metaJ.Valid {
			srow.MetadataJSON = []byte(metaJ.String)
		}
		out = append(out, srow)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		if _, err := s.GetRequest(ctx, requestID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *postgresStore) Close() error {
	return s.db.Close()
}
