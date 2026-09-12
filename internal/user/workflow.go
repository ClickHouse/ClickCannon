package user

import (
	"clickcannon/internal/metrics"
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"time"
)

type Workflow interface {
	NextQuery(ctx context.Context) (*ExecutableQuery, error)
	ThinkTime() time.Duration
	Name() string
	// ResetLoop rewinds to the start of the current query loop so preflights
	// and time ranges are re-sampled. Returns true if the reset was performed.
	ResetLoop() bool
}

func newWorkflow(log *slog.Logger, userCfg *Config, name string, cfg WorkflowBaseConfig, queryRunner QueryRunner, rng *rand.Rand, datasetStart, datasetEnd time.Time, m metrics.Store) (Workflow, error) {
	switch c := cfg.Config.(type) {
	case *QueriesWorkflowConfig:
		return NewQueriesWorkflow(log, userCfg, name, c, queryRunner, rng, datasetStart, datasetEnd, m), nil
	case *HARWorkflowConfig:
		//return NewHARWorkflow(), nil
		return nil, fmt.Errorf("unimplemented workflow type %q", cfg.Type)
	default:
		return nil, fmt.Errorf("unknown workflow type %q", cfg.Type)
	}
}

type QueriesWorkflow struct {
	log          *slog.Logger
	name         string
	userCfg      *Config
	cfg          *QueriesWorkflowConfig
	queryRunner  QueryRunner
	metrics      metrics.Store
	rng          *rand.Rand
	index        int
	datasetStart time.Time
	datasetEnd   time.Time

	sampledTimeRange    *ResolvedTimeRange
	workflowBinds       map[string]string
	workflowBindsCached bool
}

func NewQueriesWorkflow(log *slog.Logger, userCfg *Config, name string, cfg *QueriesWorkflowConfig, queryRunner QueryRunner, rng *rand.Rand, datasetStart, datasetEnd time.Time, m metrics.Store) *QueriesWorkflow {
	return &QueriesWorkflow{
		log:          log,
		name:         name,
		userCfg:      userCfg,
		cfg:          cfg,
		queryRunner:  queryRunner,
		metrics:      m,
		rng:          rng,
		datasetStart: datasetStart,
		datasetEnd:   datasetEnd,
	}
}

func (b *QueriesWorkflow) Name() string {
	return b.name
}

func (b *QueriesWorkflow) anchor() time.Time {
	switch b.cfg.TimeAnchor {
	case TimeAnchorNow:
		return time.Now()
	case TimeAnchorDatasetEnd:
		return b.datasetEnd
	case TimeAnchorDatasetRandom:
		window := b.datasetEnd.Unix() - b.datasetStart.Unix()
		offset := b.rng.Int64N(window)
		return time.Unix(b.datasetStart.Unix()+offset, 0)
	default:
		return time.Now()
	}
}

func (b *QueriesWorkflow) NextQuery(ctx context.Context) (*ExecutableQuery, error) {
	queryIndex, q := b.nextQueryConfig()

	params := QueryParams{
		Database:  b.userCfg.Database,
		Table:     b.userCfg.Table,
		Preflight: make(map[string]string),
	}

	resolved := b.resolveTimeRange(q)
	var resolvedDuration time.Duration
	if resolved != nil {
		params.TimeStart = resolved.Start
		params.TimeEnd = resolved.End
		resolvedDuration = resolved.End.Sub(resolved.Start)
	}

	workloadBinds, err := b.getWorkloadBinds(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("workflow preflight_queries for query (index=%d, name=%q): %w", queryIndex, q.Name, err)
	}
	for k, v := range workloadBinds {
		params.Preflight[k] = v
	}

	for k, v := range q.Vars {
		params.Preflight[k] = v
	}

	if len(q.PreflightQueries) > 0 {
		binds, err := b.execPreflights(ctx, q.Name, params, q.PreflightQueries)
		if err != nil {
			return nil, fmt.Errorf("preflight_queries for query (index=%d, name=%q): %w", queryIndex, q.Name, err)
		}
		params.Preflight = binds
	}

	return &ExecutableQuery{
		QueryIndex: queryIndex,
		Name:       q.Name,
		SQL:        TryAppendFormatNull(q.SQL),
		Settings:   mergeSettings(b.cfg.DefaultSettings, q.Settings),
		Params:     params.Params(),
		TimeRange:  resolvedDuration,
		Perf:       q.Perf,
	}, nil
}

func (b *QueriesWorkflow) ThinkTime() time.Duration {
	tt := b.cfg.ThinkTime
	spread := tt.Max - tt.Min
	if spread <= 0 {
		return tt.Min
	}

	return tt.Min + time.Duration(b.rng.Int64N(int64(spread)))
}

