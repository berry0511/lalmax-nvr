package observability

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Provider struct {
	traces            *sdktrace.TracerProvider
	metrics           *metric.MeterProvider
	logs              *sdklog.LoggerProvider
	localTraceEnabled atomic.Bool
}

type ProviderConfig struct {
	Enabled               bool
	Endpoint              string
	ServiceName           string
	TracesEnabled         bool
	MetricsEnabled        bool
	LogsEnabled           bool
	Headers               map[string]string
	Timeout               time.Duration
	MetricsExportInterval time.Duration
	LogsExportInterval    time.Duration
	LocalTraceStore       storage.TraceRepository
	LocalTraceEnabled     *bool
	TraceRetention        time.Duration
	MaxLocalTraces        int
}

func NewProvider(ctx context.Context, version string, cfg ProviderConfig) (*Provider, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.instance.id", uuid.NewString()),
		attribute.String("service.version", version),
	))
	if err != nil {
		return nil, err
	}

	provider := &Provider{}
	localTraceEnabled := cfg.LocalTraceEnabled == nil || *cfg.LocalTraceEnabled
	provider.localTraceEnabled.Store(localTraceEnabled)
	traceOptions := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if cfg.LocalTraceStore != nil {
		traceOptions = append(traceOptions, sdktrace.WithSpanProcessor(newTraceStoreProcessor(cfg.LocalTraceStore, cfg.TraceRetention, cfg.MaxLocalTraces, &provider.localTraceEnabled)))
	}
	if cfg.Enabled && cfg.TracesEnabled {
		exporter, exportErr := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(signalEndpoint(cfg.Endpoint, "traces")),
			otlptracehttp.WithHeaders(cloneHeaders(cfg.Headers)),
			otlptracehttp.WithTimeout(cfg.Timeout),
		)
		if exportErr != nil {
			return nil, fmt.Errorf("create OTLP trace exporter: %w", exportErr)
		}
		traceOptions = append(traceOptions, sdktrace.WithBatcher(exporter))
	}
	tracerProvider := sdktrace.NewTracerProvider(traceOptions...)

	metricOptions := []metric.Option{metric.WithResource(res)}
	if cfg.Enabled && cfg.MetricsEnabled {
		exporter, exportErr := otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithEndpointURL(signalEndpoint(cfg.Endpoint, "metrics")),
			otlpmetrichttp.WithHeaders(cloneHeaders(cfg.Headers)),
			otlpmetrichttp.WithTimeout(cfg.Timeout),
		)
		if exportErr != nil {
			_ = tracerProvider.Shutdown(ctx)
			return nil, fmt.Errorf("create OTLP metric exporter: %w", exportErr)
		}
		reader := metric.NewPeriodicReader(
			exporter,
			metric.WithInterval(cfg.MetricsExportInterval),
			metric.WithProducer(runtime.NewProducer()),
		)
		metricOptions = append(metricOptions, metric.WithReader(reader))
	}
	meterProvider := metric.NewMeterProvider(metricOptions...)

	var loggerProvider *sdklog.LoggerProvider
	if cfg.Enabled && cfg.LogsEnabled {
		exporter, exportErr := otlploghttp.New(ctx,
			otlploghttp.WithEndpointURL(signalEndpoint(cfg.Endpoint, "logs")),
			otlploghttp.WithHeaders(cloneHeaders(cfg.Headers)),
			otlploghttp.WithTimeout(cfg.Timeout),
		)
		if exportErr != nil {
			_ = meterProvider.Shutdown(ctx)
			_ = tracerProvider.Shutdown(ctx)
			return nil, fmt.Errorf("create OTLP log exporter: %w", exportErr)
		}
		loggerProvider = sdklog.NewLoggerProvider(
			sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter, sdklog.WithExportInterval(cfg.LogsExportInterval))),
		)
	} else {
		loggerProvider = sdklog.NewLoggerProvider(sdklog.WithResource(res))
	}

	if cfg.Enabled && cfg.MetricsEnabled {
		if runtimeErr := runtime.Start(runtime.WithMeterProvider(meterProvider)); runtimeErr != nil {
			_ = loggerProvider.Shutdown(ctx)
			_ = meterProvider.Shutdown(ctx)
			_ = tracerProvider.Shutdown(ctx)
			return nil, fmt.Errorf("instrument Go runtime metrics: %w", runtimeErr)
		}
	}

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otellogglobal.SetLoggerProvider(loggerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	provider.traces = tracerProvider
	provider.metrics = meterProvider
	provider.logs = loggerProvider
	return provider, nil
}

func (p *Provider) SetLocalTraceEnabled(enabled bool) {
	if p != nil {
		p.localTraceEnabled.Store(enabled)
	}
}

func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	return errors.Join(p.logs.Shutdown(ctx), p.metrics.Shutdown(ctx), p.traces.Shutdown(ctx))
}

func signalEndpoint(base, signal string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	u.Path = path.Join("/", u.Path, "v1", signal)
	return u.String()
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(headers))
	for name, value := range headers {
		cloned[name] = value
	}
	return cloned
}
