package app

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
)

func setupErr(err error) {
	fmt.Println(err)
	os.Exit(1)
}

func Setup() (string, string, *Config, *slog.Logger, func()) {
	time.Local = time.UTC

	runID := os.Getenv("CLICKCANNON_RUN_ID")
	if runID == "" {
		runID = NewRunID()
	}

	cfg, cfgFileName, err := LoadConfig()
	if err != nil {
		setupErr(fmt.Errorf("failed to load config: %w", err))
	}

	if cfg.App.Seed == "" {
		cfg.App.Seed = runID
	}

	logLevel, err := ParseLogLevel(cfg.App.LogLevel)
	if err != nil {
		setupErr(fmt.Errorf("failed to parse log level: %w", err))
	}

	timeStr := time.Now().Format("2006-01-02_15-04-05")
	logFileName := fmt.Sprintf("clickcannon_%s_%s.log", timeStr, runID[:8])
	log, logFile, err := NewLogger(logFileName, logLevel, cfg.App.LogToConsole, cfg.App.LogToFile)
	if err != nil {
		setupErr(fmt.Errorf("failed to create logger: %w", err))
	}
	log = log.With("run_id", runID)

	var closeFunc func()
	if logFile != nil {
		closeFunc = func() {
			fileCloseErr := logFile.Close()
			if fileCloseErr != nil {
				log.Error("failed to close log file", "file", logFileName, "err", fileCloseErr)
			}
		}
	}

	log.Info("starting clickcannon", "seed", cfg.App.Seed, "data_type", cfg.App.DataType)
	if cfg.App.PrintOTLPSize {
		printOTLPSize(log, cfg)
	}

	return runID, cfgFileName, cfg, log, closeFunc
}

func NewRunID() string {
	return uuid.Must(uuid.NewUUID()).String()
}
