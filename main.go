package main

import (
	"clickcannon/internal/app"
	"clickcannon/internal/block"
	"clickcannon/internal/disk"
	"clickcannon/internal/generate"
	"clickcannon/internal/insert"
	"clickcannon/internal/metrics"
	otelexport "clickcannon/internal/otel"
	"clickcannon/internal/user"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"log/slog"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
)

// queryTableSchema queries the ClickHouse system.columns table to retrieve
// the column names and types for the specified database and table.
// Returns empty slices on error to allow graceful fallback to auto-detection.
// Excludes MATERIALIZED and ALIAS columns as they cannot be inserted directly.
func queryTableSchema(ctx context.Context, client *ch.Client, database, table string, log *slog.Logger) ([]string, []proto.ColumnType, error) {
	// Use system.columns to get ordered column names and types
	// Exclude MATERIALIZED and ALIAS columns which cannot be inserted
	sqlFmt := `SELECT name, type FROM system.columns WHERE database = '%s' AND table = '%s' AND default_kind NOT IN ('MATERIALIZED', 'ALIAS') ORDER BY position`
	var names proto.ColStr
	var types proto.ColStr

	// Escape single quotes in identifiers
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	body := fmt.Sprintf(sqlFmt, esc(database), esc(table))

	if err := client.Do(ctx, ch.Query{
		Body: body,
		Result: proto.Results{
			{Name: "name", Data: &names},
			{Name: "type", Data: &types},
		},
	}); err != nil {
		log.Warn("failed to query schema, falling back to auto-detection", "database", database, "table", table, "err", err)
		return nil, nil, err
	}

	// Build slices
	n := names.Rows()
	if n == 0 {
		log.Warn("failed to query schema, falling back to auto-detection", "database", database, "table", table, "err", "no columns found")
		return nil, nil, fmt.Errorf("no columns found for %s.%s", database, table)
	}

	if n != types.Rows() {
		log.Warn("failed to query schema, falling back to auto-detection", "database", database, "table", table, "err", "column count mismatch")
		return nil, nil, fmt.Errorf("column count mismatch: names=%d, types=%d", n, types.Rows())
	}

	nameSlice := make([]string, n)
	typeSlice := make([]proto.ColumnType, n)
	for i := 0; i < n; i++ {
		nameSlice[i] = names.Row(i)
		typeSlice[i] = proto.ColumnType(types.Row(i))
	}

	log.Info("queried schema", "database", database, "table", table, "columns", n)
	return nameSlice, typeSlice, nil
}

func resolveMetricsClickHouseDSN(cfg *app.Config) string {
	if cfg == nil {
		return ""
	}

	if cfg.Metrics.CreateSchema && cfg.Insert.Enabled {
		if cfg.Insert.ClickHouse.Address != "" {
			base := fmt.Sprintf("tcp://%s", cfg.Insert.ClickHouse.Address)
			query := url.Values{}
			if cfg.Insert.ClickHouse.User != "" {
				query.Set("username", cfg.Insert.ClickHouse.User)
			}
			if cfg.Insert.ClickHouse.Password != "" {
				query.Set("password", cfg.Insert.ClickHouse.Password)
			}
			if cfg.Insert.ClickHouse.Secure {
				base = fmt.Sprintf("tls://%s", cfg.Insert.ClickHouse.Address)
			}
			if encoded := query.Encode(); encoded != "" {
				base += "?" + encoded
			}
			return base
		}
	}

	if cfg.Metrics.ClickHouseDSN != "" {
		if cfg.Metrics.CreateSchema && cfg.Insert.Enabled {
			parsed, err := url.Parse(cfg.Metrics.ClickHouseDSN)
			if err == nil {
				query := parsed.Query()
				if cfg.Insert.ClickHouse.User != "" {
					query.Set("username", cfg.Insert.ClickHouse.User)
				}
				if cfg.Insert.ClickHouse.Password != "" {
					query.Set("password", cfg.Insert.ClickHouse.Password)
				}
				parsed.RawQuery = query.Encode()
				return parsed.String()
			}
		}

		return cfg.Metrics.ClickHouseDSN
	}

	if !cfg.Insert.Enabled {
		return ""
	}

	base := fmt.Sprintf("tcp://%s", cfg.Insert.ClickHouse.Address)
	query := url.Values{}
	if cfg.Insert.ClickHouse.User != "" {
		query.Set("username", cfg.Insert.ClickHouse.User)
	}
	if cfg.Insert.ClickHouse.Password != "" {
		query.Set("password", cfg.Insert.ClickHouse.Password)
	}
	if cfg.Insert.ClickHouse.Secure {
		base = fmt.Sprintf("tls://%s", cfg.Insert.ClickHouse.Address)
	}
	if encoded := query.Encode(); encoded != "" {
		base += "?" + encoded
	}

	return base
}

