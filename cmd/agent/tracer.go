package main

import (
	"context"
	"strings"
	"time"

	"nautilus/internal/config"
	"nautilus/internal/log"
	"nautilus/internal/observability/tracer"
	"nautilus/internal/observability/tracer/noop"
	"nautilus/internal/observability/tracer/otel"
)

const otelShutdownTimeout = 5 * time.Second

func newTracer(ctx context.Context, logger *log.Logger) (tracer.Tracer, func(context.Context) error) {
	endpointURL := config.Get[string]("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	if endpointURL == "" {
		endpointURL = otel.TracewayTraceEndpoint(config.Get[string]("OTEL_EXPORTER_OTLP_ENDPOINT"))
	}

	projectToken := config.Get[string]("TRACEWAY_PROJECT_TOKEN")
	if endpointURL == "" && !config.Get("OTEL_TRACES_ENABLED", false) {
		return noop.NewTracer(), func(context.Context) error {
			return nil
		}
	}

	headers := map[string]string{}
	if projectToken != "" {
		headers["Authorization"] = "Bearer " + projectToken
	}

	otelTracer, err := otel.NewOTLP(ctx, otel.Config{
		ServiceName: config.Get("OTEL_SERVICE_NAME", "nautilus-agent"),
		EndpointURL: endpointURL,
		Headers:     headers,
		Insecure:    config.Get("OTEL_EXPORTER_OTLP_INSECURE", false) || config.Get("OTEL_EXPORTER_OTLP_TRACES_INSECURE", false),
		NoCompress:  otelCompressionDisabled(),
	})
	if err != nil {
		logger.Fatal("error creating otel tracer", "error", err)
	}

	return otelTracer, func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); ok {
			return otelTracer.Shutdown(ctx)
		}

		shutdownCtx, cancel := context.WithTimeout(ctx, otelShutdownTimeout)
		defer cancel()
		return otelTracer.Shutdown(shutdownCtx)
	}
}

func otelCompressionDisabled() bool {
	return strings.EqualFold(config.Get[string]("OTEL_EXPORTER_OTLP_COMPRESSION"), "none") ||
		strings.EqualFold(config.Get[string]("OTEL_EXPORTER_OTLP_TRACES_COMPRESSION"), "none")
}
