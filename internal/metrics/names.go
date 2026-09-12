package metrics

type Name string

const (
	// Counters

	// Disk read counters

	DiskRowsTotal              Name = "disk_rows_total"
	DiskBytesCompressedTotal   Name = "disk_bytes_compressed_total"   // raw bytes read from disk (zstd-compressed file)
	DiskBytesUncompressedTotal Name = "disk_bytes_uncompressed_total" // bytes after zstd decompression (native protocol format)

	// Insert counters

	InsertRowsTotal              Name = "insert_rows_total"
	InsertBytesUncompressedTotal Name = "insert_bytes_uncompressed_total" // InsertedBytes ProfileEvent: uncompressed data size as seen by ClickHouse
	InsertBytesCompressedTotal   Name = "insert_bytes_compressed_total"   // NetworkReceiveBytes ProfileEvent: compressed bytes received over the wire
	InsertBatchesTotal           Name = "insert_batches_total"

	// Per-worker insert counters (attribute: worker_id)
	InsertRowsWorkerTotal              Name = "insert_rows_worker_total"
	InsertBytesUncompressedWorkerTotal Name = "insert_bytes_uncompressed_worker_total"
	InsertBytesCompressedWorkerTotal   Name = "insert_bytes_compressed_worker_total"
	InsertBatchesWorkerTotal           Name = "insert_batches_worker_total"

	// OTel export counters

	OTelRowsTotal          Name = "otel_rows_total"
	OTelBatchesTotal       Name = "otel_batches_total"
	OTelBytesTotal         Name = "otel_bytes_total" // marshaled OTLP request size sent over the wire
	OTelExportsFailedTotal Name = "otel_exports_failed_total"

	// Per-worker OTel export counters (attribute: worker_id)
	OTelRowsWorkerTotal    Name = "otel_rows_worker_total"
	OTelBatchesWorkerTotal Name = "otel_batches_worker_total"

	// Generate counters

	GenerateRowsTotal       Name = "generate_rows_total"
	GenerateBlocksTotal     Name = "generate_blocks_total"
	GenerateRowsWorkerTotal Name = "generate_rows_worker_total"

	// Metric generator (OTLP metrics export) counters

	MetricGenPointsTotal        Name = "metricgen_points_total"
	MetricGenRequestsTotal      Name = "metricgen_requests_total"
	MetricGenBytesTotal         Name = "metricgen_bytes_total" // marshaled OTLP request size sent over the wire
	MetricGenExportsFailedTotal Name = "metricgen_exports_failed_total"
	// MetricGenPointsRejectedTotal counts points rejected via OTLP partial
	// success responses; the requests themselves succeeded and are not retried.
	MetricGenPointsRejectedTotal Name = "metricgen_points_rejected_total"

	// Per-worker metric generator counters (attribute: worker_id)
	MetricGenPointsWorkerTotal Name = "metricgen_points_worker_total"
	MetricGenSweepsWorkerTotal Name = "metricgen_sweeps_worker_total"

	// Per-worker disk read counters (attribute: worker_id)
	DiskRowsWorkerTotal              Name = "disk_rows_worker_total"
	DiskBytesCompressedWorkerTotal   Name = "disk_bytes_compressed_worker_total"
	DiskBytesUncompressedWorkerTotal Name = "disk_bytes_uncompressed_worker_total"

	// Per-worker user query counters (attribute: worker_id)
	QueriesOkWorkerTotal      Name = "queries_ok_worker_total"
	QueriesFailedWorkerTotal  Name = "queries_failed_worker_total"
	BlocksRetiredTotal        Name = "blocks_retired_total"
	InsertWorkersRetiredTotal Name = "insert_workers_retired_total"

	// User query counters

	QueriesOkTotal     Name = "queries_ok_total"
	QueriesFailedTotal Name = "queries_failed_total"

	// Preflight query counters

	PreflightsOkTotal     Name = "preflights_ok_total"
	PreflightsFailedTotal Name = "preflights_failed_total"

	// Counters — Go runtime

	ProgramGCRunsTotal    Name = "program_gc_runs_total"
	ProgramGCPauseNsTotal Name = "program_gc_pause_ns_total"
	ProgramCPUUserNsTotal Name = "program_cpu_user_ns_total"
	ProgramCPUSysNsTotal  Name = "program_cpu_sys_ns_total"

	// Gauges

	TargetBytesPerSecond        Name = "target_bytes_per_second"
	TargetGenerateRowsPerSecond Name = "target_generate_rows_per_second"
	TargetWorkerBytesPerSecond  Name = "target_worker_bytes_per_second"

	TargetMetricGenPointsPerSecond Name = "target_metricgen_points_per_second"

	ActiveGenerators       Name = "active_generators"
	ActiveReaders          Name = "active_readers"
	ActiveInserters        Name = "active_inserters"
	ActiveOTelExporters    Name = "active_otel_exporters"
	ActiveMetricGenWorkers Name = "active_metricgen_workers"
	ActiveUsers            Name = "active_users"

	BlockPoolCount    Name = "block_pool_count"
	BlockPoolCapacity Name = "block_pool_capacity"
	BlockQueueLength  Name = "block_queue_length"

	// Gauges — Go runtime

	ProgramHeapAllocBytes Name = "program_heap_alloc_bytes"
	ProgramSysBytes       Name = "program_sys_bytes"
	ProgramNumGoroutines  Name = "program_num_goroutines"
	ProgramNextGCBytes    Name = "program_next_gc_bytes"
	ProgramNumCPU         Name = "program_num_cpu"

	// Samples

	QueryLatencyMicros     Name = "query_latency_micros"
	PreflightLatencyMicros Name = "preflight_latency_micros"

	// Per-flush OTel export latency (attributes: worker_id, rows)
	OTelExportLatencyMicros Name = "otel_export_latency_micros"

	// Per-flush metric generator export latency (attributes: worker_id, points)
	MetricGenExportLatencyMicros Name = "metricgen_export_latency_micros"
)
