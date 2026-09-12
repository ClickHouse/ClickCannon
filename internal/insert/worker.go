package insert

import (
	"clickcannon/internal/block"
	"clickcannon/internal/metrics"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
)

var errWorkerRetired = errors.New("worker retired")

type worker struct {
	id    int
	idStr string
	log   *slog.Logger

	batchSize               int
	workerRetirementBatches int
	initialBatchCount       int

	chConfig *ClickHouseConfig
	table    string

	nodeBalancer *nodeBalancer

	blockCreateFunc func() block.SharedColumns
	blockPool       block.Pool
	insertQueue     <-chan block.SharedColumns

	metrics metrics.Store
}

func newWorker(
	id int,
	log *slog.Logger,
	config *Config,
	nodeBalancer *nodeBalancer,
	blockCreateFunc func() block.SharedColumns,
	blockPool block.Pool,
	insertTable string,
	insertQueue <-chan block.SharedColumns,
	metrics metrics.Store,
) *worker {
	var initialBatchCount int
	if config.WorkerRetirementBatches > 0 && config.Threads > 0 {
		initialBatchCount = (id * config.WorkerRetirementBatches) / config.Threads
	}

	w := worker{
		id:    id,
		idStr: strconv.Itoa(id),
		log:   log.With("component", "insert_worker", "id", id),

		batchSize:               config.BatchSize,
		workerRetirementBatches: config.WorkerRetirementBatches,
		initialBatchCount:       initialBatchCount,

		nodeBalancer: nodeBalancer,

		chConfig: &config.ClickHouse,
		table:    insertTable,

		blockCreateFunc: blockCreateFunc,
		blockPool:       blockPool,
		insertQueue:     insertQueue,

		metrics: metrics,
	}

	return &w
}

func (w *worker) Run(ctx context.Context) error {
	w.log.Info("started")

	c, closeClient, connErr := w.buildClient(ctx)
	if connErr != nil {
		return fmt.Errorf("failed to build client: %w", connErr)
	}
	defer closeClient()

	insertBlock := w.blockCreateFunc()
	insertInput := insertBlock.Input()

	batchCount := w.initialBatchCount

	for {
		select {
		case <-ctx.Done():
			w.log.Info("stopped")
			return ctx.Err()
		default:
		}

		// Before starting query, wait for first block
		// proto.Input is not set on first block since it hasn't been swapped in yet.
		var currentInput proto.Input
		rowCount := 0

		currentBlock, queueOk := <-w.insertQueue
		if !queueOk {
			break
		}

		if err := c.Do(ctx, ch.Query{
			Body:  fmt.Sprintf("INSERT INTO %q.%q %s VALUES", w.chConfig.Database, w.table, insertInput.Columns()),
			Input: insertInput,
			OnInput: func(ctx context.Context) error {
				insertBlock.Reset()

				if currentInput != nil {
					swapInput(currentInput, insertInput)
					// TODO: verify this is called in the case of context cancelled, may have block leak
					w.blockPool.Release(currentBlock)
					currentBlock = nil
					currentInput = nil
				}
				if rowCount > w.batchSize {
					// Row count may exceed maximum, but not by more than one block
					return io.EOF
				}

				if currentBlock == nil {
					select {
					case <-ctx.Done():
						return io.EOF
					case nextBlock, ok := <-w.insertQueue:
						if !ok {
							w.log.Debug("closing insert, queue channel not ok", "rows", rowCount)
							return io.EOF
						}
						currentBlock = nextBlock
					case <-time.After(10 * time.Second):
						w.log.Debug("closing insert, no block available after timeout", "rows", rowCount)
						return io.EOF
					}
				}

				currentInput = currentBlock.Input()

				// This is a hack to swap block reference + underlying column pointers
				swapInput(insertInput, currentInput)

				rowCount += insertInput[0].Data.Rows()

				return nil
			},
			OnProfileEvents: func(ctx context.Context, events []ch.ProfileEvent) error {
				for _, e := range events {
					if e.Type != proto.ProfileIncrement {
						continue
					}

					// https://github.com/ClickHouse/ClickHouse/blob/master/src/Common/ProfileEvents.cpp
					switch e.Name {
					case "InsertedRows":
						w.metrics.IncrementMetric(metrics.InsertRowsTotal, uint64(e.Value))
						w.metrics.IncrementMetricWithAttr(metrics.InsertRowsWorkerTotal, uint64(e.Value), "worker_id", w.idStr)
					case "InsertedBytes":
						w.metrics.IncrementMetric(metrics.InsertBytesUncompressedTotal, uint64(e.Value))
						w.metrics.IncrementMetricWithAttr(metrics.InsertBytesUncompressedWorkerTotal, uint64(e.Value), "worker_id", w.idStr)
					case "NetworkReceiveBytes":
						w.metrics.IncrementMetric(metrics.InsertBytesCompressedTotal, uint64(e.Value))
						w.metrics.IncrementMetricWithAttr(metrics.InsertBytesCompressedWorkerTotal, uint64(e.Value), "worker_id", w.idStr)
					default:
						continue
					}
				}
				return nil
			},
		}); err != nil && !errors.Is(err, context.Canceled) {
			if currentBlock != nil {
				w.log.Debug("releasing leftover block")

				if currentInput != nil {
					insertBlock.Reset()
					swapInput(currentInput, insertInput)
				}
				w.blockPool.Release(currentBlock)
				currentBlock = nil
				currentInput = nil
			}

			return err
		}

		// Release any block still held after context cancellation.
		// OnInput may have loaded a block into currentBlock without getting
		// a chance to release it before ch-go aborted the query.
		if currentBlock != nil {
			if currentInput != nil {
				insertBlock.Reset()
				swapInput(currentInput, insertInput)
				currentInput = nil
			}
			w.blockPool.Release(currentBlock)
			currentBlock = nil
		}

		w.metrics.IncrementMetric(metrics.InsertBatchesTotal, 1)
		w.metrics.IncrementMetricWithAttr(metrics.InsertBatchesWorkerTotal, 1, "worker_id", w.idStr)

		batchCount++
		if w.workerRetirementBatches > 0 && batchCount-w.initialBatchCount >= w.workerRetirementBatches {
			w.metrics.IncrementMetric(metrics.InsertWorkersRetiredTotal, 1)
			w.log.Debug("retiring worker",
				"batches_sent", batchCount-w.initialBatchCount,
				"retirement_limit", w.workerRetirementBatches,
				"initial_offset", w.initialBatchCount,
			)
			return errWorkerRetired
		}
	}

	w.log.Info("stopped")
	return nil
}

