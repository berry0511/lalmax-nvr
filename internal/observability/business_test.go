package observability

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestDetachedBusinessSpanKeepsParentAfterCancellation(t *testing.T) {
	previous := otel.GetTracerProvider()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})

	ctx, parent := StartBusinessSpan(context.Background(), "camera.start")
	parentContext := parent.SpanContext()
	requestCtx, cancel := context.WithCancel(ctx)
	cancel()
	if requestCtx.Err() == nil {
		t.Fatal("request context should be canceled")
	}

	workerCtx := DetachedContext(requestCtx)
	if workerCtx.Err() != nil {
		t.Fatal("detached worker context should not inherit cancellation")
	}
	_, child := StartBusinessSpan(workerCtx, "recorder.connection.attempt")
	EndBusinessSpan(child, errors.New("device unavailable"))
	parent.End()

	var found bool
	for _, span := range recorder.Ended() {
		if span.Name() != "recorder.connection.attempt" {
			continue
		}
		found = true
		if span.SpanContext().TraceID() != parentContext.TraceID() {
			t.Fatalf("child trace ID = %s, want %s", span.SpanContext().TraceID(), parentContext.TraceID())
		}
		if span.Parent().SpanID() != parentContext.SpanID() {
			t.Fatalf("child parent span ID = %s, want %s", span.Parent().SpanID(), parentContext.SpanID())
		}
		if span.Status().Code != codes.Error || span.Status().Description != "operation failed" {
			t.Fatalf("unexpected child span status: %+v", span.Status())
		}
		if len(span.Events()) != 0 {
			t.Fatal("business span must not export raw exception messages")
		}
	}
	if !found {
		t.Fatal("child business span was not recorded")
	}
}
