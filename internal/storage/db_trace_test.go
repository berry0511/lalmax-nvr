package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalTraceStoreListDetailAndPrune(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "trace.db"))
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()
	require.NoError(t, db.Init(ctx))

	now := time.Now()
	require.NoError(t, db.StoreTraceSpan(ctx, TraceSpanRecord{
		TraceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SpanID: "1111111111111111", Name: "GET /api/cameras/{id}",
		Service: "lalmax-nvr", StatusCode: 2, StartUnixNS: now.Add(-time.Second).UnixNano(), EndUnixNS: now.UnixNano(),
		Attributes: `{"http.route":"/api/cameras/{id}"}`, Events: `[]`,
	}))
	require.NoError(t, db.StoreTraceSpan(ctx, TraceSpanRecord{
		TraceID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SpanID: "2222222222222222", Name: "camera.start",
		Service: "lalmax-nvr", StartUnixNS: now.Add(-2 * time.Second).UnixNano(), EndUnixNS: now.Add(-time.Second).UnixNano(),
	}))

	traces, err := db.ListTraces(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, traces, 2)
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", traces[0].TraceID)
	require.Equal(t, int64(now.UnixMilli()), traces[0].EndTime)
	require.Equal(t, 1, traces[0].ErrorCount)

	spans, err := db.GetTraceSpans(ctx, traces[0].TraceID)
	require.NoError(t, err)
	require.Len(t, spans, 1)
	require.Equal(t, "GET /api/cameras/{id}", spans[0].Name)
	require.JSONEq(t, `{"http.route":"/api/cameras/{id}"}`, spans[0].Attributes)

	require.NoError(t, db.PruneTraceSpans(ctx, now.Add(-500*time.Millisecond), 100))
	traces, err = db.ListTraces(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, traces, 1)
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", traces[0].TraceID)
}
