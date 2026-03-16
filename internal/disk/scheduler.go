package disk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"otelspam/internal/block"
	"otelspam/internal/metrics"
	"sync"
)

type Scheduler struct {
	log         *slog.Logger
	workerLog   *slog.Logger
	diskCfg     *Config
	folderPath  string
	passthrough bool

	blockPool   block.Pool
	insertQueue chan<- block.SharedColumns
	metrics     metrics.Store

	replayTimeKeeper *block.ReplayTimeKeeper

	mu      sync.RWMutex
	workers map[int]*worker
}

func NewScheduler(log *slog.Logger, diskCfg *Config, folderPath string, blockPool block.Pool, insertQueue chan<- block.SharedColumns, metrics metrics.Store, passthrough bool) *Scheduler {
	return &Scheduler{
		log:              log.With("component", "disk_scheduler"),
		workerLog:        log,
		diskCfg:          diskCfg,
		folderPath:       folderPath,
		passthrough:      passthrough,
		blockPool:        blockPool,
		insertQueue:      insertQueue,
		metrics:          metrics,
		workers:          make(map[int]*worker),
		replayTimeKeeper: block.NewReplayTimeKeeper(log),
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	s.log.Info("started")

	dataFiles, err := getDataFiles(s.folderPath)
	if err != nil {
		return fmt.Errorf("failed to load data files: %w", err)
	}

	if len(dataFiles) == 0 {
		s.log.Warn("no data files found, exiting")
		return nil
	}

	s.log.Info("found files", "file_count", len(dataFiles))

	var wg sync.WaitGroup
	fileCh := s.generateFiles(ctx, dataFiles)

	for i := range s.diskCfg.Threads {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.metrics.IncrementMetric(metrics.ActiveReaders, 1)
			s.runWorker(ctx, id, fileCh)
			s.metrics.DecrementMetric(metrics.ActiveReaders, 1)
		}(i)
	}

	var timeWg sync.WaitGroup
	timeCtx, cancelTimeCtx := context.WithCancel(ctx)
	timeWg.Add(1)
	go func() {
		defer timeWg.Done()
		s.replayTimeKeeper.Run(timeCtx)
	}()

	wg.Wait()

	cancelTimeCtx()
	timeWg.Wait()

	s.log.Info("stopped")

	return nil
}

func (s *Scheduler) generateFiles(ctx context.Context, dataFiles []dataFile) <-chan dataFile {
	ch := make(chan dataFile)

	go func() {
		defer close(ch)

		i := 0
		for {
			for j, f := range dataFiles {
				f.LoopIndex = i

				select {
				case <-ctx.Done():
					return
				case ch <- f:
					if i > 0 && j == 0 {
						s.log.Info("looped files", "loop_count", i, "file_count", len(dataFiles))
					}
				}
			}

			if !s.diskCfg.Loop {
				return
			}

			i++
		}
	}()

	return ch
}

func (s *Scheduler) runWorker(ctx context.Context, workerID int, fileCh <-chan dataFile) {
	for {
		select {
		case <-ctx.Done():
			return
		case file, ok := <-fileCh:
			if !ok {
				return
			}

			w, err := newWorker(workerID, s.workerLog, file, s.diskCfg.ShiftTimestamp, s.getNewWorkerSpeed(), s.blockPool, s.insertQueue, s.metrics, s.passthrough, s.replayTimeKeeper)
			if err != nil && !errors.Is(err, context.Canceled) {
				s.log.Warn("newWorker failed, skipping file", "worker_id", workerID, "file", file.Path, "err", err)
				continue
			}

			s.register(workerID, w)
			s.rebalanceSpeed()

			err = w.Run(ctx)

			s.unregister(workerID)
			s.rebalanceSpeed()

			if err != nil && !errors.Is(err, context.Canceled) {
				s.log.Warn("worker failed, skipping file", "worker_id", workerID, "file", file.Path, "err", err)
			}
		}
	}
}

func (s *Scheduler) register(id int, w *worker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workers[id] = w
}

func (s *Scheduler) unregister(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.workers, id)
}

func (s *Scheduler) rebalanceSpeed() {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := len(s.workers)
	if count == 0 {
		return
	}

	perWorker := (s.diskCfg.MiBytesPerSecondLimit * 1024 * 1024) / uint64(count)

	for _, w := range s.workers {
		w.UpdateSpeedLimit(perWorker)
	}

	s.metrics.SetMetric(metrics.TargetWorkerBytesPerSecond, perWorker)
}

func (s *Scheduler) getNewWorkerSpeed() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// TODO: this is lazy. +1, compared to rebalanceSpeed()
	return (s.diskCfg.MiBytesPerSecondLimit * 1024 * 1024) / uint64(len(s.workers)+1)
}
