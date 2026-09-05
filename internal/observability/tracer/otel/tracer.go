package otel

import (
	"context"
	"fmt"
	"time"

	"nautilus/internal/observability/tracer"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

const defaultServiceName = "nautilus"

var _ tracer.Tracer = (*Tracer)(nil)
var _ tracer.Span = (*Span)(nil)

type Config struct {
	ServiceName string
	TracerName  string
	EndpointURL string
	Headers     map[string]string
	Insecure    bool
	NoCompress  bool
	Timeout     time.Duration
}

func NewOTLP(ctx context.Context, config Config) (*Tracer, error) {
	if config.ServiceName == "" {
		config.ServiceName = defaultServiceName
	}
	if config.TracerName == "" {
		config.TracerName = config.ServiceName
	}

	var opts []otlptracehttp.Option
	if len(config.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(config.Headers))
	}
	if config.EndpointURL != "" {
		opts = append(opts, otlptracehttp.WithEndpointURL(config.EndpointURL))
	}
	if config.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if !config.NoCompress {
		opts = append(opts, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
	}
	if config.Timeout > 0 {
		opts = append(opts, otlptracehttp.WithTimeout(config.Timeout))
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otel: create otlp trace exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes("", attribute.String("service.name", config.ServiceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: create resource: %w", err)
	}

	provider := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
	)

	result := New(provider, config.TracerName)
	result.shutdown = provider.Shutdown
	return result, nil
}

func (t *Tracer) Shutdown(ctx context.Context) error {
	if t.shutdown == nil {
		return nil
	}
	if err := t.shutdown(ctx); err != nil {
		return fmt.Errorf("otel: shutdown tracer provider: %w", err)
	}
	return nil
}

func convertAttrs(attrs []tracer.Attribute) []attribute.KeyValue {
	converted := make([]attribute.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		converted = append(converted, convertAttr(attr))
	}
	return converted
}

func convertAttr(attr tracer.Attribute) attribute.KeyValue {
	key := attribute.Key(attr.Key)
	switch value := attr.Value.(type) {
	case string:
		return key.String(value)
	case int:
		return key.Int(value)
	case int64:
		return key.Int64(value)
	case bool:
		return key.Bool(value)
	case float64:
		return key.Float64(value)
	default:
		return key.String(fmt.Sprint(value))
	}
}
