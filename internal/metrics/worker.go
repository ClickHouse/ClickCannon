package metrics

import (
	"clickcannon/internal/block"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// metricKey identifies an aggregated metric. AttrKey/AttrValue are empty for
// global metrics and non-empty for per-entity metrics (e.g. per-worker counters).
type metricKey struct {
	Name      Name
	AttrKey   string
	AttrValue string
}

type Worker struct {
	log *slog.Logger

	runID     string
	conn      driver.Conn
	insertSQL string

	dataType                    string
	targetBytesPerSecond        uint64
	targetGenerateRowsPerSecond uint64

	configAttributes map[string]string

	blockPool  block.Pool
	blockQueue chan block.SharedColumns

	metricsQueue chan Entry
	mu           sync.Mutex
	metrics      map[metricKey]uint64
	pointMetrics []Entry

	// otlp is the optional OTLP exporter for the self-metrics, additive to the
	// ClickHouse perf sink. Nil when disabled.
	otlp *otlpEmitter
}

func NewWorker(log *slog.Logger, runID, configName, dataType string, targetBytesPerSecond, targetGenerateRowsPerSecond uint64, runAttr map[string]string, cfg *Config, blockPool block.Pool, blockQueue chan block.SharedColumns) (*Worker, error) {
	w := Worker{
		log:                         log.With("component", "metrics_worker", "data_type", dataType),
		runID:                       runID,
		dataType:                    dataType,
		targetBytesPerSecond:        targetBytesPerSecond,
		targetGenerateRowsPerSecond: targetGenerateRowsPerSecond,
		configAttributes:            runAttr,

		blockPool:  blockPool,
		blockQueue: blockQueue,

		metricsQueue: make(chan Entry, 100_000),
		metrics:      make(map[metricKey]uint64),
		pointMetrics: make([]Entry, 0, 100_000),
	}

	if cfg.OTLP.Enabled {
		emitter, err := newOTLPEmitter(w.log, cfg.OTLP, runID, configName, runAttr, time.Now())
		if err != nil {
			return nil, err
		}
		w.otlp = emitter
		w.log.Info("otlp export enabled", "url", cfg.OTLP.URL, "interval", emitter.interval)
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

	// The OTLP exporter runs on its own interval; a nil channel never fires.
	var otlpTickerC <-chan time.Time
	if w.otlp != nil {
		otlpTicker := time.NewTicker(w.otlp.interval)
		defer otlpTicker.Stop()
		otlpTickerC = otlpTicker.C
		defer w.otlp.close()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m := <-w.metricsQueue:
			w.applyMetricEntry(m)
		case <-otlpTickerC:
			w.collectInternalMetrics()
			w.otlp.export(ctx, w.snapshotCumulative())
		case <-ticker.C:
			w.collectInternalMetrics()

			if w.insertSQL != "" {
				snapshot, pointSnapshot := w.snapshotAndReset()
				if err := w.pushMetrics(ctx, snapshot, pointSnapshot); err != nil {
					w.log.Error("failed to push metrics", "err", err)
				}
			} else {
				w.resetMetrics()
			}
		}
	}
}

func (w *Worker) applyMetricEntry(m Entry) {
	w.mu.Lock()
	defer w.mu.Unlock()

	key := metricKey{Name: m.Name, AttrKey: m.AttrKey, AttrValue: m.AttrValue}

	if _, ok := w.metrics[key]; !ok && m.Mode != EntryModePoint {
		w.metrics[key] = 0
	}

	switch m.Mode {
	case EntryModeIncrement:
		w.metrics[key] += m.Value
	case EntryModeDecrement:
		if m.Value >= w.metrics[key] {
			w.metrics[key] = 0
		} else {
			w.metrics[key] -= m.Value
		}
	case EntryModeSet:
		w.metrics[key] = m.Value
	case EntryModePoint:
		w.pointMetrics = append(w.pointMetrics, m)
		if w.otlp != nil {
			w.otlp.addSample(m)
		}
	}
}

// collectInternalMetrics gathers block pool and runtime stats directly into
// the map, bypassing the queue. Expensive calls (ReadMemStats, Getrusage) are
// done outside the lock; only the map writes are held under it.
func (w *Worker) collectInternalMetrics() {
	blockPoolCount, blockPoolCapacity := w.blockPool.Stats()
	retired := w.blockPool.TotalRetired()
	queueLen := len(w.blockQueue)
	numGoroutines := runtime.NumGoroutine()
	numCPU := runtime.NumCPU()

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms) // stop-the-world; must not hold w.mu

	var cpuUser, cpuSys uint64
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err == nil {
		cpuUser = uint64(ru.Utime.Nano())
		cpuSys = uint64(ru.Stime.Nano())
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.metrics[metricKey{Name: BlockPoolCount}] = uint64(blockPoolCount)
	w.metrics[metricKey{Name: BlockPoolCapacity}] = uint64(blockPoolCapacity)
	w.metrics[metricKey{Name: BlockQueueLength}] = uint64(queueLen)
	w.metrics[metricKey{Name: BlocksRetiredTotal}] = uint64(retired)
	w.metrics[metricKey{Name: TargetBytesPerSecond}] = w.targetBytesPerSecond
	w.metrics[metricKey{Name: TargetGenerateRowsPerSecond}] = w.targetGenerateRowsPerSecond
	w.metrics[metricKey{Name: ProgramHeapAllocBytes}] = ms.HeapAlloc
	w.metrics[metricKey{Name: ProgramSysBytes}] = ms.Sys
	w.metrics[metricKey{Name: ProgramNumGoroutines}] = uint64(numGoroutines)
	w.metrics[metricKey{Name: ProgramGCRunsTotal}] = uint64(ms.NumGC)
	w.metrics[metricKey{Name: ProgramGCPauseNsTotal}] = ms.PauseTotalNs
	w.metrics[metricKey{Name: ProgramNextGCBytes}] = ms.NextGC
	w.metrics[metricKey{Name: ProgramNumCPU}] = uint64(numCPU)
	if cpuUser > 0 {
		w.metrics[metricKey{Name: ProgramCPUUserNsTotal}] = cpuUser
		w.metrics[metricKey{Name: ProgramCPUSysNsTotal}] = cpuSys
	}
}

// snapshotAndReset copies the current metrics state and clears point metrics.
// Returns owned copies safe to use outside the lock.
func (w *Worker) snapshotAndReset() (map[metricKey]uint64, []Entry) {
	w.mu.Lock()
	defer w.mu.Unlock()

	snapshot := make(map[metricKey]uint64, len(w.metrics))
	maps.Copy(snapshot, w.metrics)

	var pointSnapshot []Entry
	if len(w.pointMetrics) > 0 {
		pointSnapshot = make([]Entry, len(w.pointMetrics))
		copy(pointSnapshot, w.pointMetrics)
		w.pointMetrics = w.pointMetrics[:0]
	}

	return snapshot, pointSnapshot
}

// snapshotCumulative copies the current cumulative/gauge metric state without
// clearing anything. Sample points are buffered separately by the OTLP emitter.
func (w *Worker) snapshotCumulative() map[metricKey]uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	snapshot := make(map[metricKey]uint64, len(w.metrics))
	maps.Copy(snapshot, w.metrics)
	return snapshot
}

