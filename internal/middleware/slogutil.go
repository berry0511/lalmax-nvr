package middleware

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// SetupLogger creates and configures a logger with the specified level and format.
// Returns a configured slog.Logger instance.
func SetupLogger(level, format string) *slog.Logger {
	// Parse level string to slog.Level
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo // default to info
	}

	// Create handler based on format
	var stdout slog.Handler
	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: false,
	}
	if strings.ToLower(format) == "json" {
		stdout = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		stdout = slog.NewTextHandler(os.Stdout, opts)
	}

	otelHandler := newLevelFilterHandler(otelslog.NewHandler(
		"github.com/lalmax-pro/lalmax-nvr",
		otelslog.WithSource(true),
	), logLevel)
	return slog.New(newTeeHandler(stdout, NewRingHandler(DefaultLogRing(), logLevel), otelHandler))
}

type levelFilterHandler struct {
	handler slog.Handler
	level   slog.Level
}

func newLevelFilterHandler(handler slog.Handler, level slog.Level) slog.Handler {
	return &levelFilterHandler{handler: handler, level: level}
}

func (h *levelFilterHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level && h.handler.Enabled(ctx, level)
}

func (h *levelFilterHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level < h.level {
		return nil
	}
	return h.handler.Handle(ctx, record)
}

func (h *levelFilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelFilterHandler{handler: h.handler.WithAttrs(attrs), level: h.level}
}

func (h *levelFilterHandler) WithGroup(name string) slog.Handler {
	return &levelFilterHandler{handler: h.handler.WithGroup(name), level: h.level}
}

// ComponentLogger creates a logger with a component attribute.
// Returns a logger that includes the component name in all log messages.
func ComponentLogger(name string) *slog.Logger {
	return slog.Default().With("component", name)
}
