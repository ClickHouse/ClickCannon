package metricgen

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"clickcannon/internal/metrics"

	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"
)

// worker generates and exports its shard of the series space. Series are
// striped across workers by index (j % threads == id), so every worker touches
// every metric and the shard assignment is deterministic. Each worker advances
// its own sweep counter: workers may drift a few sweeps apart, like real
// collectors scraping independent targets.
type worker struct {
	id    int
	idStr string
	log   *slog.Logger

	cfg     *Config
	plan    *plan
	limiter *rate.Limiter // nil = unlimited
	chunk   int           // points requested from the limiter per wait

	metrics metrics.Store
}

func newWorker(id int, log *slog.Logger, cfg *Config, p *plan, limiter *rate.Limiter, chunk int, m metrics.Store) *worker {
	return &worker{
		id:      id,
		idStr:   strconv.Itoa(id),
		log:     log.With("component", "metricgen_worker", "id", id),
		cfg:     cfg,
		plan:    p,
		limiter: limiter,
		chunk:   chunk,
		metrics: m,
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
			w.log.Debug("failed to close grpc client", "err", closeErr)
		}
	}()

	b := newBuilder(w.plan)
	lastFlush := time.Now()
	lastLog := time.Now()

	const (
		baseFlushBackoff = 250 * time.Millisecond
		maxFlushBackoff  = 5 * time.Second
		maxFlushAttempts = 5
	)

	var totalPoints, totalRequests uint64

	// flush exports the accumulated request, retrying transient failures in
	// place with backoff. The request is dropped (bounded data loss) only
	// after exhausting retries or on shutdown; the worker keeps generating
	// either way.
	flush := func(fctx context.Context) {
		if b.len() == 0 {
			return
		}
		req := b.build()
		points := b.len()
		// Marshal exactly once: the passthrough codec (rawRequestCodec) ships these bytes as-is, and len(data) is the exact
		// wire size for the bytes metric.
		data, merr := proto.Marshal(req)
		if merr != nil {
			w.metrics.IncrementMetric(metrics.MetricGenExportsFailedTotal, 1)
			w.log.Error("marshal failed, dropping request", "points", points, "err", merr)
			b.reset()
			lastFlush = time.Now()
			return
		}
		size := len(data)

		backoff := baseFlushBackoff
		for attempt := 1; ; attempt++ {
			flushStart := time.Now()
			rejected, rejectMsg, ferr := c.export(fctx, data)
			if isMessageTooLarge(ferr) {
				// Retrying can never succeed: the marshaled request exceeds the
				// receiver's gRPC message limit.
				w.metrics.IncrementMetric(metrics.MetricGenExportsFailedTotal, 1)
				w.log.Error("export rejected: request exceeds the receiver's gRPC message limit; lower points_per_request or raise max_recv_msg_size_mib on the OTLP receiver",
					"points", points, "bytes", size, "err", ferr)
				b.reset()
				lastFlush = time.Now()
				return
			}
			if ferr == nil {
				// Partial success is still a success: never retried, the
				// request counts as delivered, rejections tracked separately.
				if rejected > 0 {
					w.metrics.IncrementMetric(metrics.MetricGenPointsRejectedTotal, uint64(rejected))
					w.log.Warn("endpoint rejected some data points (partial success, not retried)",
						"rejected", rejected, "points", points, "message", rejectMsg)
				}
				b.reset()
				lastFlush = time.Now()
				totalPoints += uint64(points)
				totalRequests++
				w.metrics.IncrementMetric(metrics.MetricGenPointsTotal, uint64(points))
				w.metrics.IncrementMetric(metrics.MetricGenRequestsTotal, 1)
				w.metrics.IncrementMetric(metrics.MetricGenBytesTotal, uint64(size))
				w.metrics.IncrementMetricWithAttr(metrics.MetricGenPointsWorkerTotal, uint64(points), "worker_id", w.idStr)
				w.metrics.AddMetricPointWithAttributes(metrics.MetricGenExportLatencyMicros, uint64(time.Since(flushStart).Microseconds()), map[string]string{
					"worker_id": w.idStr,
					"points":    strconv.Itoa(points),
				})
				return
			}

			w.metrics.IncrementMetric(metrics.MetricGenExportsFailedTotal, 1)
			if fctx.Err() != nil || attempt >= maxFlushAttempts {
				w.log.Warn("export failed, dropping request", "attempts", attempt, "points", points, "err", ferr)
				b.reset()
				lastFlush = time.Now()
				return
			}
			w.log.Debug("export failed, retrying", "attempt", attempt, "backoff", backoff, "err", ferr)
			select {
			case <-fctx.Done():
				w.log.Warn("export failed, dropping request on shutdown", "points", points, "err", ferr)
				b.reset()
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxFlushBackoff)
		}
	}

	// checkpoint runs between chunks of points: honors cancellation, waits for
	// rate-limiter permission, and flushes a stale partial request so low
	// rates still export promptly.
	allowance := 0
	checkpoint := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if b.len() > 0 && time.Since(lastFlush) > w.cfg.FlushInterval {
			flush(ctx)
		}
		if w.limiter != nil {
			if err := w.limiter.WaitN(ctx, w.chunk); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("rate limiter wait: %w", err)
			}
		}
		allowance = w.chunk
		return nil
	}

	sweeps := uint64(w.cfg.Sweeps)
	for k := uint64(0); sweeps == 0 || k < sweeps; k++ {
		for i := range w.plan.metrics {
			def := w.plan.metricAt(i, k)
			for j := w.id; j < def.cardinality; j += w.cfg.Threads {
				if allowance == 0 {
					if err := checkpoint(); err != nil {
						w.log.Info("stopped", "sweeps_completed", k, "points_sent", totalPoints)
						return err
					}
				}
				allowance--

				b.addPoint(def, j, k)
				if b.len() >= w.cfg.PointsPerRequest {
					flush(ctx)
				}
			}
		}

		w.metrics.IncrementMetricWithAttr(metrics.MetricGenSweepsWorkerTotal, 1, "worker_id", w.idStr)
		if time.Since(lastLog) > 30*time.Second {
			lastLog = time.Now()
			w.log.Info("progress",
				"sweep", k+1,
				"virtual_time", time.UnixMilli(w.plan.sweepTimeMs(int64(k), 0)).UTC().Format(time.RFC3339),
				"points_sent", totalPoints,
				"requests_sent", totalRequests)
		}
	}

	flush(ctx)
	w.log.Info("stopped", "sweeps_completed", sweeps, "points_sent", totalPoints)
	return nil
}
