package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"otelspam/internal/block"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type Worker struct {
	log *slog.Logger

	runID     string
	conn      driver.Conn
	insertSQL string

	dataType             string
	targetBytesPerSecond uint64
	configAttributes     map[string]string

	blockPool  block.Pool
	blockQueue chan block.SharedColumns

	metricsQueue chan Entry
	mu           sync.Mutex
	metrics      map[Name]uint64
	pointMetrics []Entry
}

func NewWorker(log *slog.Logger, runID, configName, dataType string, targetBytesPerSecond uint64, runAttr map[string]string, cfg *Config, blockPool block.Pool, blockQueue chan block.SharedColumns) (*Worker, error) {
	w := Worker{
		log:                  log.With("component", "metrics_worker", "data_type", dataType),
		runID:                runID,
		dataType:             dataType,
		targetBytesPerSecond: targetBytesPerSecond,
		configAttributes:     runAttr,

		blockPool:  blockPool,
		blockQueue: blockQueue,

		metricsQueue: make(chan Entry, 10_000),
		metrics:      make(map[Name]uint64),
		pointMetrics: make([]Entry, 0, 10_000),
	}

	if cfg.ClickHouseDSN != "" {
		opt, err := clickhouse.ParseDSN(cfg.ClickHouseDSN)
		if err != nil {
			return nil, fmt.Errorf("failed to parse DSN: %w", err)
		}

		w.conn, err = clickhouse.Open(opt)
		if err != nil {
			return nil, fmt.Errorf("failed to connect: %w", err)
		}
		w.log.Info("clickhouse connected")

		if cfg.CreateSchema {
			err = w.conn.Exec(context.Background(), fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %q", cfg.Database))
			if err != nil {
				return nil, fmt.Errorf("failed to create metrics database: %w", err)
			}

			runDDL := fmt.Sprintf(`
				CREATE TABLE IF NOT EXISTS %q.%q (
					run_id String,
					name String,
					timestamp DateTime64(3),
				    data_type LowCardinality(String),
				    target_bytes_per_second UInt64,
				    attributes Map(LowCardinality(String), String)
				) Engine = MergeTree()
				ORDER BY (run_id, timestamp)
			`, cfg.Database, cfg.RunTable)

			err = w.conn.Exec(context.Background(), runDDL)
			if err != nil {
				return nil, fmt.Errorf("failed to create run table: %w", err)
			}
		}

		insertRunSQL := fmt.Sprintf(`INSERT INTO %q.%q VALUES (?, ?, ?, ?, ?, ?)`, cfg.Database, cfg.RunTable)
		err = w.conn.Exec(context.Background(), insertRunSQL, runID, configName, time.Now(), dataType, targetBytesPerSecond, runAttr)
		if err != nil {
			return nil, fmt.Errorf("failed to insert run: %w", err)
		}
		w.log.Info("inserted run info")

		if cfg.CreateSchema {
			metricsDDL := fmt.Sprintf(`
				CREATE TABLE IF NOT EXISTS %q.%q (
					run_id String,
					metric_name LowCardinality(String),
					timestamp DateTime64(3),
					value UInt64,
					attributes Map(LowCardinality(String), String)
				) Engine = MergeTree()
				ORDER BY (run_id, metric_name, timestamp)
			`, cfg.Database, cfg.MetricsTable)

			err = w.conn.Exec(context.Background(), metricsDDL)
			if err != nil {
				return nil, fmt.Errorf("failed to create metrics table: %w", err)
			}
		}

		w.insertSQL = fmt.Sprintf(`INSERT INTO %q.%q VALUES (?, ?, ?, ?, ?)`, cfg.Database, cfg.MetricsTable)
	}

	return &w, nil
}

func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("started")
	defer w.log.Info("stopped")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m := <-w.metricsQueue:
			w.applyMetricEntry(m)
		case <-ticker.C:
			w.collectInternalMetrics()

			if w.insertSQL != "" {
				snapshot, pointSnapshot := w.snapshotAndReset()
				go w.pushMetricsSnapshot(ctx, snapshot, pointSnapshot)
			} else {
				w.resetMetrics()
			}
		}
	}
}

func (w *Worker) applyMetricEntry(m Entry) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, ok := w.metrics[m.Name]; !ok && m.Mode != EntryModePoint {
		w.metrics[m.Name] = 0
	}

	switch m.Mode {
	case EntryModeIncrement:
		w.metrics[m.Name] += m.Value
	case EntryModeDecrement:
		if m.Value >= w.metrics[m.Name] {
			w.metrics[m.Name] = 0
		} else {
			w.metrics[m.Name] -= m.Value
		}
	case EntryModeSet:
		w.metrics[m.Name] = m.Value
	case EntryModePoint:
		w.pointMetrics = append(w.pointMetrics, m)
	}
}

// collectInternalMetrics gathers block pool, runtime, and system metrics
// directly under the lock, avoiding the metrics channel entirely.
func (w *Worker) collectInternalMetrics() {
	w.mu.Lock()
	defer w.mu.Unlock()

	blockPoolCount, blockPoolCapacity := w.blockPool.Stats()
	w.metrics[BlockPoolCount] = uint64(blockPoolCount)
	w.metrics[BlockPoolCapacity] = uint64(blockPoolCapacity)
	w.metrics[BlockQueueLength] = uint64(len(w.blockQueue))
	w.metrics[BlocksRetiredTotal] = uint64(w.blockPool.TotalRetired())
	w.metrics[TargetBytesPerSecond] = w.targetBytesPerSecond

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	w.metrics[ProgramHeapAllocBytes] = ms.HeapAlloc
	w.metrics[ProgramSysBytes] = ms.Sys
	w.metrics[ProgramNumGoroutines] = uint64(runtime.NumGoroutine())
	w.metrics[ProgramNumGC] = uint64(ms.NumGC)
	w.metrics[ProgramPauseTotalNs] = ms.PauseTotalNs
	w.metrics[ProgramNextGCBytes] = ms.NextGC
	w.metrics[ProgramNumCPU] = uint64(runtime.NumCPU())

	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err == nil {
		w.metrics[ProgramCPUUserNs] = uint64(ru.Utime.Nano())
		w.metrics[ProgramCPUSysNs] = uint64(ru.Stime.Nano())
	}
}