func main() {
	runID, cfgFileName, cfg, log, closeLogFile := app.Setup()
	if closeLogFile != nil {
		defer closeLogFile()
	}

	if cfg.Pprof.Address != "" {
		go func() {
			log.Info("pprof listening", "address", cfg.Pprof.Address)
			if err := http.ListenAndServe(cfg.Pprof.Address, nil); err != nil {
				log.Error("pprof server error", "err", err)
			}
		}()
	}

	runName := cfgFileName
	if cfg.App.Name != "" {
		runName = cfg.App.Name
	}

	targetBytesPerSecond := cfg.Disk.MiBytesPerSecondLimit * 1024 * 1024

	// Only one sink can consume the block queue. OTel export takes precedence
	// over insert when both are enabled.
	insertEnabled := cfg.Insert.Enabled
	otelEnabled := cfg.OTel.Enabled
	if insertEnabled && otelEnabled {
		log.Warn("both insert and otel export are enabled; only one sink can consume the block queue — using otel export, insert disabled")
		insertEnabled = false
	}

	log.Info("config",
		"config_file", cfgFileName,
		"run_name", runName,
		"disk_enabled", cfg.Disk.Enabled,
		"generate_enabled", cfg.Generate.Enabled,
		"insert_enabled", insertEnabled,
		"insert_threads", cfg.Insert.Threads,
		"batch_size", cfg.Insert.BatchSize,
		"otel_enabled", otelEnabled,
		"otel_threads", cfg.OTel.Threads,
	)

	// Determine source thread count for block pool sizing
	sourceThreads := cfg.Disk.Threads
	if cfg.Generate.Enabled {
		sourceThreads = cfg.Generate.Threads
	}

	// The active sink's thread count feeds block pool sizing.
	consumerThreads := 0
	if insertEnabled {
		consumerThreads = cfg.Insert.Threads
	} else if otelEnabled {
		consumerThreads = cfg.OTel.Threads
	}

	blocksToAlloc := (sourceThreads + consumerThreads) * 2
	insertQueue := make(chan block.SharedColumns, blocksToAlloc)
	var blockCreateFunc func() block.SharedColumns

	if cfg.Generate.Enabled {
		// Generate mode: check if custom fields configuration is used
		if cfg.Generate.EnableCustomFields && cfg.Generate.ProfileConfigFile != "" {
			// Load custom configuration to create appropriate columns
			customConfig, err := generate.LoadCustomFieldsConfig(cfg.Generate.ProfileConfigFile)
			if err != nil {
				log.Error("failed to load custom fields config", "err", err)
				return
			}
			
			log.Info("using custom fields configuration",
				"file", cfg.Generate.ProfileConfigFile,
				"fields_count", len(customConfig.CustomFields))
			
			// Create factory function for dynamic columns
			// Validate config first
			testCols, err := generate.NewDynamicColumns(customConfig)
			if err != nil {
				log.Error("failed to create dynamic columns template", "err", err)
				return
			}
			if testCols == nil {
				log.Error("NewDynamicColumns returned nil")
				return
			}
			
			log.Info("validated custom fields configuration", "fields", len(customConfig.CustomFields))
			
			blockCreateFunc = func() block.SharedColumns {
				dynCols, err := generate.NewDynamicColumns(customConfig)
				if err != nil {
					log.Error("failed to create dynamic columns", "err", err)
					// Return a placeholder to avoid nil
					panic(fmt.Sprintf("failed to create dynamic columns: %v", err))
				}
				return dynCols
			}
		} else {
			// Use traditional profile-based column types
			if cfg.App.DataType == app.ConfigDataTypeLogs {
				blockCreateFunc = func() block.SharedColumns {
					return generate.NewGenLogsColumns()
				}
			} else if cfg.App.DataType == app.ConfigDataTypeTraces {
				blockCreateFunc = func() block.SharedColumns {
					return generate.NewGenTracesColumns()
				}
			} else if cfg.App.DataType == app.ConfigDataTypeProfiles {
				blockCreateFunc = func() block.SharedColumns {
					return generate.NewGenProfilesColumns()
				}
			}
		}
	} else {
		// Disk mode: prefer to adapt to the insert target table schema when available.
		if insertEnabled && cfg.Insert.ClickHouse.Address != "" {
			// Try to query ClickHouse for the target table schema.
			func() {
				chOpts := ch.Options{
					Address:    cfg.Insert.ClickHouse.Address,
					User:       cfg.Insert.ClickHouse.User,
					Password:   cfg.Insert.ClickHouse.Password,
					Database:   cfg.Insert.ClickHouse.Database,
					ClientName: "clickcannon",
				}
				if cfg.Insert.ClickHouse.Secure {
					chOpts.TLS = &tls.Config{}
				}
				if cfg.Insert.ClickHouse.Compression != "" {
					chOpts.Compression, _ = ch.CompressionString(cfg.Insert.ClickHouse.Compression)
				}

				c, err := ch.Dial(context.Background(), chOpts)
				if err == nil {
					defer c.Close()

					// Get the target table name based on data type
					var targetTable string
					switch cfg.App.DataType {
					case app.ConfigDataTypeLogs:
						targetTable = cfg.Insert.ClickHouse.LogsTable
					case app.ConfigDataTypeTraces:
						targetTable = cfg.Insert.ClickHouse.TracesTable
					case app.ConfigDataTypeProfiles:
						targetTable = cfg.Insert.ClickHouse.ProfilesTable
					default:
						targetTable = cfg.Insert.ClickHouse.LogsTable
					}

					// Query schema using the helper function
					nameSlice, typeSlice, qerr := queryTableSchema(context.Background(), c, cfg.Insert.ClickHouse.Database, targetTable, log)
					if qerr == nil && len(nameSlice) > 0 {
						// Create factory based on schema
						blockCreateFunc = func() block.SharedColumns {
							return block.NewDynamicSharedColumnsFromSchema(nameSlice, typeSlice)
						}
						return
					}
				} else {
					log.Warn("failed to connect to ClickHouse, falling back to auto-detection", "err", err)
				}

				// Fallback to fully-dynamic container when schema fetch fails
				blockCreateFunc = func() block.SharedColumns {
					return block.NewDynamicSharedColumns()
				}
			}()
		} else {
			blockCreateFunc = func() block.SharedColumns {
				return block.NewDynamicSharedColumns()
			}
		}
	}

	var blockPool block.Pool
	if cfg.Generate.Enabled {
		if cfg.Generate.ReuseBlocks {
			blockPool = block.NewBlockPool(blocksToAlloc, cfg.Generate.BlockRetirementUses, blockCreateFunc)
		} else {
			blockPool = block.NewGarbageBlockPool(blockCreateFunc)
		}
	} else if cfg.Disk.ReuseBlocks {
		blockPool = block.NewBlockPool(blocksToAlloc, cfg.Disk.BlockRetirementUses, blockCreateFunc)
	} else {
		blockPool = block.NewGarbageBlockPool(blockCreateFunc)
	}

	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, os.Interrupt, syscall.SIGTERM)

	metricsCtx, cancelMetrics := context.WithCancel(context.Background())
	var metricsWg sync.WaitGroup

	var metricsStore metrics.Store
	if cfg.Metrics.Enabled {
		runAttr := cfg.Metrics.Attributes
		if runAttr == nil {
			runAttr = make(map[string]string)
		}
		m, metricsErr := metrics.NewWorker(log, runID, runName, cfg.App.DataType, targetBytesPerSecond, cfg.Generate.RowsPerSecond, runAttr, &cfg.Metrics, resolveMetricsClickHouseDSN(cfg), blockPool, insertQueue)
		if metricsErr != nil {
			log.Error("failed to create metrics worker", "err", metricsErr)
			return
		}
		metricsStore = m

		metricsWg.Add(1)
		go func() {
			defer metricsWg.Done()
			mErr := m.Run(metricsCtx)
			if mErr != nil && !errors.Is(mErr, context.Canceled) {
				log.Error("metrics worker error", "err", mErr)
			}
		}()
	} else {
		metricsStore = metrics.NewDisabledStore()
	}

	var sourceWg sync.WaitGroup
	sourceCtx, cancelSource := context.WithCancel(context.Background())
	if cfg.Generate.Enabled {
		gs := generate.NewScheduler(log, &cfg.Generate, cfg.App.Seed, cfg.App.DataType, blockPool, insertQueue, metricsStore, !(insertEnabled || otelEnabled))
		sourceWg.Add(1)
		go func() {
			defer sourceWg.Done()
			gsErr := gs.Run(sourceCtx)
			if gsErr != nil && !errors.Is(gsErr, context.Canceled) {
				log.Error("generate scheduler error", "err", gsErr)
			}
			close(insertQueue)
		}()
	} else if cfg.Disk.Enabled {
		dws := disk.NewScheduler(log, &cfg.Disk, cfg.GetDataFolder(), blockPool, insertQueue, metricsStore, !(insertEnabled || otelEnabled))
		sourceWg.Add(1)
		go func() {
			defer sourceWg.Done()
			dwsErr := dws.Run(sourceCtx)
			if dwsErr != nil && !errors.Is(dwsErr, context.Canceled) {
				log.Error("disk worker scheduler error", "err", dwsErr)
			}
			close(insertQueue)
		}()
	} else {
		close(insertQueue)
	}

	var insertWg sync.WaitGroup
	insertCtx, cancelInsert := context.WithCancel(context.Background())
	if insertEnabled {
		if !cfg.Disk.Enabled && !cfg.Generate.Enabled {
			log.Warn("insert is enabled but no data source (disk/generate) is enabled, insert workers will not start")
		} else {
			iws := insert.NewScheduler(log, &cfg.Insert, cfg.GetInsertTable(), blockCreateFunc, blockPool, insertQueue, metricsStore)
			insertWg.Add(1)
			go func() {
				defer insertWg.Done()
				iwsErr := iws.Run(insertCtx)
				if iwsErr != nil && !errors.Is(iwsErr, context.Canceled) {
					log.Error("insert worker scheduler error", "err", iwsErr)
				}
			}()
		}
	}

	var otelWg sync.WaitGroup
	otelCtx, cancelOtel := context.WithCancel(context.Background())
	if otelEnabled {
		if !cfg.Disk.Enabled && !cfg.Generate.Enabled {
			log.Warn("otel export is enabled but no data source (disk/generate) is enabled, otel workers will not start")
		} else {
			ows := otelexport.NewScheduler(log, &cfg.OTel, cfg.App.DataType, blockPool, insertQueue, metricsStore)
			otelWg.Add(1)
			go func() {
				defer otelWg.Done()
				owsErr := ows.Run(otelCtx)
				if owsErr != nil && !errors.Is(owsErr, context.Canceled) {
					log.Error("otel worker scheduler error", "err", owsErr)
				}
			}()
		}
	}

	var userWg sync.WaitGroup
	userCtx, cancelUser := context.WithCancel(context.Background())
	if cfg.User.Enabled {
		uws := user.NewScheduler(log, cfg.App.Seed, &cfg.User, metricsStore)
		userWg.Add(1)
		go func() {
			defer userWg.Done()
			uwsErr := uws.Run(userCtx)
			if uwsErr != nil && !errors.Is(uwsErr, context.Canceled) {
				log.Error("user worker scheduler error", "err", uwsErr)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		sourceWg.Wait()
		insertWg.Wait()
		otelWg.Wait()
		userWg.Wait()
		close(done)
	}()

	select {
	case <-terminate:
		log.Info("stop requested")
	case <-done:
	}

	cancelSource()
	sourceWg.Wait()
	cancelInsert()
	insertWg.Wait()
	cancelOtel()
	otelWg.Wait()
	cancelUser()
	userWg.Wait()
	cancelMetrics()
	metricsWg.Wait()

	log.Info("done")
}
