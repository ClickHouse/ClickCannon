package otel

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"clickcannon/internal/block"
	"clickcannon/internal/metrics"
)

// Scheduler manages the OTLP exporter worker goroutines. Each worker consumes
// blocks from the shared queue, converts them to OTLP, and exports them.
type Scheduler struct {
	log       *slog.Logger
	workerLog *slog.Logger
	cfg       Config
	dataType  string
	blockPool block.Pool
	queue     <-chan block.SharedColumns
	metrics   metrics.Store
}

func NewScheduler(
	log *slog.Logger,
	cfg *Config,
	dataType string,
	blockPool block.Pool,
	queue <-chan block.SharedColumns,
	m metrics.Store,
) *Scheduler {
	return &Scheduler{
		log:       log.With("component", "otel_scheduler"),
		workerLog: log,
		cfg:       cfg.withDefaults(),
		dataType:  dataType,
		blockPool: blockPool,
		queue:     queue,
		metrics:   m,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	s.log.Info("started", "threads", s.cfg.Threads, "url", s.cfg.URL, "batch_size", s.cfg.BatchSize)

	var wg sync.WaitGroup
	for i := range s.cfg.Threads {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.metrics.IncrementMetric(metrics.ActiveOTelExporters, 1)
			defer s.metrics.DecrementMetric(metrics.ActiveOTelExporters, 1)
			s.runWorker(ctx, id)
		}(i)
	}

	wg.Wait()
	s.log.Info("stopped")
	return nil
}

func (s *Scheduler) runWorker(ctx context.Context, id int) {
	const (
		baseBackoff = 500 * time.Millisecond
		maxBackoff  = 30 * time.Second
	)
	backoff := baseBackoff

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		w := newWorker(id, s.workerLog, &s.cfg, s.dataType, s.blockPool, s.queue, s.metrics)
		err := w.Run(ctx)

		switch {
		case err == nil:
			return
		case errors.Is(err, context.Canceled):
			return
		default:
			s.log.Warn("otel worker failed, restarting", "worker_id", id, "err", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
		}
	}
}