func (w *Worker) resetMetrics() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.resetMetricsLocked()
}

// resetMetricsLocked zeroes resettable counters and clears point metrics.
// Caller must hold w.mu.
func (w *Worker) resetMetricsLocked() {
	for name := range w.metrics {
		switch name {
		case TotalRows:
		case TotalBytesCompressed:
		case TotalBytesUncompressed:
		case TargetBytesPerSecond:
		case TargetWorkerBytesPerSecond:
		case ActiveReaders:
		case ActiveInserters:
		case ActiveUsers:
		case BlockPoolCount:
		case BlockPoolCapacity:
		case BlockQueueLength:
		case BlocksRetiredTotal:
		case InsertWorkersRetiredTotal:
		case ProgramHeapAllocBytes:
		case ProgramSysBytes:
		case ProgramNumGoroutines:
		case ProgramNumGC:
		case ProgramPauseTotalNs:
		case ProgramNextGCBytes:
		case ProgramCPUUserNs:
		case ProgramCPUSysNs:
		case ProgramNumCPU:
		default:
			w.metrics[name] = 0
		}
	}

	w.pointMetrics = w.pointMetrics[:0]
}

// snapshotAndReset copies the current metrics state and resets counters,
// returning owned copies safe to use from another goroutine.
func (w *Worker) snapshotAndReset() (map[Name]uint64, []Entry) {
	w.mu.Lock()
	defer w.mu.Unlock()

	snapshot := make(map[Name]uint64, len(w.metrics))
	maps.Copy(snapshot, w.metrics)

	var pointSnapshot []Entry
	if len(w.pointMetrics) > 0 {
		pointSnapshot = make([]Entry, len(w.pointMetrics))
		copy(pointSnapshot, w.pointMetrics)
	}

	w.resetMetricsLocked()

	return snapshot, pointSnapshot
}

// pushMetricsSnapshot sends a pre-built snapshot to ClickHouse.
// Safe to call from a goroutine since it owns the snapshot data.
func (w *Worker) pushMetricsSnapshot(ctx context.Context, snapshot map[Name]uint64, pointSnapshot []Entry) {
	batch, err := w.conn.PrepareBatch(ctx, w.insertSQL)
	if err != nil {
		w.log.Error("failed to prepare metrics batch", "err", err)
		return
	}
	defer func(batch driver.Batch) {
		batchErr := batch.Close()
		if batchErr != nil {
			w.log.Error("failed to close batch", "err", batchErr)
		}
	}(batch)

	now := time.Now()
	for name, value := range snapshot {
		err = batch.Append(w.runID, string(name), now, value, w.mergeAttributes(nil))
		if err != nil {
			w.log.Error("failed to append metric to batch", "name", name, "value", value, "err", err)
			return
		}
	}

	for _, m := range pointSnapshot {
		err = batch.Append(w.runID, string(m.Name), m.Timestamp, m.Value, w.mergeAttributes(m.Attributes))
		if err != nil {
			w.log.Error("failed to append point metric to batch", "name", m.Name, "value", m.Value, "err", err)
			return
		}
	}

	err = batch.Send()
	if err != nil {
		w.log.Error("failed to send metrics", "err", err)
		return
	}

	w.log.Debug("pushed metrics", "count", batch.Rows())
}

// mergeAttributes returns config attributes merged with per-metric attributes.
// Per-metric attributes take precedence over config attributes.
func (w *Worker) mergeAttributes(pointAttr map[string]string) map[string]string {
	if len(w.configAttributes) == 0 {
		if pointAttr == nil {
			return map[string]string{}
		}
		return pointAttr
	}

	merged := make(map[string]string, len(w.configAttributes)+len(pointAttr))
	for k, v := range w.configAttributes {
		merged[k] = v
	}
	for k, v := range pointAttr {
		merged[k] = v
	}
	return merged
}

func (w *Worker) IncrementMetric(name Name, delta uint64) {
	select {
	case w.metricsQueue <- Entry{
		Mode:  EntryModeIncrement,
		Name:  name,
		Value: delta,
	}:
	default:
	}
}

func (w *Worker) DecrementMetric(name Name, delta uint64) {
	select {
	case w.metricsQueue <- Entry{
		Mode:  EntryModeDecrement,
		Name:  name,
		Value: delta,
	}:
	default:
	}
}

func (w *Worker) SetMetric(name Name, value uint64) {
	select {
	case w.metricsQueue <- Entry{
		Mode:  EntryModeSet,
		Name:  name,
		Value: value,
	}:
	default:
	}
}

func (w *Worker) GetMetric(name Name) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.metrics[name]
}

func (w *Worker) AddMetricPoint(name Name, value uint64) {
	select {
	case w.metricsQueue <- Entry{
		Mode:      EntryModePoint,
		Timestamp: time.Now(),
		Name:      name,
		Value:     value,
	}:
	default:
	}
}

func (w *Worker) AddMetricPointWithAttributes(name Name, value uint64, attributes map[string]string) {
	select {
	case w.metricsQueue <- Entry{
		Mode:       EntryModePoint,
		Timestamp:  time.Now(),
		Name:       name,
		Attributes: attributes,
		Value:      value,
	}:
	default:
	}
}
