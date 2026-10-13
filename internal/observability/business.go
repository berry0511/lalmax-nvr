package observability

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const businessTracerName = "github.com/lalmax-pro/lalmax-nvr/business"
const httpClientTracerName = "github.com/lalmax-pro/lalmax-nvr/http-client"

// StartBusinessSpan starts a span for a domain operation. Attribute keys should
// use the nvr.* namespace and must not include credentials or full source URLs.
func StartBusinessSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(businessTracerName).Start(ctx, name, trace.WithAttributes(attrs...))
}

// RunBusinessOperation traces a bounded operation and returns its context to
// callers that need to nest further operations inside it.
func RunBusinessOperation(ctx context.Context, name string, operation func(context.Context) error, attrs ...attribute.KeyValue) (context.Context, error) {
	opCtx, span := StartBusinessSpan(ctx, name, attrs...)
	err := operation(opCtx)
	EndBusinessSpan(span, err)
	return opCtx, err
}

// EndBusinessSpan records an operation failure and ends its span.
func EndBusinessSpan(span trace.Span, err error) {
	if err != nil {
		// Errors from media libraries may embed RTSP/HLS URLs or credentials.
		// Keep spans useful for correlation without exporting the raw message.
		span.SetAttributes(attribute.String("error.type", fmt.Sprintf("%T", err)))
		span.SetStatus(codes.Error, "operation failed")
	}
	span.End()
}

// DetachedContext preserves trace and baggage values while detaching work from
// the lifetime of an HTTP request. The recorder still owns cancellation via its
// own context.WithCancel call.
func DetachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

// StartHTTPClientSpan starts a bounded client span and propagates W3C trace
// context to a trusted internal HTTP service. Baggage is intentionally omitted
// because it may contain values that should not cross service boundaries.
func StartHTTPClientSpan(ctx context.Context, req *http.Request) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{
		attribute.String("http.request.method", req.Method),
		attribute.String("url.scheme", req.URL.Scheme),
		attribute.String("server.address", req.URL.Hostname()),
		attribute.String("url.path", req.URL.Path),
	}
	if port := req.URL.Port(); port != "" {
		if n, err := strconv.Atoi(port); err == nil {
			attrs = append(attrs, attribute.Int("server.port", n))
		}
	}
	ctx, span := otel.Tracer(httpClientTracerName).Start(ctx, "HTTP "+req.Method,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))
	return ctx, span
}
