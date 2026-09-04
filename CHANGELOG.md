# Changelog

## Unreleased

### New Features

- **Profiles support** — A new `profiles` data type joins `logs` and `traces` across the disk, generate, and insert pipelines. Set `app.data_type: profiles` to replay pre-exported profile `.native` files (`disk.profiles_path`), generate synthetic profiles (each row a sample with a random-depth call stack, configurable via `generate.profiles`), and insert into a profiles table (`insert.clickhouse.profiles_table`).
- **OTel exporter** - Added experimental support for an OpenTelemetry OTLP exporter for disk/generated data. Both OTLP transports are supported via `otel.protocol`: `grpc` (default) and `http` (protobuf body, per-signal `/v1/logs` and `/v1/traces` paths appended automatically, gzip via `Content-Encoding`, and `Retry-After` honored on `429`/`503`).

## v0.4.0

### Breaking Changes

- **Metrics renamed to Prometheus conventions** — All counter metrics now use `_total` suffix. `*_per_second` accumulated metrics (which reset every second) are replaced by true monotonic counters.

| Old name | New name |
|---|---|
| `read_rows_per_second` | `disk_rows_total` |
| `read_compressed_bytes_per_second` | `disk_bytes_compressed_total` |
| `read_uncompressed_bytes_per_second` | `disk_bytes_uncompressed_total` |
| `insert_rows_per_second` | `insert_rows_total` |
| `insert_bytes_per_second` | `insert_bytes_compressed_total` |
| `insert_batches_per_second` | `insert_batches_total` |
| `user_queries_per_second` | `queries_ok_total` |
| `failed_user_queries_per_second` | `queries_failed_total` |
| `total_rows` | `disk_rows_total` |
| `total_bytes_compressed` | `disk_bytes_compressed_total` |
| `total_bytes_uncompressed` | `disk_bytes_uncompressed_total` |
| `program_num_gc` | `program_gc_runs_total` |
| `program_gc_pause_total_ns` | `program_gc_pause_ns_total` |
| `program_cpu_user_ns` | `program_cpu_user_ns_total` |
| `program_cpu_sys_ns` | `program_cpu_sys_ns_total` |

### Improvements

- **Preflight failure restarts query loop** — When `preflight_cadence: per_loop` is set and a workflow-level preflight fails (including `sql.ErrNoRows`), the worker now resets to the start of the query sequence and re-samples the time range and binds, instead of aborting the worker entirely. This avoids scheduler-level restarts with exponential backoff for transient preflight failures (e.g., sampled time range with no matching data). Other cadences (`once`, `per_query`) retain the previous behavior.

### New Features

