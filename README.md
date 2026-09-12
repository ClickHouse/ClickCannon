<h1 align="center">ClickCannon</h1>

# About

A program for replaying OTel data into ClickHouse and simulating concurrent user queries against it. Five independent modes can be run in any combination:

- **disk** — reads `.native`/`.native.zst` files from disk and feeds them to the insert workers
- **generate** — generates synthetic OTel data (logs, traces, or profiles) from a code-defined profile and feeds it to the insert workers
- **insert** — inserts data into ClickHouse via ch-go
- **metric_gen**: synthesizes OTLP metrics (all five types) and exports them over gRPC to an OTel collector endpoint
- **user** — simulates concurrent users running parameterized queries against ClickHouse

`disk` and `generate` are mutually exclusive data sources — enable one or the other. Each mode is independently toggled via `enabled` in the config. You can run `generate` + `insert` to load synthetic data, `disk` + `insert` to replay existing data, or `user` alone against an already-populated table. `metric_gen` is self-contained (it has its own generator and OTLP exporter) and can run alone or alongside `user`.

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
```

Adding a new generator profile means writing one Go file that calls `generate.RegisterProfile("name", builder)` from `init()`.

Generators available: `Pool/V`, `Const`, `RandStr(n).Prefix(p)`, `Hex(n).Prefix(p)`, `UUID()`, `IP().AsU32()/AsHex()`, `Int(min, max).Prefix(p)`, `Float(max).Precision(n).Prefix(p)`, `Bool(trueProb)`. Map columns use probabilistic key presence — each key has a per-row probability of appearing. `KP` produces unique keys (prefix + random hex) for thrashing LowCardinality dictionaries.

When generating traces, each worker independently produces complete traces with correlated `TraceId`/`SpanId`/`ParentSpanId` hierarchies. When generating profiles, each worker produces whole profiles — many unique-stack sample rows sharing a `ProfileId`, timestamp, duration, period, and resource attributes — where each row carries a random-depth call stack (function/file/mapping names, addresses, line numbers) and per-sample attributes. All randomness is seeded from `app.seed` for reproducible runs.

`profiles` is supported by the disk, generate, and insert pipelines only — the otel export sink does not support it.

## Metric generator (OTLP metrics)

The `metric_gen` mode is a self-contained OTLP metrics generator/exporter built to stress test the ClickHouse exporter's `metrics_schema: v2` pipeline. Instead of writing metric tables directly (which would duplicate the exporter's conversion logic), it emits OTLP over gRPC at a configurable rate and lets a real collector run the write path:

```
ClickCannon (metric_gen) ──OTLP/gRPC──▶ OTel Collector (clickhouseexporter, metrics_schema: v2) ──▶ ClickHouse
```

Three primary knobs control the workload:

1. **Number of unique metrics** (`metric_count`): names come from a fixed lookup table (`adjective_adjective_noun`), stable across runs.
2. **Cardinality per metric**: exponential decay `y = m * b^x` (`cardinality_m`, `cardinality_b`), where `x` is the metric's 1-based index in the table. A few head metrics carry most of the series; the tail is sparse.
3. **Rate** (`points_per_second`): total data points per second across all workers.

All five OTel metric types are generated (gauge, sum, histogram, exponential histogram, summary) with a configurable type mix, delta + cumulative temporality, monotonic and non-monotonic sums, int and double values, exemplars, and realistic resource/scope/point attributes. Series are pinned to a pool of simulated resources (pods) so resource attributes group like a real fleet, and optional `resource_lifetime` churn makes pods "restart" (new series identities) to stress the exporter's series cache.

Generation is **stateless and deterministic**: every point is a pure function of (seed, metric, series, sweep), so hundreds of millions of series cost no generator memory. Cumulative counters are strictly monotonic within an epoch and reset deterministically. Metric names, label sets, bounds, and quantiles derive from fixed seeds; only point values vary with `app.seed`, so saved dashboards and query workloads survive re-runs.

**Time model:** the generator works in *sweeps* over a virtual clock. Each sweep emits one point per series at `start_time + sweep*interval`; the rate limit controls how fast sweeps happen, decoupling virtual time from wall time. Two example shapes:

- **Breadth** (millions of series): `metric_count: 1000`, `cardinality_m: 50000`, `cardinality_b: 0.99` gives about 4.9M series; every sweep writes one point per series.
- **Depth** (millions of points per series): `metric_count: 50`, `cardinality_m: 20`, `cardinality_b: 1`, `interval: 1s`, `start_time: now-1440h`, `sweeps: 5000000`, unlimited rate backfills five million points per series as fast as the pipeline allows.

See the `metric_gen` section of `example.yaml` for every option.

### Benchmarking a collector (DPM)

DPM (data points per minute) is `points_per_second * 60`. Track achieved DPM as the rate of `metricgen_points_total` against the `target_metricgen_points_per_second` gauge.

metric_gen has no separate insert/otel toggle to disable for a passthrough baseline; generation and export are one step. Baseline against a no-op collector first (OTLP receiver, `debug`/`nop` exporter, no ClickHouse write) with `points_per_second: 0` to find the generation+gRPC ceiling, then point `url` at the real collector pipeline and compare. Achieved DPM below that ceiling means the collector or ClickHouse is the bottleneck, not the generator.

Watch `metricgen_export_latency_micros` for rising latency, the earliest saturation signal, and `metricgen_exports_failed_total` for batches dropped after retries.

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

You can split data across multiple files — each file becomes a unit of work for the disk reader threads.

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