func (w *Worker) resetMetrics() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pointMetrics = w.pointMetrics[:0]
}

func (w *Worker) pushMetrics(ctx context.Context, snapshot map[metricKey]uint64, pointSnapshot []Entry) error {
	batch, err := w.conn.PrepareBatch(ctx, w.insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare metrics batch: %w", err)
	}
	defer func(batch driver.Batch) {
		if batchErr := batch.Close(); batchErr != nil {
			w.log.Error("failed to close batch", "err", batchErr)
		}
	}(batch)

	now := time.Now()
	for key, value := range snapshot {
		var extraAttr map[string]string
		if key.AttrKey != "" {
			extraAttr = map[string]string{key.AttrKey: key.AttrValue}
		}
		if err = batch.Append(w.runID, string(key.Name), now, value, w.mergeAttributes(extraAttr)); err != nil {
			return fmt.Errorf("failed to append metric (%s/%d) to batch: %w", key.Name, value, err)
		}
	}

	for _, m := range pointSnapshot {
		if err = batch.Append(w.runID, string(m.Name), m.Timestamp, m.Value, w.mergeAttributes(m.Attributes)); err != nil {
			return fmt.Errorf("failed to append point metric (%s/%d) to batch: %w", m.Name, m.Value, err)
		}
	}

	if err = batch.Send(); err != nil {
		return fmt.Errorf("failed to send metrics: %w", err)
	}

	w.log.Debug("pushed metrics", "count", batch.Rows())
	return nil
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

func (w *Worker) IncrementMetricWithAttr(name Name, delta uint64, attrKey, attrValue string) {
	select {
	case w.metricsQueue <- Entry{
		Mode:      EntryModeIncrement,
		Name:      name,
		AttrKey:   attrKey,
		AttrValue: attrValue,
		Value:     delta,
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

	return w.metrics[metricKey{Name: name}]
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