func (b *QueriesWorkflow) resolveTimeRange(q *QueryConfig) *ResolvedTimeRange {
	// Query-level override
	if q.TimeRange != nil {
		if q.TimeRange.Type == TimeRangeNone {
			return nil
		}

		resolved, ok := q.TimeRange.Resolve(b.anchor(), b.rng)
		if !ok {
			return nil
		}

		return &resolved
	}

	// Default time range
	if b.cfg.DefaultTimeRange == nil || b.cfg.DefaultTimeRange.Type == TimeRangeNone {
		return nil
	}

	if b.cfg.TimeRangeCadence == TimeRangeCadencePerQuery || b.sampledTimeRange == nil {
		resolved, ok := b.cfg.DefaultTimeRange.Resolve(b.anchor(), b.rng)
		if !ok {
			return nil
		}

		trDuration := resolved.End.Sub(resolved.Start)
		b.log.Debug("resolved time range", "time_range", trDuration.String(), "time_range_seconds", int(trDuration.Seconds()))
		b.sampledTimeRange = &resolved
	}

	return b.sampledTimeRange
}

// getWorkloadBinds returns the workload-level vars and preflight query results, using the
// configured preflight_cadence to decide whether to run fresh or return a cached result.
func (b *QueriesWorkflow) getWorkloadBinds(ctx context.Context, params QueryParams) (map[string]string, error) {
	if b.workflowBindsCached && b.cfg.PreflightCadence != WorkflowPreflightCadencePerQuery {
		return b.workflowBinds, nil
	}

	binds := make(map[string]string, len(b.cfg.Vars)+len(b.cfg.PreflightQueries))
	for k, v := range b.cfg.Vars {
		binds[k] = v
	}

	if len(b.cfg.PreflightQueries) > 0 {
		params.Preflight = binds
		var err error
		binds, err = b.execPreflights(ctx, "workflow", params, b.cfg.PreflightQueries)
		if err != nil {
			return nil, err
		}
	}

	if b.cfg.PreflightCadence != WorkflowPreflightCadencePerQuery {
		b.workflowBinds = binds
		b.workflowBindsCached = true
	}

	return binds, nil
}

// execPreflights runs preflight queries in sequence, each receiving all binds accumulated so far.
// The scope names the owner for perf recording: the query name for query-level preflights or
// "workflow" for workflow-level ones. Returns the full merged bind map (input binds + new results).
func (b *QueriesWorkflow) execPreflights(ctx context.Context, scope string, params QueryParams, preflights []PreflightQueryConfig) (map[string]string, error) {
	binds := make(map[string]string, len(params.Preflight)+len(preflights))
	for k, v := range params.Preflight {
		binds[k] = v
	}

	for i, pf := range preflights {
		params.Preflight = binds
		start := time.Now()
		result, err := b.queryRunner.ExecPreflight(ctx, pf.Binds, pf.SQL, pf.Settings, params.Params())
		if err != nil {
			b.metrics.IncrementMetric(metrics.PreflightsFailedTotal, 1)
			return nil, fmt.Errorf("preflight_queries[%d]: %w", i, err)
		}
		b.metrics.IncrementMetric(metrics.PreflightsOkTotal, 1)
		b.metrics.AddMetricPointWithAttributes(metrics.PreflightLatencyMicros, uint64(time.Since(start).Microseconds()), map[string]string{
			"workflow_name":   b.name,
			"preflight_scope": scope,
			"preflight_index": strconv.Itoa(i),
		})
		for k, v := range result {
			binds[k] = v
		}
	}

	return binds, nil
}

func mergeSettings(defaults, overrides map[string]string) map[string]string {
	if len(defaults) == 0 {
		return overrides
	}

	merged := make(map[string]string, len(defaults)+len(overrides))
	for k, v := range defaults {
		merged[k] = v
	}

	for k, v := range overrides {
		merged[k] = v
	}

	return merged
}

// ResetLoop rewinds the query index to the start of the current loop so that
// nextQueryConfig will see loopWrapped=true and clear cached time ranges and
// preflight binds. Only resets when preflight_cadence is per_loop — for other
// cadences a preflight failure is not recoverable by re-looping.
func (b *QueriesWorkflow) ResetLoop() bool {
	if b.cfg.PreflightCadence != WorkflowPreflightCadencePerLoop {
		return false
	}

	n := len(b.cfg.Queries)
	b.index = (b.index / n) * n

	return true
}

func (b *QueriesWorkflow) nextQueryConfig() (int, *QueryConfig) {
	queries := b.cfg.Queries

	if b.cfg.Random {
		i := b.rng.IntN(len(queries))
		return i, &queries[i]
	}

	queryIndex := b.index % len(queries)
	loopWrapped := queryIndex == 0

	if loopWrapped && b.cfg.TimeRangeCadence == TimeRangeCadencePerLoop {
		b.sampledTimeRange = nil
	}
	if loopWrapped && b.cfg.PreflightCadence == WorkflowPreflightCadencePerLoop {
		b.workflowBindsCached = false
	}

	q := &queries[queryIndex]
	b.index++

	return queryIndex, q
}
