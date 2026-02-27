package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/klauspost/compress/zstd"
)

type readWorker struct {
	id                  int
	filePath            string
	fileCompressed      bool
	bytesPerSecondLimit float64
	passthrough         bool
	shiftTimestamp      string

	firstTimestamp time.Time

	speedRd *SpeedLimitedReader

	blockPool   *StructPool[SharedColumns]
	insertQueue chan<- SharedColumns

	metrics MetricsStore
}

func newReadWorker(id int, shiftTimestamp string, filePath string, fileCompressed bool, bytesPerSecondLimit float64, passthrough bool, blockPool *StructPool[SharedColumns], insertQueue chan<- SharedColumns, metrics MetricsStore) *readWorker {
	w := readWorker{
		id:                  id,
		filePath:            filePath,
		fileCompressed:      fileCompressed,
		bytesPerSecondLimit: bytesPerSecondLimit,
		passthrough:         passthrough,
		shiftTimestamp:      shiftTimestamp,

		blockPool:   blockPool,
		insertQueue: insertQueue,

		metrics: metrics,
	}

	return &w
}

func (w *readWorker) UpdateSpeedLimit(bytesPerSecond float64) {
	if w.speedRd == nil {
		return
	}

	w.speedRd.Reset(bytesPerSecond)
}

func (w *readWorker) start(ctx context.Context) {
	fmt.Printf("[reader %d] reading %s (compressed: %t)\n", w.id, w.filePath, w.fileCompressed)
	data, err := os.Open(w.filePath)
	if err != nil {
		panic(err)
	}
	defer func(data *os.File) {
		closeErr := data.Close()
		if closeErr != nil {
			panic(closeErr)
		}
	}(data)

	var dec proto.Block
	compressedSpeedRd := NewSpeedReader(data, func(n uint64) {
		w.metrics.IncrementMetric(MetricNameReadCompressedBytesPerSecond, n)
		w.metrics.IncrementMetric(MetricNameTotalBytesCompressed, n)
	})
	bufRd := bufio.NewReader(compressedSpeedRd)
	var optReader io.Reader = bufRd
	if w.fileCompressed {
		zstdRd, err := zstd.NewReader(optReader,
			zstd.IgnoreChecksum(true),
			zstd.WithDecoderConcurrency(1), // TODO: with higher concurrency, this performs better when set to 1
			zstd.WithDecoderLowmem(true),
		)
		if err != nil {
			panic(err)
		}
		defer zstdRd.Close()
		optReader = zstdRd
	}
	optReader = bufio.NewReaderSize(optReader, 32*1024)
	w.speedRd = NewSpeedLimitedReader(optReader, w.bytesPerSecondLimit, func(n uint64) {
		w.metrics.IncrementMetric(MetricNameReadUncompressedBytesPerSecond, n)
		w.metrics.IncrementMetric(MetricNameTotalBytesUncompressed, n)
	})
	defer w.speedRd.Close()

	rd := proto.NewReader(w.speedRd)

	for {
		cols, ok := w.blockPool.AcquireCtx(ctx)
		if !ok {
			return
		}

		colsRes := cols.Results()
		err = dec.DecodeRawBlock(rd, 54451, colsRes)
		if errors.Is(err, io.EOF) {
			w.blockPool.Release(cols)
			break
		} else if err != nil {
			panic(err)
		}

		switch w.shiftTimestamp {
		case ConfigShiftTimestampDate:
			cols.UpdateDate()
		case ConfigShiftTimestampAll:
			if w.firstTimestamp.IsZero() {
				w.firstTimestamp = cols.FirstTimestamp()
			}

			cols.UpdateTimestamp(w.firstTimestamp)
		}

		w.metrics.IncrementMetric(MetricNameReadRowsPerSecond, uint64(colsRes.Rows()))
		w.metrics.IncrementMetric(MetricNameTotalRows, uint64(colsRes.Rows()))

		if w.passthrough {
			// Passthrough for testing max disk read speed.
			// Block is immediately released, never sent to the insert queue.
			w.blockPool.Release(cols)
		} else {
			select {
			case w.insertQueue <- cols:
			case <-ctx.Done():
				w.blockPool.Release(cols)
				return
			}
		}
	}
}