- **Synthetic data generation (`generate` mode)** — A new `generate` mode produces OTel logs or traces directly in-process and feeds them to the insert workers, removing the need to pre-export `.native` files. Data shape is defined by code-built **profiles** (built-in: `otel_demo`) registered at `init()` time, so adding a new shape is one Go file. Configurable threads, rows-per-block, rows-per-second rate limit, and block reuse/retirement. Trace generation produces complete traces with correlated `TraceId`/`SpanId`/`ParentSpanId` hierarchies and configurable spans-per-trace, depth, and duration ranges. All randomness is seeded from `app.seed` for reproducible runs. `generate` and `disk` are mutually exclusive data sources.
- **Insert bytes uncompressed metric** — `insert_bytes_uncompressed_total` now tracks the uncompressed data size of inserts (from ClickHouse's `InsertedBytes` ProfileEvent), alongside the existing `insert_bytes_compressed_total` for wire bytes.
- **Per-worker insert metrics** — Four new counters track insert activity broken down by worker: `insert_rows_worker_total`, `insert_bytes_uncompressed_worker_total`, `insert_bytes_compressed_worker_total`, `insert_batches_worker_total`. Filter or group by `attributes['worker_id']` in queries.
- **Per-worker disk read metrics** — Same pattern for disk readers: `disk_rows_worker_total`, `disk_bytes_compressed_worker_total`, `disk_bytes_uncompressed_worker_total`.
- **Per-worker user query metrics** — `queries_ok_worker_total`, `queries_failed_worker_total`, keyed by `attributes['worker_id']`.
- **Grafana dashboard query improvements** — All counter panels now compute per-second rates using `lagInFrame` with proper `PARTITION BY metric_name`, replacing the old pre-computed rate metrics.
- **Query index attribute for queries** — The query latency metric now stores `query_index` in the attributes, perhaps useful for sorting a sequence of queries in a chart.
- **Preflight query counters** — `preflights_ok_total` and `preflights_failed_total` count individual preflight query executions.
- **Configurable metric attributes** — The metrics worker config now accepts an `attributes` map of key-value pairs that are attached to the run record and every emitted metric point. Useful for tagging runs by environment, team, hardware, etc.
- **Configurable log-normal sigma** — `log_normal` time ranges now accept a `sigma` field controlling spread and tail weight. Defaults to 0.5; typical range 0.3–1.5.

### Bug Fixes

- Fixed a bug where metrics could be lost between the send and reset steps of the metrics worker loop.

### Developer Experience

- **`plot-timerange` debug command** — Added a `cmd/plot-timerange` program that outputs sampled time range values from the configured distribution, useful for tuning `log_normal`/`exponential` parameters before running a full workload.

---

## v0.3.0

### Breaking Changes

- **Renamed "behaviors" to "workflows"** — The `behaviors` key in user config has been renamed to `workflows`.
- **Multiple preflight queries** — `preflight_query` was replaced with `preflight_queries` and now accepts one or more queries per workflow or per query.
 
Update your config files accordingly.

### New Features

#### Workflow & Query Configuration
- **Variables support** — Workflows and individual queries can now define variables that are interpolated at runtime.
- **Multiple preflight queries** — Workflows and queries now support multiple preflight queries (previously limited to one), configurable at both the workflow and query level. Workflow-level preflight queries can each run at an independent cadence (once, per loop, or per query).
- **Default query settings** — A `default_settings` block can now be specified to apply shared settings across all queries in a workflow.
- **Runtime duration for users** — Users now support a configurable `duration` field to limit how long a user runs before stopping.

#### Memory & Performance
- **Block retirement (memory leak mitigation)** — Blocks are now periodically retired and re-allocated to limit long-run memory growth caused by the ch-go driver's internal allocations. The retirement threshold is configurable.
- **Insert worker retirement** — Insert workers are retired and restarted after processing a configurable number of blocks, reducing potential memory leaks from the ch-go library.
- **Ring buffer speed limiter** — The disk reader's sliding-window rate limiter has been replaced with a more efficient ring buffer implementation.
- **ch-go slice caching** — Input and result slices for ch-go are now cached and reused across inserts to reduce allocations.
- **Disabled preallocated column slices** — Removed preallocated column slices in blocks to reduce per-block allocation overhead.

#### Observability & Metrics
- **Program metrics collection** — The metrics worker now captures runtime metrics about the program itself (goroutine counts, memory usage, etc.) rather than the underlying host machine.
- **CPU count metric** — A dedicated CPU count metric is now reported.
- **`create_schema` for metrics worker** — The metrics worker config now supports `create_schema` to auto-create the destination schema on startup.
- **Configurable pprof server** — A pprof HTTP server can now be enabled and configured via `pprof` settings in the config for runtime profiling.
- **Grafana dashboard updates** — Added program metrics panel, worker/block metrics, and a block retirement tracking panel to the bundled Grafana dashboard.

#### Telemetry Generation
- **ID shifting on loops** — When a workflow loops, telemetry IDs are now shifted on each iteration to prevent duplicate span/log IDs across loop cycles.
- **Configurable `TimestampTime` in logs** — Log data generation now supports toggling whether the `TimestampTime` field is included in emitted log records.

#### Developer Experience
- **Environment variable overrides** — `CLICKCANNON_RUN_ID` and `CLICKCANNON_CONFIG` environment variables can now be used to override the run ID and config file path without modifying the config file.
- **Node balancing optimization** — Insert workers no longer perform a host IP lookup when node balancing is disabled.

### Bug Fixes

- Fixed a bug where reusing blocks for logs data would incorrectly use the traces column constructor, causing schema mismatches.
- Fixed a potential block leak when a context was cancelled mid-flight during block acquisition.

---

## v0.2.0

A significant restructuring of internals focused on stability, config ergonomics, improved logging, and a more coherent code structure. This version also expanded query and user worker capabilities substantially.

### New Features

- **User workload simulation** — Introduced a user worker model that simulates realistic query behavior against ClickHouse, with configurable concurrency and timing.
- **Preflight queries** — Queries can now define a preflight query to look up dynamic values (e.g. time ranges, IDs) before execution. Supports multi-value binds and graceful skip on no-rows results.
- **Query settings** — Per-query ClickHouse settings (e.g. `max_threads`, `max_execution_time`) can now be specified in config.
- **Time range cadence & rounding** — Query time ranges now support a configurable cadence and automatic rounding for more realistic replay behavior.
- **Query time duration metric** — Query execution duration is now recorded as a metric.
- **Query latency metrics** — Point-in-time query latency is captured and surfaced in the Grafana dashboard.
- **File queue looping** — The disk reader can now loop the file queue indefinitely, with configurable time-shift behavior on each loop iteration.
- **Timestamp shift modes** — Multiple timestamp shift strategies are supported: `now`, relative offset, and minute-level shifting.
- **Run name & attributes** — A run name can be configured and is attached to metrics records for easier filtering across runs.
- **Block pool metrics** — Metrics for block pool utilization are now tracked and reported.
- **Target throughput metric** — Each insert worker now records its target MiB/s as a metric.
- **Node balancing** — Insert workers can balance connections across multiple ClickHouse nodes.

### Stability & Structure

- Restructured checkpoint logic for cleaner state management.
- Improved insert worker shutdown: close functions cleaned up to prevent log spam when the server is unreachable.
- Polished async error handling and reconnect logic for insert workers.
- Read workers are now synchronized to the first timestamp in the dataset for consistent replay alignment.
- Metrics worker is now cancelled after data workers finish, ensuring clean shutdown ordering.
- Replaced panics in setup paths with proper error returns.
- Common close/context-cancel errors are suppressed to reduce noise in logs.
- Config and run name are logged on startup.

### Bug Fixes

- Fixed a loop timing bug that caused incorrect replay pacing.
- Fixed incorrect detection of the first file in the queue.
- Fixed an obscure low-cardinality mismatched row values bug.
- Fixed hungry read threads starving insert threads under high load.
- Fixed format string replacement in HAR query replay.

---

## v0.1.0

Initial alpha release. Core pipeline is functional: reads OTel data from disk, rewrites timestamps, and inserts into ClickHouse at a configurable throughput. Not intended for production use — config format and behavior may change significantly between versions.

### Capabilities

- Reads traces and logs from disk and inserts into ClickHouse via the native ch-go protocol.
- Configurable insert throughput with an uncompressed speed limiter.
- Basic file cycling through a directory of data files.
- Node balancing across multiple ClickHouse endpoints.
- Metrics worker that writes operational metrics to a separate ClickHouse table.
- Query templating for parameterized ClickHouse queries.
- Multiple timestamp shift modes for replaying historical data as if it were live.
- Bundled Grafana dashboard for monitoring insert throughput and pipeline health.
- HAR file query replay (browser `.har` files), with parameter extraction and time range shifting.
- Docker support via included Dockerfile.
