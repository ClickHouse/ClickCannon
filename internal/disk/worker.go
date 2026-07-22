package disk

import (
	"bufio"
	"clickcannon/internal/block"
	"clickcannon/internal/metrics"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/klauspost/compress/zstd"
)

const ShiftTimestampNone = "none" // Does not shift input data timestamp, replays data as written.
const ShiftTimestampDate = "date" // Shifts only the date of the input data's timestamp to be relative to the current date.
const ShiftTimestampAll = "all"   // Shifts the input data's timestamp to be relative to the current time.
const ShiftTimestampNow = "now"   // Shifts the input data's timestamp to be the current time.
const ShiftTimestampMinute = "minute"

type worker struct {
	id    int
	idStr string
	log   *slog.Logger

	file dataFile

	shiftTimestamp string
	passthrough    bool

	bytesPerSecondLimit uint64

	speedRd *SpeedLimitedReader

	metrics metrics.Store

	timestampSet     bool
	replayTimeKeeper *block.ReplayTimeKeeper

	blockPool   block.Pool
	insertQueue chan<- block.SharedColumns
}

func newWorker(
	id int,
	log *slog.Logger,
	file dataFile,
	shiftTimestamp string,
	bytesPerSecondLimit uint64,
	blockPool block.Pool,
	insertQueue chan<- block.SharedColumns,
	metrics metrics.Store,
	passthrough bool,
	replayTimeKeeper *block.ReplayTimeKeeper,
) *worker {
	return &worker{
		id:    id,
		idStr: strconv.Itoa(id),
		log:   log.With("component", "disk_worker", "id", id, "file", file.Path, "compressed", file.Compressed, "file_index", file.Index, "loop_index", file.LoopIndex),

		file:           file,
		shiftTimestamp: shiftTimestamp,

		bytesPerSecondLimit: bytesPerSecondLimit,

		blockPool:   blockPool,
		insertQueue: insertQueue,
		metrics:     metrics,

		passthrough: passthrough,

		replayTimeKeeper: replayTimeKeeper,
	}
}

func (w *worker) UpdateSpeedLimit(bytesPerSecondLimit uint64) {
	w.bytesPerSecondLimit = bytesPerSecondLimit
	if w.speedRd != nil {
		w.speedRd.Reset(bytesPerSecondLimit)
	}
}

func (w *worker) Run(ctx context.Context) error {
	w.log.Info("started", "speed_limit_bytes", w.bytesPerSecondLimit)

	rd, rdClose, rdErr := w.buildReader()
	if rdErr != nil {
		return fmt.Errorf("failed to build reader: %w", rdErr)
	}
	defer rdClose()

	var dec proto.Block
	for {
		select {
		case <-ctx.Done():
			w.log.Info("stopped")
			return ctx.Err()
		default:
		}

		err := w.decodeBlock(ctx, rd, &dec)
		if errors.Is(err, io.EOF) {
			w.log.Info("finished")
			return nil
		} else if err != nil {
			w.log.Error("read failed, stopping", "err", err)
			return nil
		}
	}
}

func (w *worker) buildReader() (*proto.Reader, func(), error) {
	data, err := os.Open(w.file.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("opening file: %w", err)
	}

	compressedSpeedRd := NewSpeedReader(data, func(n uint64) {
		w.metrics.IncrementMetric(metrics.DiskBytesCompressedTotal, n)
		w.metrics.IncrementMetricWithAttr(metrics.DiskBytesCompressedWorkerTotal, n, "worker_id", w.idStr)
	})

	var (
		optReader io.Reader = bufio.NewReader(compressedSpeedRd)
		zstdClose func()
	)

	if w.file.Compressed {
		zstdRd, err := zstd.NewReader(optReader,
			zstd.IgnoreChecksum(true),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderLowmem(true),
		)
		if err != nil {
			if fileErr := data.Close(); fileErr != nil {
				w.log.Error("closing file", "err", fileErr)
			}

			return nil, nil, fmt.Errorf("creating zstd reader: %w", err)
		}

		zstdClose = zstdRd.Close
		optReader = zstdRd
	}

	w.speedRd = NewSpeedLimitedReader(optReader, w.bytesPerSecondLimit, func(n uint64) {
		w.metrics.IncrementMetric(metrics.DiskBytesUncompressedTotal, n)
		w.metrics.IncrementMetricWithAttr(metrics.DiskBytesUncompressedWorkerTotal, n, "worker_id", w.idStr)
	})

	cleanup := func() {
		w.speedRd.Close()

		if zstdClose != nil {
			zstdClose()
		}

		if fileErr := data.Close(); fileErr != nil {
			w.log.Error("closing file", "err", fileErr)
		}
	}

	return proto.NewReader(w.speedRd), cleanup, nil
}

