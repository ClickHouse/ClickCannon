package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
)

type insertWorker struct {
	id               int
	maxRowsPerInsert int

	chConfig *ClickHouseConfig
	table    string

	nodeBalancer *NodeBalancer

	blockPool   *StructPool[SharedColumns]
	insertQueue <-chan SharedColumns

	metrics MetricsStore
}

func newInsertWorker(id int, config *Config, nodeBalancer *NodeBalancer, blockPool *StructPool[SharedColumns], insertQueue <-chan SharedColumns, metrics MetricsStore) *insertWorker {
	w := insertWorker{
		id:               id,
		maxRowsPerInsert: config.Insert.MaxRowsPerInsert,

		nodeBalancer: nodeBalancer,

		chConfig: &config.Insert.ClickHouse,
		table:    config.GetInsertTable(),

		blockPool:   blockPool,
		insertQueue: insertQueue,

		metrics: metrics,
	}

	return &w
}

func (w *insertWorker) start() {
	defer w.log("exiting")

	chOpts := ch.Options{
		Address:    w.chConfig.Address,
		User:       w.chConfig.User,
		Password:   w.chConfig.Password,
		Database:   w.chConfig.Database,
		ClientName: "otelspam",
		Settings: []ch.Setting{
			{Key: "insert_deduplicate", Value: "0"},
		},
	}

	if w.chConfig.Secure {
		chOpts.TLS = &tls.Config{}
	}

	if w.chConfig.Compression != "" {
		chOpts.Compression, _ = ch.CompressionString(w.chConfig.Compression)
	}

	var c *ch.Client
	var hostIP string
	for i := 0; i < 100; i++ {
		var err error
		c, err = ch.Dial(context.Background(), chOpts)
		if err != nil {
			w.logErr(fmt.Errorf("failed to dial: %w", err))
			continue
		}

		hostIP, err = w.getNode(c)
		if err != nil {
			w.logErr(err)
			if closeErr := c.Close(); closeErr != nil {
				w.logErr(fmt.Errorf("failed to close: %w", closeErr))
			}
			continue
		}

		if w.nodeBalancer == nil || w.nodeBalancer.IsNextNode(hostIP) {
			break
		}

		w.log("incorrect node IP %s in sequence, reconnecting...", hostIP)
		if closeErr := c.Close(); closeErr != nil {
			w.logErr(fmt.Errorf("failed to close: %w", closeErr))
		}
	}
	w.log("Host IP: %s", hostIP)

	defer func(c *ch.Client) {
		closeErr := c.Close()
		if closeErr != nil {
			w.logErr(fmt.Errorf("failed to close: %w", closeErr))
		}
	}(c)

	// Acquire insertBlock once for the worker's lifetime. Keeping it outside the
	// INSERT loop prevents a deadlock when read.threads == insert.threads: if all
	// insert workers released and re-acquired insertBlocks simultaneously, the pool
	// could be exhausted (queue full + readers each holding a block = exact pool
	// capacity), leaving no block for any insert worker to acquire.
	insertBlock := w.blockPool.Acquire()
	insertInput := insertBlock.Input()
	defer w.blockPool.Release(insertBlock)

	for {
		// Before starting query, wait for first block
		// proto.Input is not set on first block since it hasn't been swapped in yet.
		var currentInput proto.Input
		rowCount := 0

		currentBlock, ok := <-w.insertQueue
		if !ok {
			break
		}

		if err := c.Do(context.Background(), ch.Query{
			Body:  fmt.Sprintf("INSERT INTO %q.%q %s VALUES", w.chConfig.Database, w.table, insertInput.Columns()),
			Input: insertInput,
			OnInput: func(ctx context.Context) error {
				insertBlock.Reset()

				if currentInput != nil {
					swapInput(currentInput, insertInput)
					w.blockPool.Release(currentBlock)
					currentBlock = nil
					currentInput = nil
				}
				if rowCount > w.maxRowsPerInsert {
					// Row count may exceed maximum, but not by more than one block
					return io.EOF
				}

				if currentBlock == nil {
					select {
					case nextBlock, ok := <-w.insertQueue:
						if !ok {
							w.log("closing insert, channel not ok. rows: %d\n", rowCount)
							return io.EOF
						}
						currentBlock = nextBlock
					case <-time.After(10 * time.Second):
						w.log("closing insert, no block available after timeout. rows: %d\n", rowCount)
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
						w.metrics.IncrementMetric(MetricNameInsertRowsPerSecond, uint64(e.Value))
					case "InsertedBytes":
						//insertBytesPerSecond.Add(e.Value)
					case "NetworkReceiveBytes":
						w.metrics.IncrementMetric(MetricNameInsertBytesPerSecond, uint64(e.Value))
					default:
						continue
					}
				}
				return nil
			},
		}); err != nil {
			w.logErr(fmt.Errorf("failed to insert: %w", err))
		}
	}
}

func (w *insertWorker) getNode(c *ch.Client) (string, error) {
	sql := `SELECT host_address FROM system.clusters WHERE cluster='default' AND is_local=1`

	var data proto.ColStr
	if err := c.Do(context.Background(), ch.Query{
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

func (w *insertWorker) log(fmtStr string, args ...any) {
	log.Printf("[Insert Worker %d] %s\n", w.id, fmt.Sprintf(fmtStr, args...))
}

func (w *insertWorker) logErr(err error) {
	log.Printf("[Insert Worker %d | error] %s\n", w.id, err.Error())
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
