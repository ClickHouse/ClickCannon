<h1 align="center">ClickCannon</h1>

# About

A program for replaying OTel data into ClickHouse and simulating concurrent user queries against it. Four independent modes can be run in any combination:

- **disk** — reads `.native`/`.native.zst` files from disk and feeds them to the insert workers
- **generate** — generates synthetic OTel data (logs, traces, profiles, or metrics) from a code-defined profile and feeds it to the insert workers
- **insert** — inserts data into ClickHouse via ch-go
- **user** — simulates concurrent users running parameterized queries against ClickHouse

`disk` and `generate` are mutually exclusive data sources — enable one or the other. Each mode is independently toggled via `enabled` in the config. You can run `generate` + `insert` to load synthetic data, `disk` + `insert` to replay existing data, or `user` alone against an already-populated table.

([development blog post](https://clickhouse.com/blog/building-clickcannon-a-tool-for-benchmark-clickhouse))

# Usage

Copy `example.yaml`, edit it for your environment, and enable the modes you want.

Run with Go:
```sh
go run clickcannon --config my-config.yaml
```

Or build a binary first:
```sh
go build -o clickcannon . && ./clickcannon --config my-config.yaml
```

Or with Docker (mount your config and data):
```sh
docker build -t clickcannon .
docker run -v $(pwd)/my-config.yaml:/root/my-config.yaml \
           -v $(pwd)/trace_data:/root/trace_data \
           clickcannon ./clickcannon --config /root/my-config.yaml
```

The config path can also be set via environment variable:
```sh
CLICKCANNON_CONFIG=my-config.yaml go run clickcannon
```

By default a random UUID is generated as the run ID each time the program starts. To set a specific run ID, set `CLICKCANNON_RUN_ID`:
```sh
CLICKCANNON_RUN_ID=my-run-id go run clickcannon --config my-config.yaml
```

# Data Sources

ClickCannon supports two data sources: disk replay and synthetic generation. Use one or the other.

## Generate (synthetic data)

The generate mode creates synthetic OTel data directly — no pre-exported files needed. Data shape is defined by a code-built **profile** registered at init() time in `internal/generate/profile_*.go`. `otel_demo` is a built-in profile for generic OTel demo data. Pick one in YAML:

```yaml
generate:
  enabled: true
  threads: 8
  rows_per_block: 8192
  rows_per_second: 0  # 0 = unlimited
  reuse_blocks: true
  block_retirement_uses: 50
  # Name of a code-defined profile. Defaults to otel_demo.
  profile: otel_demo
  # Trace-specific settings (only used when data_type: traces)
  traces:
    spans_per_trace_min: 3
    spans_per_trace_max: 12
    max_depth: 5
    duration_min_us: 1000
    duration_max_us: 5000000
  # Profile-specific settings (only used when data_type: profiles)
  profiles:
    samples_per_profile_min: 50
    samples_per_profile_max: 500
    stack_depth_min: 5
    stack_depth_max: 64
    duration_min_ms: 1000
    duration_max_ms: 60000
    period_ns: 10000000
  # Metric-specific settings (only used when data_type: metrics)
  metrics:
    points_per_series_min: 10
    points_per_series_max: 120
    point_interval_seconds: 15
    histogram_buckets: 18
    exp_histogram_buckets: 40
    exp_histogram_scale: 3
```

Adding a new generator profile means writing one Go file that calls `generate.RegisterProfile("name", builder)` from `init()`.

Generators available: `Pool/V`, `Const`, `RandStr(n).Prefix(p)`, `Hex(n).Prefix(p)`, `UUID()`, `IP().AsU32()/AsHex()`, `Int(min, max).Prefix(p)`, `Float(max).Precision(n).Prefix(p)`, `Bool(trueProb)`. Map columns use probabilistic key presence — each key has a per-row probability of appearing. `KP` produces unique keys (prefix + random hex) for thrashing LowCardinality dictionaries.

When generating traces, each worker independently produces complete traces with correlated `TraceId`/`SpanId`/`ParentSpanId` hierarchies. When generating profiles, each worker produces whole profiles — many unique-stack sample rows sharing a `ProfileId`, timestamp, duration, period, and resource attributes — where each row carries a random-depth call stack (function/file/mapping names, addresses, line numbers) and per-sample attributes. All randomness is seeded from `app.seed` for reproducible runs.

When generating metrics, each worker produces whole series — data points sharing a `MetricName`, `ServiceName`, resource/scope/datapoint attributes, and `StartTimeUnix`, with `TimeUnix` advancing by the collection interval. Set `app.metrics_type` to pick which OTel metrics table schema is produced (`gauge`, `sum`, `histogram`, `exponential_histogram`, or `summary`); one type is targeted per run. Sums are cumulative (mostly monotonic) counters, histogram bucket counts/sums/min/max are internally consistent, and exemplar columns are emitted empty.

`profiles` is supported by the disk, generate, and insert pipelines only — the otel export sink does not support it. `metrics` is supported by all pipelines including the otel export sink.

## Disk (replay from files)

Replays pre-exported data from disk.

Export logs:
```sql
SELECT * FROM otel.otel_logs LIMIT 10000000 INTO OUTFILE 'log_data/logs.native.zst' COMPRESSION 'zstd' FORMAT Native
```

Export traces:
```sql
SELECT * FROM otel.otel_traces LIMIT 10000000 INTO OUTFILE 'trace_data/traces.native.zst' COMPRESSION 'zstd' FORMAT Native
```

Export profiles:
```sql
SELECT * FROM otel.otel_profiles LIMIT 10000000 INTO OUTFILE 'profile_data/profiles.native.zst' COMPRESSION 'zstd' FORMAT Native
```

Export metrics (one directory per metrics table; set `app.metrics_type` to match):
```sql
SELECT * FROM otel.otel_metrics_gauge LIMIT 10000000 INTO OUTFILE 'metrics_gauge_data/gauge.native.zst' COMPRESSION 'zstd' FORMAT Native
```

You can split data across multiple files — each file becomes a unit of work for the disk reader threads.

# Benchmarking an OTel Collector with Metrics (DPM)

The otel export sink can drive an OTLP/gRPC collector endpoint with generated (or disk-replayed) metrics to measure the collector's ingestion throughput in data points per minute (DPM). Enable `generate` + `otel` with `data_type: metrics`:

```yaml
app:
  data_type: metrics
  metrics_type: gauge   # one table schema per run
generate:
  enabled: true
  threads: 4
  rows_per_block: 2000
  rows_per_second: 166667   # 10M DPM target; 0 = unlimited
otel:
  enabled: true
  url: collector-host:4317
  threads: 4
  batch_size: 2000
```

**DPM targeting.** Each metrics row is one data point, so `rows_per_second = target DPM / 60`. Achieved DPM is `rate(otel_rows_total) * 60` in the performance metrics.

**Batch sizing matters.** OTLP metric data points are large (~1 KiB each with attributes); collectors default to a 4 MiB gRPC message limit. Keep `otel.batch_size` (and `generate.rows_per_block`, which is the minimum flush granularity) at or below ~2000 rows, or raise the collector's `max_recv_msg_size_mib`. Oversized batches fail with `ResourceExhausted: received message larger than max` and are dropped after retries.

**Finding the ceiling.** First baseline the generator itself: run with both `insert` and `otel` disabled (passthrough mode) to measure the pure generation rate, then against a local no-op collector (`debug` exporter) to measure the conversion+gRPC ceiling. The target collector measurement is only meaningful below both. Then either step `rows_per_second` up across runs until achieved DPM flattens below target, or run unlimited (`rows_per_second: 0`) and let gRPC backpressure find the ceiling. Watch:

- `otel_rows_total` rate flattening below target — the collector is the bottleneck
- `otel_export_latency_micros` samples rising — earliest saturation signal, before failures appear
- `otel_exports_failed_total` — batches dropped after retry exhaustion
- Collector-side `otelcol_receiver_accepted_metric_points` / `otelcol_receiver_refused_metric_points` for ground truth

**Semantics.** Exported metrics preserve OTLP structure: data points are grouped into Metrics by (name, unit, description) under their resource/scope; sums carry cumulative temporality and monotonicity at the metric level; histogram bucket counts, bounds, min/max, and summary quantiles pass through as generated. Exemplars are not exported.

# Memory Management

ClickCannon includes two workarounds for memory growth that occurs during long runs. Both are caused by ch-go accumulating allocations over time and are addressed by periodic retirement of the relevant objects.

## Block retirement (`disk.block_retirement_uses`)

When `disk.reuse_blocks` is enabled, native blocks read from disk are recycled rather than garbage collected after each insert. This improves throughput stability by avoiding GC pressure, but ch-go has a quirk where column slice backing arrays grow each time a block is reset and refilled — memory is never returned. Over a long run this causes steady memory growth.

`block_retirement_uses` sets a limit on how many times a block can be reused before it is discarded and replaced with a fresh allocation. Setting this to a reasonable value bounds the growth without giving up the throughput benefits.

**Deriving a value:** Check your Grafana dashboard for memory growth rate and insert throughput. A block is retired after N uses regardless of size, so a lower value means more frequent fresh allocations (more GC) but tighter memory bounds. 100 is a reasonable starting point. If memory is still growing, lower it; if GC pauses are visible in throughput, raise it.

Set to `0` to disable retirement (blocks live for the program's lifetime, original behavior).

## Insert worker retirement (`insert.worker_retirement_batches`)

The ch-go encoder inside each insert worker accumulates buffer allocations over time as it encodes blocks. These buffers grow to fit the largest block seen and are never shrunk. Over many batches this causes each worker's memory footprint to drift upward.

`worker_retirement_batches` sets how many batches a worker sends before it exits and is replaced by a fresh one. Workers are staggered so they don't all restart simultaneously: each worker i gets an initial batch offset of `(i * retirement_batches) / threads`. It then counts from that offset and retires after sending exactly `retirement_batches` batches, so every worker sends the same number regardless of its position. The offsets spread retirements evenly across the retirement window, and because the offset is recalculated from the stable worker ID on each restart, the stagger is maintained for the life of the program.

**Deriving a value:** Estimate your target throughput in batches per second (throughput / `insert.batch_size`), then decide how often you want workers to recycle. For a run targeting 1M rows/s with `batch_size=100000`, that's ~10 batches/s; retiring every 100 batches means a recycle roughly every 10 seconds per worker. Lower values reduce peak memory per worker but add reconnection overhead. Higher values allow more drift.

Set to `0` to disable retirement (workers run indefinitely, original behavior).

# Grafana

A Grafana dashboard is included in `grafana.json`. Import it via Dashboards > Import in the Grafana UI. It reads metrics from the ClickHouse server configured under `metrics` in your config.

### Disk & Insert panels

![Example of disk and insert dashboard](.static/grafana_disk_and_insert_dashboard.png)


### User Query panels

![Example of user dashboard](.static/grafana_user_dashboard.png)
