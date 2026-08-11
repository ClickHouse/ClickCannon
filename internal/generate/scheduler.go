package generate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"clickcannon/internal/block"
	"clickcannon/internal/metrics"

	"golang.org/x/time/rate"
)

const DefaultProfile = "otel_demo"

// Scheduler manages generate worker goroutines and rate limiting.
type Scheduler struct {
	log         *slog.Logger
	cfg         *Config
	seed        string
	dataType    string
	passthrough bool
	blockPool   block.Pool
	insertQueue chan<- block.SharedColumns
	metrics     metrics.Store
}

func NewScheduler(
	log *slog.Logger,
	cfg *Config,
	seed string,
	dataType string,
	blockPool block.Pool,
	insertQueue chan<- block.SharedColumns,
	metrics metrics.Store,
	passthrough bool,
) *Scheduler {
	return &Scheduler{
		log:         log.With("component", "generate_scheduler"),
		cfg:         cfg,
		seed:        seed,
		dataType:    dataType,
		passthrough: passthrough,
		blockPool:   blockPool,
		insertQueue: insertQueue,
		metrics:     metrics,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	s.log.Info("started")

	// Check if custom fields configuration is enabled
	var customConfig *CustomFieldsConfig
	if s.cfg.EnableCustomFields && s.cfg.ProfileConfigFile != "" {
		var err error
		customConfig, err = LoadCustomFieldsConfig(s.cfg.ProfileConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load custom fields config: %w", err)
		}
		s.log.Info("loaded custom fields config", 
			"file", s.cfg.ProfileConfigFile,
			"fields_count", len(customConfig.CustomFields),
			"data_type", customConfig.DataType)
	}

	// If custom config is provided, use it; otherwise use profile
	var logsFiller *LogsFiller
	var tracesFiller *TracesFiller
	var profilesFiller *ProfilesFiller
	var dynamicFiller *DynamicFiller

	if customConfig != nil {
		// Use custom configuration
		dynamicFiller = NewDynamicFiller(customConfig)
		s.log.Info("using custom fields generator")
	} else {
		// Use traditional profile-based approach
		profileName := s.cfg.Profile
		if profileName == "" {
			profileName = DefaultProfile
		}

		profile, err := GetProfile(profileName)
		if err != nil {
			return fmt.Errorf("failed to load profile: %w", err)
		}
		s.log.Info("loaded profile", "name", profileName)

		switch s.dataType {
		case "logs":
			logsFiller = NewLogsFiller(profile)
		case "traces":
			tracesFiller = NewTracesFiller(profile, s.cfg.Traces)
		case "profiles":
			profilesFiller = NewProfilesFiller(profile, s.cfg.Profiles)
		default:
			return fmt.Errorf("unsupported data type %q", s.dataType)
		}
	}

	var limiter *rate.Limiter
	if s.cfg.RowsPerSecond > 0 {
		limiter = rate.NewLimiter(rate.Limit(s.cfg.RowsPerSecond), s.cfg.RowsPerBlock)
		s.log.Info("rate limiting enabled", "rows_per_second", s.cfg.RowsPerSecond)
	} else {
		s.log.Info("rate limiting disabled (unlimited)")
	}

	var wg sync.WaitGroup
	for i := range s.cfg.Threads {
		w := &worker{
			id:             i,
			idStr:          strconv.Itoa(i),
			log:            s.log,
			rng:            NewRng(s.seed, i),
			blockPool:      s.blockPool,
			insertQueue:    s.insertQueue,
			metrics:        s.metrics,
			limiter:        limiter,
			rowsPerBlock:   s.cfg.RowsPerBlock,
			dataType:       s.dataType,
			passthrough:    s.passthrough,
			logsFiller:     logsFiller,
			tracesFiller:   tracesFiller,
			profilesFiller: profilesFiller,
			dynamicFiller:  dynamicFiller,
			customConfig:   customConfig,
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			s.metrics.IncrementMetric(metrics.ActiveGenerators, 1)
			defer s.metrics.DecrementMetric(metrics.ActiveGenerators, 1)

			if wErr := w.Run(ctx); wErr != nil && !errors.Is(wErr, context.Canceled) {
				s.log.Error("worker error", "worker_id", w.id, "err", wErr)
			}
		}()
	}

	wg.Wait()
	s.log.Info("stopped")
	return nil
}