func (w *worker) buildClient(ctx context.Context) (*ch.Client, func(), error) {
	chOpts := ch.Options{
		Address:    w.chConfig.Address,
		User:       w.chConfig.User,
		Password:   w.chConfig.Password,
		Database:   w.chConfig.Database,
		ClientName: "clickcannon",
		Settings: []ch.Setting{
			{Key: "insert_deduplicate", Value: "0"},
			{Key: "send_profile_events", Value: "1"},
		},
	}

	if w.chConfig.Secure {
		chOpts.TLS = &tls.Config{}
	}

	if w.chConfig.Compression != "" {
		chOpts.Compression, _ = ch.CompressionString(w.chConfig.Compression)
	}

	var (
		c         *ch.Client
		closeFunc func()
		hostIP    string
		err       error
	)
	for i := 0; i < 100; i++ {
		attempt := i + 1
		c, err = ch.Dial(ctx, chOpts)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to dial: %w", err)
		}
		closeFunc = func() {
			if closeErr := c.Close(); closeErr != nil && !errors.Is(closeErr, ch.ErrClosed) {
				w.log.Error("failed to close conn", "err", closeErr)
			}
		}

		if w.nodeBalancer == nil {
			break
		}

		hostIP, err = w.getNode(ctx, c)
		if err != nil {
			w.log.Error("failed to get node", "attempt", attempt, "err", err)
			closeFunc()
			continue
		}

		if w.nodeBalancer.IsNextNode(hostIP) {
			break
		}

		w.log.Debug("incorrect node IP in sequence, reconnecting", "attempt", attempt, "ip", hostIP)
		closeFunc()
	}

	if c == nil {
		return nil, nil, fmt.Errorf("failed to connect: %w", err)
	}

	w.log.Info("clickhouse connected", "ip", hostIP)
	return c, closeFunc, nil
}

func (w *worker) getNode(ctx context.Context, c *ch.Client) (string, error) {
	sql := `SELECT host_address FROM system.clusters WHERE cluster='default' AND is_local=1`

	var data proto.ColStr
	if err := c.Do(ctx, ch.Query{
		Body: sql,
		Result: proto.Results{
			{Name: "host_address", Data: &data},
		},
	}); err != nil {
		return "", fmt.Errorf("failed to query current node: %w", err)
	}

	if data.Rows() == 0 {
		return "", errors.New("no rows returned for current node IP")
	}

	hostIP := data.First()
	if hostIP == "" {
		return "", errors.New("empty result for current node IP")
	}

	return hostIP, nil
}

// swapInput temporarily swaps the column data within a proto.Input.
// This is a hack since the ch-go client OnInput function doesn't allow using different
// blocks, but I want to save CPU and memory by simply using a placeholder block to stream data.
func swapInput(a, b proto.Input) {
	for i := range a {
		tmp := a[i]
		a[i] = b[i]
		b[i] = tmp
	}
}
