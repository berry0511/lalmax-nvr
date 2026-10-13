package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/config"
	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestObservabilityTraceAPIs(t *testing.T) {
	db, store := setupTestDB(t)
	defer db.Close()
	now := time.Now()
	traceID := "0123456789abcdef0123456789abcdef"
	require.NoError(t, db.StoreTraceSpan(context.Background(), storage.TraceSpanRecord{
		TraceID: traceID, SpanID: "0123456789abcdef", Name: "camera.start", Service: "lalmax-nvr",
		StatusCode: 2, StartUnixNS: now.Add(-20 * time.Millisecond).UnixNano(), EndUnixNS: now.UnixNano(), Attributes: `{}`, Events: `[]`, Links: `[]`,
	}))
	cfg := &config.Config{}
	cfg.Observability.TraceUIURLTemplate = "http://traces.local/trace/{trace_id}"
	h := newHandlerWithConfig(db, store, cfg)
	routes := h.Routes()

	list := doRequest(t, routes, http.MethodGet, "/api/observability/traces?limit=10", nil, "", "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	var listBody struct {
		Traces   []storage.TraceSummary `json:"traces"`
		Template string                 `json:"external_trace_url_template"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &listBody))
	require.Len(t, listBody.Traces, 1)
	require.Equal(t, traceID, listBody.Traces[0].TraceID)
	require.Equal(t, cfg.Observability.TraceUIURLTemplate, listBody.Template)

	detail := doRequest(t, routes, http.MethodGet, "/api/observability/traces/"+traceID, nil, "", "")
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	var detailBody struct {
		TraceID string                    `json:"trace_id"`
		Spans   []storage.TraceSpanRecord `json:"spans"`
	}
	require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &detailBody))
	require.Equal(t, traceID, detailBody.TraceID)
	require.Len(t, detailBody.Spans, 1)
	require.Equal(t, "camera.start", detailBody.Spans[0].Name)
}
