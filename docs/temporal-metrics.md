# Temporal worker metrics

Every `worker --queue=...` process exports Temporal SDK metrics at `GET /metrics`.
The default listener is `127.0.0.1:9090`. Set `TEMPORAL_METRICS_ADDRESS` to change
it; use distinct ports for multiple workers on one host. A bind failure fails
worker startup. On shutdown, workers and the Temporal client stop before the
metrics listener and OTel provider, whose cleanup has a two-second deadline.

```sh
TEMPORAL_METRICS_ADDRESS=127.0.0.1:9091 dotenvx run -- go run ./cmd/worker --queue=uploads
curl http://127.0.0.1:9091/metrics
```

Compose sets `0.0.0.0:9090` inside each worker container so a collector on
`app-network` can scrape `worker:9090`, `smoke-worker:9090`, and
`webhook-worker:9090`. It does not publish those ports to the host. The endpoint
has no authentication; keep it on the private monitoring network.

Add this job to an existing Prometheus instance attached to that network:

```yaml
scrape_configs:
  - job_name: nautilus-temporal-workers
    scrape_interval: 15s
    static_configs:
      - targets: [worker:9090, smoke-worker:9090, webhook-worker:9090]
```

The implementation uses Temporal's official OpenTelemetry metrics adapter and
the OTel Prometheus exporter. Each process owns its registry and meter provider;
it does not replace the global OTel provider or change tracing. Only worker
clients receive the metrics handler; app/API and command-line workflow clients
keep their existing behavior. This endpoint reports SDK metrics, not Temporal
server metrics or durable workflow state.

The constant `worker_queue` label identifies the worker process. SDK labels such
as `namespace`, `task_queue`, `workflow_type`, and `activity_type` remain available
where emitted. No document IDs or payloads are added as labels. Counters use
Prometheus `_total` names; timer histograms use seconds and `_seconds` names,
with bucket boundaries from 1 ms through two hours. A metric appears after the
SDK first records it, so an idle/new process need not expose every metric.

## Initial alerts

Tune these thresholds for workload volume and expected activity durations.
For a scrape outage, alert on `up{job="nautilus-temporal-workers"} == 0` for two
minutes. For SDK signals, start with the following PromQL expressions:

| Signal | Expression | Suggested duration |
| --- | --- | --- |
| Activity queue delay, p95 over 30 seconds | `histogram_quantile(0.95, sum by (le, worker_queue) (rate(temporal_activity_schedule_to_start_latency_seconds_bucket[5m]))) > 30` | 10 minutes |
| Repeated activity failures | `sum by (worker_queue, activity_type) (increase(temporal_activity_execution_failed_total[15m])) > 5` | 5 minutes |
| Workflow task execution failures | `sum by (worker_queue, workflow_type) (increase(temporal_workflow_task_execution_failed_total[5m])) > 0` | 5 minutes |
| Exhausted activity slots | `sum by (worker_queue) (temporal_worker_task_slots_available{worker_type="ActivityWorker"}) == 0` | 10 minutes |

Activity failures count failed attempts, including attempts that later recover;
workflow task failures can expose code/replay problems. Inspect the relevant
execution history in Temporal before treating an activity retry as final failure.
Queue latency is sampled when tasks start, so it cannot detect an entirely
unpolled backlog by itself: combine it with scrape availability and Temporal
server queue/backlog monitoring. Saturated slots alone can reflect healthy load;
use persistent queue latency to decide whether more worker capacity is needed.

References: [Temporal SDK metrics setup](https://docs.temporal.io/cloud/metrics/sdk-metrics-setup),
[SDK metrics reference](https://docs.temporal.io/references/sdk-metrics), and
[official Go OTel adapter](https://pkg.go.dev/go.temporal.io/sdk/contrib/opentelemetry).