func isUnsupportedColumnLayoutError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "(columns) !=") || contains(msg, "unexpected column")
}

func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && (s == substr || containsAt(s, substr, 0)))
}

func containsAt(s, substr string, start int) bool {
	if len(substr) == 0 {
		return true
	}
	if start+len(substr) > len(s) {
		return false
	}
	if s[start:start+len(substr)] == substr {
		return true
	}
	return containsAt(s, substr, start+1)
}

func (w *worker) decodeBlock(ctx context.Context, rd *proto.Reader, dec *proto.Block) error {
	cols := w.blockPool.Acquire()
	cols.Reset()

	var colsRes proto.Results
	if dyn, ok := cols.(*block.DynamicSharedColumns); ok {
		colsRes = make(proto.Results, 0)
		err := dec.DecodeRawBlock(rd, 54451, colsRes.Auto())
		if errors.Is(err, io.EOF) {
			w.blockPool.Release(cols)
			return io.EOF
		} else if err != nil {
			w.blockPool.Release(cols)
			if isUnsupportedColumnLayoutError(err) {
				w.log.Warn("skipping incompatible block layout", "err", err)
				return nil
			}
			return fmt.Errorf("failed to decode block: %w", err)
		}
		if err := dyn.BindResults(colsRes); err != nil {
			w.blockPool.Release(cols)
			return fmt.Errorf("failed to bind decoded columns: %w", err)
		}
	} else {
		colsRes = cols.Results()
		err := dec.DecodeRawBlock(rd, 54451, colsRes)
		if errors.Is(err, io.EOF) {
			w.blockPool.Release(cols)
			return io.EOF
		} else if err != nil {
			w.blockPool.Release(cols)
			if isUnsupportedColumnLayoutError(err) {
				w.log.Warn("skipping incompatible block layout", "err", err)
				return nil
			}
			return fmt.Errorf("failed to decode block: %w", err)
		}
	}

	if w.file.LoopIndex == 0 {
		if w.file.Index == 0 && !w.timestampSet {
			w.replayTimeKeeper.ReportEarliestTimestamp(cols.FirstTimestamp())
			w.timestampSet = true
		}

		w.replayTimeKeeper.ReportLatestTimestamp(cols.LastTimestamp())
	}

	switch w.shiftTimestamp {
	case ShiftTimestampDate:
		cols.UpdateDate()
	case ShiftTimestampAll:
		cols.ShiftTimestamp(w.replayTimeKeeper.Snapshot(w.file.LoopIndex))
	case ShiftTimestampNow:
		cols.UpdateTimestampNow()
	case ShiftTimestampMinute:
		cols.UpdateTimestampMinute()
	}

	cols.MutateIDs(w.file.LoopIndex)

	w.metrics.IncrementMetric(metrics.DiskRowsTotal, uint64(colsRes.Rows()))
	w.metrics.IncrementMetricWithAttr(metrics.DiskRowsWorkerTotal, uint64(colsRes.Rows()), "worker_id", w.idStr)

	if w.passthrough {
		// Passthrough for testing max disk read speed.
		// Block is immediately released, never sent to the insert queue.
		w.blockPool.Release(cols)

		return nil
	}

	select {
	case <-ctx.Done():
	case w.insertQueue <- cols:
	}

	return nil
}
