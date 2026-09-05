package app

import (
	"context"
	"strings"
	"time"

	"nautilus/internal/config"
	"nautilus/internal/log"
	"nautilus/internal/observability/tracer"
	"nautilus/internal/observability/tracer/noop"
	"nautilus/internal/observability/tracer/otel"
	"nautilus/internal/server"
)

const otelShutdownTimeout = 5 * time.Second

func newTracer(ctx context.Context, srv *server.Server, logger *log.Logger) tracer.Tracer {
	endpointURL := config.Get[string]("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	if endpointURL == "" {
		endpointURL = otel.TracewayTraceEndpoint(config.Get[string]("OTEL_EXPORTER_OTLP_ENDPOINT"))
	}

	projectToken := config.Get[string]("TRACEWAY_PROJECT_TOKEN")
	if endpointURL == "" && !config.Get("OTEL_TRACES_ENABLED", false) {
		return noop.NewTracer()
	}

	headers := map[string]string{}
	if projectToken != "" {
		headers["Authorization"] = "Bearer " + projectToken
	}

	otelTracer, err := otel.NewOTLP(ctx, otel.Config{
		ServiceName: config.Get("OTEL_SERVICE_NAME", "nautilus"),
		EndpointURL: endpointURL,
		Headers:     headers,
		Insecure:    config.Get("OTEL_EXPORTER_OTLP_INSECURE", false) || config.Get("OTEL_EXPORTER_OTLP_TRACES_INSECURE", false),
		NoCompress:  otelCompressionDisabled(),
	})
	if err != nil {
		logger.Fatal("error creating otel tracer", "error", err)
	}

	srv.RegisterOnShutdown(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), otelShutdownTimeout)
		defer cancel()

		if err := otelTracer.Shutdown(shutdownCtx); err != nil {
			logger.Error("error shutting down otel tracer", "error", err)
		}
	})

	return otelTracer
}

func otelCompressionDisabled() bool {
	return strings.EqualFold(config.Get[string]("OTEL_EXPORTER_OTLP_COMPRESSION"), "none") ||
		strings.EqualFold(config.Get[string]("OTEL_EXPORTER_OTLP_TRACES_COMPRESSION"), "none")
}
