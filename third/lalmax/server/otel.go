package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/q191201771/lalmax/server"

// otelMiddleware instruments the embedded lalmax HTTP surface, including its
// control API, HLS/FMP4 endpoints and WebRTC HTTP handlers.
func otelMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// These handlers remain open for the lifetime of an event or media
		// connection. Their establishment events are traced separately.
		if c.Request.URL.Path == "/api/hook/stream" || strings.HasSuffix(c.Request.URL.Path, ".flv") {
			c.Next()
			return
		}

		ctx := propagation.TraceContext{}.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		ctx, span := otel.Tracer(tracerName).Start(ctx, c.Request.Method,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.request.method", c.Request.Method),
				attribute.String("url.scheme", requestScheme(c.Request)),
				attribute.String("server.address", c.Request.Host),
			),
		)
		c.Request = c.Request.WithContext(ctx)

		defer func() {
			if recovered := recover(); recovered != nil {
				span.SetAttributes(attribute.String("exception.type", fmt.Sprintf("%T", recovered)))
				span.SetStatus(codes.Error, "request panicked")
				span.End()
				panic(recovered)
			}

			route := c.FullPath()
			if route == "" {
				route = "unmatched"
			}
			status := c.Writer.Status()
			span.SetName(c.Request.Method + " " + route)
			span.SetAttributes(
				attribute.String("http.route", route),
				attribute.Int("http.response.status_code", status),
			)
			if status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(status))
			}
			span.End()
		}()

		c.Next()
	}
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
