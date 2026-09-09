package temporal

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	exporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"

	"nautilus/internal/errors"
	"nautilus/internal/log"
)

type Metrics struct {
	Handler  client.MetricsHandler
	Address  string
	server   *http.Server
	provider *metric.MeterProvider
	done     chan struct{}
}

// StartMetrics owns an isolated registry; it does not change global OTel state.
func StartMetrics(ctx context.Context, address, queue string) (*Metrics, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, errors.Wrap(err, "listen for Temporal metrics")
	}
	registry := prometheus.NewRegistry()
	reader, err := exporter.New(exporter.WithRegisterer(registry))
	if err != nil {
		_ = listener.Close()
		return nil, errors.Wrap(err, "create Temporal metrics exporter")
	}
	provider := metric.NewMeterProvider(metric.WithReader(reader), metric.WithView(metric.NewView(
		metric.Instrument{Kind: metric.InstrumentKindHistogram},
		metric.Stream{Aggregation: metric.AggregationExplicitBucketHistogram{
			Boundaries: []float64{0.001, 0.01, 0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300, 600, 1800, 7200},
		}},
	)))
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	m := &Metrics{
		Handler: opentelemetry.NewMetricsHandler(opentelemetry.MetricsHandlerOptions{
			Meter:                provider.Meter("temporal-sdk-go"),
			InitialAttributes:    attribute.NewSet(attribute.String("worker_queue", queue)),
			UseMonotonicCounters: true,
			OnError:              func(err error) { log.FromContext(ctx).Error("Temporal metrics failed", "error", err) },
		}),
		Address:  listener.Addr().String(),
		server:   &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second},
		provider: provider,
		done:     make(chan struct{}),
	}
	go func() {
		defer close(m.done)
		if err := m.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.FromContext(ctx).Error("Temporal metrics server failed", "error", err)
		}
	}()
	return m, nil
}

// Close remains bounded even when worker cancellation has already fired.
func (m *Metrics) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := m.server.Shutdown(ctx)
	if err != nil {
		_ = m.server.Close()
	}
	<-m.done
	if stopErr := m.provider.Shutdown(ctx); err == nil {
		err = stopErr
	}
	return errors.Wrap(err, "stop Temporal metrics")
}
