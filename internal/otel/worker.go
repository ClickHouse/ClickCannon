package otel

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"clickcannon/internal/block"
	"clickcannon/internal/metrics"

	"google.golang.org/protobuf/proto"
)

// batch accumulates converted rows and flushes them as a single OTLP export.
// Two implementations exist (logs and traces); the worker loop is shared.
type batch interface {
	// addBlock reads every row of blk into the batch. The block's data has
	// already been copied into OTLP messages when this returns, so the caller
	// may release blk immediately.
	addBlock(blk block.SharedColumns)
	// len returns the number of rows accumulated so far.
	len() int
	// flush exports the accumulated rows and returns the wire size and row
	// count sent. The batch is NOT reset on error so the caller can decide.
	flush(ctx context.Context, c exporter) (bytesSent, rows int, err error)
	// reset clears the accumulated rows.
	reset()
}

type logsBatch struct {
	b   *logsBuilder
	row block.LogRow
}

func (lb *logsBatch) addBlock(blk block.SharedColumns) {
	r, ok := blk.(block.LogsReader)
	if !ok {
		return
	}
	n := r.Rows()
	for i := 0; i < n; i++ {
		r.ReadLogRow(i, &lb.row)
		lb.b.add(&lb.row)
	}
}

func (lb *logsBatch) len() int { return lb.b.len() }

func (lb *logsBatch) flush(ctx context.Context, c exporter) (int, int, error) {
	req := lb.b.build()
	size := proto.Size(req)
	rows := lb.b.len()
	if err := c.exportLogs(ctx, req); err != nil {
		return 0, 0, err
	}
	return size, rows, nil
}

func (lb *logsBatch) reset() { lb.b.reset() }

type tracesBatch struct {
	b   *tracesBuilder
	row block.TraceRow
}

func (tb *tracesBatch) addBlock(blk block.SharedColumns) {
	r, ok := blk.(block.TracesReader)
	if !ok {
		return
	}
	n := r.Rows()
	for i := 0; i < n; i++ {
		r.ReadTraceRow(i, &tb.row)
		tb.b.add(&tb.row)
	}
}

func (tb *tracesBatch) len() int { return tb.b.len() }

func (tb *tracesBatch) flush(ctx context.Context, c exporter) (int, int, error) {
	req := tb.b.build()
	size := proto.Size(req)
	rows := tb.b.len()
	if err := c.exportTraces(ctx, req); err != nil {
		return 0, 0, err
	}
	return size, rows, nil
}

func (tb *tracesBatch) reset() { tb.b.reset() }

type worker struct {
	id    int
	idStr string
	log   *slog.Logger

	cfg      *Config
	dataType string

	blockPool block.Pool
	queue     <-chan block.SharedColumns
	metrics   metrics.Store
}

func newWorker(id int, log *slog.Logger, cfg *Config, dataType string, blockPool block.Pool, queue <-chan block.SharedColumns, m metrics.Store) *worker {
	return &worker{
		id:        id,
		idStr:     strconv.Itoa(id),
		log:       log.With("component", "otel_worker", "id", id),
		cfg:       cfg,
		dataType:  dataType,
		blockPool: blockPool,
		queue:     queue,
		metrics:   m,
	}
}

func (w *worker) newBatch() batch {
	if w.dataType == "traces" {
		return &tracesBatch{b: newTracesBuilder()}
	}
	return &logsBatch{b: newLogsBuilder()}
}

// drainRelease returns any blocks still buffered in the queue to the pool. It is
// called on context cancellation so shutdown does not strand pooled blocks that
// the source produced but this worker never consumed.
func (w *worker) drainRelease() {
	for {
		select {
		case blk, ok := <-w.queue:
			if !ok {
				return
			}
			w.blockPool.Release(blk)
		default:
			return
		}
	}
}

func (w *worker) Run(ctx context.Context) error {
	w.log.Info("started")

	c, err := dial(w.cfg)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := c.close(); closeErr != nil {
			w.log.Debug("failed to close exporter client", "err", closeErr)
		}
	}()

	b := w.newBatch()

	ticker := time.NewTicker(w.cfg.FlushInterval)
	defer ticker.Stop()

	const (
		baseFlushBackoff = 250 * time.Millisecond
		maxFlushBackoff  = 5 * time.Second
		maxFlushAttempts = 5
	)

	// flush exports the current batch (if any) and records metrics. Transient
	// export failures are retried in place with backoff so a briefly-unavailable
	// endpoint does not discard already-converted rows or tear down the gRPC
	// connection. The batch is dropped (bounded data loss) only after exhausting
	// retries or on shutdown; the worker keeps running either way.
	flush := func(fctx context.Context) {
		if b.len() == 0 {
			return
		}
		backoff := baseFlushBackoff
		for attempt := 1; ; attempt++ {
			size, rows, ferr := b.flush(fctx, c)
			if ferr == nil {
				b.reset()
				w.metrics.IncrementMetric(metrics.OTelRowsTotal, uint64(rows))
				w.metrics.IncrementMetric(metrics.OTelBatchesTotal, 1)
				w.metrics.IncrementMetric(metrics.OTelBytesTotal, uint64(size))
				w.metrics.IncrementMetricWithAttr(metrics.OTelRowsWorkerTotal, uint64(rows), "worker_id", w.idStr)
				w.metrics.IncrementMetricWithAttr(metrics.OTelBatchesWorkerTotal, 1, "worker_id", w.idStr)
				return
			}

			w.metrics.IncrementMetric(metrics.OTelExportsFailedTotal, 1)
			if fctx.Err() != nil || attempt >= maxFlushAttempts {
				w.log.Warn("export failed, dropping batch", "attempts", attempt, "rows", b.len(), "err", ferr)
				b.reset()
				return
			}
			w.log.Debug("export failed, retrying", "attempt", attempt, "backoff", backoff, "err", ferr)
			select {
			case <-fctx.Done():
				w.log.Warn("export failed, dropping batch on shutdown", "rows", b.len(), "err", ferr)
				b.reset()
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxFlushBackoff)
		}
	}

	for {
		select {
		case <-ctx.Done():
			w.drainRelease()
			w.log.Info("stopped")
			return ctx.Err()

		case blk, ok := <-w.queue:
			if !ok {
				// Source finished and closed the queue: flush the final batch.
				flush(ctx)
				w.log.Info("stopped")
				return nil
			}

			b.addBlock(blk)
			// The block's rows are now copied into OTLP messages; return it to
			// the pool before any network I/O so it can be reused promptly.
			w.blockPool.Release(blk)

			if b.len() >= w.cfg.BatchSize {
				flush(ctx)
			}

		case <-ticker.C:
			flush(ctx)
		}
	}
}
