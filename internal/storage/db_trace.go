package storage

import (
	"context"
	"encoding/json"
	"time"
)

// TraceSpanRecord is the compact, queryable representation of an ended OTel span.
type TraceSpanRecord struct {
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id,omitempty"`
	Name         string `json:"name"`
	Service      string `json:"service"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	StatusCode   int    `json:"status_code"`
	StartUnixNS  int64  `json:"-"`
	EndUnixNS    int64  `json:"-"`
	StartTime    int64  `json:"start_time"`
	EndTime      int64  `json:"end_time"`
	Attributes   string `json:"attributes"`
	Events       string `json:"events"`
	Links        string `json:"links"`
}

type TraceSummary struct {
	TraceID     string `json:"trace_id"`
	StartUnixNS int64  `json:"-"`
	EndUnixNS   int64  `json:"-"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	SpanCount   int    `json:"span_count"`
	ErrorCount  int    `json:"error_count"`
}

type TraceRepository interface {
	StoreTraceSpan(context.Context, TraceSpanRecord) error
	ListTraces(context.Context, int, int) ([]TraceSummary, error)
	GetTraceSpans(context.Context, string) ([]TraceSpanRecord, error)
	PruneTraceSpans(context.Context, time.Time, int) error
}

func (d *DB) migrateTraceSpans(ctx context.Context) error {
	if _, err := d.execContext(ctx, `CREATE TABLE IF NOT EXISTS otel_trace_spans (
		span_id VARCHAR(32) PRIMARY KEY NOT NULL,
		trace_id VARCHAR(64) NOT NULL,
		parent_span_id VARCHAR(32) NOT NULL DEFAULT '',
		name TEXT NOT NULL DEFAULT '',
		service_name TEXT NOT NULL DEFAULT '',
		span_kind TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT '',
		status_code INTEGER NOT NULL DEFAULT 0,
		start_unix_nano BIGINT NOT NULL,
		end_unix_nano BIGINT NOT NULL,
		attributes_json TEXT NOT NULL,
		events_json TEXT NOT NULL,
		links_json TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := d.execContext(ctx, `CREATE INDEX IF NOT EXISTS idx_otel_trace_spans_trace ON otel_trace_spans(trace_id, start_unix_nano)`); err != nil {
		return err
	}
	_, err := d.execContext(ctx, `CREATE INDEX IF NOT EXISTS idx_otel_trace_spans_end ON otel_trace_spans(end_unix_nano)`)
	return err
}

func (d *DB) StoreTraceSpan(ctx context.Context, span TraceSpanRecord) error {
	_, err := d.execContext(ctx, `INSERT INTO otel_trace_spans
		(span_id, trace_id, parent_span_id, name, service_name, span_kind, status, status_code, start_unix_nano, end_unix_nano, attributes_json, events_json, links_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		span.SpanID, span.TraceID, span.ParentSpanID, span.Name, span.Service, span.Kind, span.Status, span.StatusCode,
		span.StartUnixNS, span.EndUnixNS, nonEmptyJSON(span.Attributes, "{}"), nonEmptyJSON(span.Events, "[]"), nonEmptyJSON(span.Links, "[]"))
	return err
}

func nonEmptyJSON(raw, fallback string) string {
	if json.Valid([]byte(raw)) {
		return raw
	}
	return fallback
}

func (d *DB) ListTraces(ctx context.Context, limit, offset int) ([]TraceSummary, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.queryContext(ctx, `SELECT trace_id, MIN(start_unix_nano), MAX(end_unix_nano), COUNT(*),
		SUM(CASE WHEN status_code = 2 THEN 1 ELSE 0 END)
		FROM otel_trace_spans GROUP BY trace_id ORDER BY MAX(end_unix_nano) DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	traces := make([]TraceSummary, 0, limit)
	for rows.Next() {
		var item TraceSummary
		if err := rows.Scan(&item.TraceID, &item.StartUnixNS, &item.EndUnixNS, &item.SpanCount, &item.ErrorCount); err != nil {
			return nil, err
		}
		item.StartTime = item.StartUnixNS / int64(time.Millisecond)
		item.EndTime = item.EndUnixNS / int64(time.Millisecond)
		traces = append(traces, item)
	}
	return traces, rows.Err()
}

func (d *DB) GetTraceSpans(ctx context.Context, traceID string) ([]TraceSpanRecord, error) {
	rows, err := d.queryContext(ctx, `SELECT trace_id, span_id, parent_span_id, name, service_name, span_kind, status, status_code,
		start_unix_nano, end_unix_nano, attributes_json, events_json, links_json FROM otel_trace_spans
		WHERE trace_id = ? ORDER BY start_unix_nano ASC, span_id ASC`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	spans := make([]TraceSpanRecord, 0, 16)
	for rows.Next() {
		var item TraceSpanRecord
		if err := rows.Scan(&item.TraceID, &item.SpanID, &item.ParentSpanID, &item.Name, &item.Service, &item.Kind, &item.Status,
			&item.StatusCode, &item.StartUnixNS, &item.EndUnixNS, &item.Attributes, &item.Events, &item.Links); err != nil {
			return nil, err
		}
		item.StartTime = item.StartUnixNS / int64(time.Millisecond)
		item.EndTime = item.EndUnixNS / int64(time.Millisecond)
		spans = append(spans, item)
	}
	return spans, rows.Err()
}

func (d *DB) PruneTraceSpans(ctx context.Context, cutoff time.Time, maxTraces int) error {
	if _, err := d.execContext(ctx, `DELETE FROM otel_trace_spans WHERE end_unix_nano < ?`, cutoff.UnixNano()); err != nil {
		return err
	}
	if maxTraces < 100 {
		maxTraces = 100
	}
	_, err := d.execContext(ctx, `DELETE FROM otel_trace_spans WHERE trace_id NOT IN (
		SELECT trace_id FROM (SELECT trace_id FROM otel_trace_spans GROUP BY trace_id ORDER BY MAX(end_unix_nano) DESC LIMIT ?) AS retained_traces
	)`, maxTraces)
	return err
}

var _ TraceRepository = (*DB)(nil)
