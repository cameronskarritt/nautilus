package temporal_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"nautilus/internal/temporal"
	"nautilus/internal/testutil/require"
)

func TestMetricsExport(t *testing.T) {
	t.Parallel()
	m, err := temporal.StartMetrics(t.Context(), "127.0.0.1:0", "uploads")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	handler := m.Handler.WithTags(map[string]string{"namespace": "nautilus", "task_queue": "uploads"})
	handler.Counter("temporal_activity_execution_failed").Inc(2)
	handler.Gauge("temporal_worker_task_slots_available").Update(3)
	handler.Timer("temporal_activity_schedule_to_start_latency").Record(2 * time.Second)

	response, err := http.Get("http://" + m.Address + "/metrics")
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	text := string(body)
	require.Contains(t, text, "# TYPE temporal_activity_execution_failed_total counter")
	require.Contains(t, text, "# TYPE temporal_worker_task_slots_available gauge")
	require.Contains(t, text, "# TYPE temporal_activity_schedule_to_start_latency_seconds histogram")
	labels := `namespace="nautilus",otel_scope_name="temporal-sdk-go",otel_scope_schema_url="",otel_scope_version="",task_queue="uploads",worker_queue="uploads"`
	require.Contains(t, text, "temporal_activity_execution_failed_total{"+labels+"} 2")
	require.Contains(t, text, "temporal_worker_task_slots_available{"+labels+"} 3")
	require.Contains(t, text, "temporal_activity_schedule_to_start_latency_seconds_sum{"+labels+"} 2")
	require.Contains(t, text, "temporal_activity_schedule_to_start_latency_seconds_bucket{"+labels+`,le="2"} 1`)
}

func TestMetricsLifecycle(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	m, err := temporal.StartMetrics(ctx, "127.0.0.1:0", "smoke")
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	_, err = temporal.StartMetrics(ctx, m.Address, "uploads")
	require.Error(t, err, "duplicate bind must fail startup")
	cancel()
	require.NoError(t, m.Close(), "shutdown must work after cancellation")
	listener, err := net.Listen("tcp", m.Address)
	require.NoError(t, err, "shutdown must release the listener before returning")
	require.NoError(t, listener.Close())
}
