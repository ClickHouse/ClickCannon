package generate

import (
	"context"
	"fmt"
	"log/slog"

	"clickcannon/internal/block"
	"clickcannon/internal/metrics"

	"golang.org/x/time/rate"
)

// worker generates synthetic data blocks and pushes them to the insert queue.
type worker struct {
	id           int
	idStr        string
	log          *slog.Logger
	rng          *Rng
	blockPool    block.Pool
	insertQueue  chan<- block.SharedColumns
	metrics      metrics.Store
	limiter      *rate.Limiter // nil if unlimited
	rowsPerBlock int
	dataType     string
	passthrough  bool // when true, blocks are released immediately (benchmarks generation throughput)

	logsFiller     *LogsFiller
	tracesFiller   *TracesFiller
	profilesFiller *ProfilesFiller
	
	// Custom fields support
	dynamicFiller *DynamicFiller
	customConfig  *CustomFieldsConfig
}

func (w *worker) ID() int { return w.id }

func (w *worker) Run(ctx context.Context) error {
	w.log.Info("started", "worker_id", w.id)
	defer w.log.Info("stopped", "worker_id", w.id)

	for {
		// Check context at the top of every iteration.
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Rate limit: wait for permission to generate a block
		if w.limiter != nil {
			if err := w.limiter.WaitN(ctx, w.rowsPerBlock); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("rate limiter wait: %w", err)
			}
		}

		cols := w.blockPool.Acquire()
		if cols == nil {
			return fmt.Errorf("block pool returned nil")
		}
		cols.Reset()

		var rowsFilled int
		
		// Check if using custom fields configuration
		if w.dynamicFiller != nil && w.customConfig != nil {
			dynCols, ok := cols.(*DynamicColumns)
			if !ok {
				w.blockPool.Release(cols)
				return fmt.Errorf("expected *DynamicColumns for custom config, got %T", cols)
			}
			rowsFilled = w.dynamicFiller.Fill(ctx, w.rng, dynCols, w.rowsPerBlock)
		} else {
			// Use traditional profile-based fillers
			switch w.dataType {
			case "logs":
				logCols, ok := cols.(*GenLogsColumns)
				if !ok {
					w.blockPool.Release(cols)
					return fmt.Errorf("expected *GenLogsColumns, got %T", cols)
				}
				rowsFilled = w.logsFiller.Fill(ctx, w.rng, logCols, w.rowsPerBlock)
			case "traces":
				traceCols, ok := cols.(*GenTracesColumns)
				if !ok {
					w.blockPool.Release(cols)
					return fmt.Errorf("expected *GenTracesColumns, got %T", cols)
				}
				rowsFilled = w.tracesFiller.Fill(ctx, w.rng, traceCols, w.rowsPerBlock)
			case "profiles":
				profileCols, ok := cols.(*GenProfilesColumns)
				if !ok {
					w.blockPool.Release(cols)
					return fmt.Errorf("expected *GenProfilesColumns, got %T", cols)
				}
				rowsFilled = w.profilesFiller.Fill(ctx, w.rng, profileCols, w.rowsPerBlock)
			}
		}

		// Partial fill from cancellation — discard the block
		if rowsFilled < w.rowsPerBlock {
			w.blockPool.Release(cols)
			return nil
		}

		w.metrics.IncrementMetric(metrics.GenerateRowsTotal, uint64(w.rowsPerBlock))
		w.metrics.IncrementMetric(metrics.GenerateBlocksTotal, 1)
		w.metrics.IncrementMetricWithAttr(metrics.GenerateRowsWorkerTotal, uint64(w.rowsPerBlock), "worker_id", w.idStr)

		if w.passthrough {
			w.blockPool.Release(cols)
			continue
		}

		select {
		case <-ctx.Done():
			w.blockPool.Release(cols)
			return nil
		case w.insertQueue <- cols:
		}
	}
}
