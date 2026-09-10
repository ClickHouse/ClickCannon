package metricgen

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"clickcannon/internal/metrics"

	"golang.org/x/time/rate"
)

// Scheduler resolves the run plan and manages the generator worker goroutines.
type Scheduler struct {
	log       *slog.Logger
	workerLog *slog.Logger
	cfg       Config
	seed      string
	metrics   metrics.Store
}

func NewScheduler(log *slog.Logger, cfg *Config, seed string, m metrics.Store) *Scheduler {
	return &Scheduler{
		log:       log.With("component", "metricgen_scheduler"),
		workerLog: log,
		cfg:       cfg.withDefaults(),
		seed:      seed,
		metrics:   m,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	p, err := newPlan(s.cfg, s.seed, time.Now())
	if err != nil {
		return fmt.Errorf("failed to resolve metric plan: %w", err)
	}

	logAttrs := []any{
		"url", s.cfg.URL,
		"threads", s.cfg.Threads,
		"metric_count", s.cfg.MetricCount,
		"total_series", p.totalSeries,
		"resources", s.cfg.Resources,
		"services", s.cfg.Services,
		"interval", s.cfg.Interval,
		"start_time", time.UnixMilli(p.startMs).UTC().Format(time.RFC3339),
		"points_per_second", s.cfg.PointsPerSecond,
		"points_per_request", s.cfg.PointsPerRequest,
	}
	if s.cfg.Sweeps > 0 {
		endMs := p.startMs + int64(s.cfg.Sweeps)*p.intervalMs
		logAttrs = append(logAttrs,
			"sweeps", s.cfg.Sweeps,
			"end_time", time.UnixMilli(endMs).UTC().Format(time.RFC3339),
			"total_points", p.totalSeries*uint64(s.cfg.Sweeps))
	}
	s.log.Info("started", logAttrs...)
	for _, t := range []string{"gauge", "sum", "histogram", "exponential_histogram", "summary"} {
		if p.typeCounts[t] > 0 {
			s.log.Info("metric type", "type", t, "metrics", p.typeCounts[t], "series", p.typeSeries[t])
		}
	}
	if p.totalSeries > warnTotalSeries {
		s.log.Warn("very large series count configured; make sure the exporter's series_cache_size and ClickHouse can handle it", "total_series", p.totalSeries)
	}

	var limiter *rate.Limiter
	chunk := 256
	if s.cfg.PointsPerSecond > 0 {
		// Chunked waits: target ~10 limiter waits per worker per second so low
		// rates flush promptly and high rates amortize limiter overhead.
		chunk = max(1, min(256, s.cfg.PointsPerSecond/(s.cfg.Threads*10)))
		burst := max(chunk*s.cfg.Threads*2, s.cfg.PointsPerSecond/10)
		limiter = rate.NewLimiter(rate.Limit(s.cfg.PointsPerSecond), burst)
		s.log.Info("rate limiting enabled", "points_per_second", s.cfg.PointsPerSecond)
	} else {
		s.log.Info("rate limiting disabled (unlimited)")
	}
	s.metrics.SetMetric(metrics.TargetMetricGenPointsPerSecond, uint64(s.cfg.PointsPerSecond))

	var wg sync.WaitGroup
	errs := make([]error, s.cfg.Threads)
	for i := range s.cfg.Threads {
		w := newWorker(i, s.workerLog, &s.cfg, p, limiter, chunk, s.metrics)
		wg.Go(func() {
			s.metrics.IncrementMetric(metrics.ActiveMetricGenWorkers, 1)
			defer s.metrics.DecrementMetric(metrics.ActiveMetricGenWorkers, 1)

			if wErr := w.Run(ctx); wErr != nil && !errors.Is(wErr, context.Canceled) {
				s.log.Error("worker error", "worker_id", w.id, "err", wErr)
				errs[w.id] = wErr
			}
		})
	}

	wg.Wait()
	s.log.Info("stopped")
	return errors.Join(errs...)
}
