package app

import (
	"clickcannon/internal/disk"
	"clickcannon/internal/generate"
	"clickcannon/internal/insert"
	"clickcannon/internal/metricgen"
	"clickcannon/internal/metrics"
	"clickcannon/internal/otel"
	"clickcannon/internal/user"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

const ConfigDataTypeLogs = "logs"
const ConfigDataTypeTraces = "traces"
const ConfigDataTypeProfiles = "profiles"

type PprofConfig struct {
	// Address to listen on for the pprof HTTP endpoint (e.g. "localhost:6060").
	// Leave empty to disable pprof.
	Address string `yaml:"address"`
}

type Config struct {
	App struct {
		Name         string `yaml:"name"`
		LogToFile    bool   `yaml:"log_to_file"`
		LogToConsole bool   `yaml:"log_to_console"`
		LogLevel     string `yaml:"log_level"`
		DataType     string `yaml:"data_type"`
		Seed         string `yaml:"seed"`
	} `yaml:"app"`

	Pprof     PprofConfig      `yaml:"pprof"`
	Disk      disk.Config      `yaml:"disk"`
	Generate  generate.Config  `yaml:"generate"`
	Insert    insert.Config    `yaml:"insert"`
	OTel      otel.Config      `yaml:"otel"`
	MetricGen metricgen.Config `yaml:"metric_gen"`
	Metrics   metrics.Config   `yaml:"metrics"`
	User      user.Config      `yaml:"user"`
}

func (c Config) GetDataFolder() string {
	switch c.App.DataType {
	case ConfigDataTypeLogs:
		return c.Disk.LogsPath
	case ConfigDataTypeTraces:
		return c.Disk.TracesPath
	case ConfigDataTypeProfiles:
		return c.Disk.ProfilesPath
	default:
		return ""
	}
}

func (c Config) GetInsertTable() string {
	switch c.App.DataType {
	case ConfigDataTypeLogs:
		return c.Insert.ClickHouse.LogsTable
	case ConfigDataTypeTraces:
		return c.Insert.ClickHouse.TracesTable
	case ConfigDataTypeProfiles:
		return c.Insert.ClickHouse.ProfilesTable
	default:
		return ""
	}
}

func (c Config) IsLogsData() bool {
	return c.App.DataType == ConfigDataTypeLogs
}

func (c *Config) Validate() error {
	// data_type drives the block pipeline (disk/generate -> insert/otel). The
	// metric_gen and user modes don't touch blocks, so a run using only those
	// may omit it.
	blockModesEnabled := c.Disk.Enabled || c.Generate.Enabled || c.Insert.Enabled || c.OTel.Enabled
	if c.App.DataType == "" {
		if blockModesEnabled {
			return errors.New("app: data_type must be set (one of: logs, traces, profiles) when disk/generate/insert/otel modes are enabled")
		}
	} else if c.App.DataType != ConfigDataTypeLogs && c.App.DataType != ConfigDataTypeTraces && c.App.DataType != ConfigDataTypeProfiles {
		return errors.New("app: data_type must be one of: logs, traces, profiles")
	}

	if c.Disk.Enabled && c.Generate.Enabled {
		return errors.New("disk and generate cannot both be enabled; use one or the other")
	}

	if c.App.DataType == ConfigDataTypeProfiles && c.OTel.Enabled {
		return errors.New("otel export does not support data_type: profiles")
	}

	if err := c.Disk.Validate(); err != nil {
		return fmt.Errorf("disk: %w", err)
	}

	if c.Disk.Enabled && c.GetDataFolder() == "" {
		switch c.App.DataType {
		case ConfigDataTypeLogs:
			return errors.New("disk: must set logs_path")
		case ConfigDataTypeTraces:
			return errors.New("disk: must set traces_path")
		case ConfigDataTypeProfiles:
			return errors.New("disk: must set profiles_path")
		}
	}

	if err := c.Generate.Validate(); err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	if err := c.Insert.Validate(); err != nil {
		return fmt.Errorf("insert: %w", err)
	}

	if c.Insert.Enabled && c.GetInsertTable() == "" {
		switch c.App.DataType {
		case ConfigDataTypeLogs:
			return errors.New("insert: clickhouse: must set logs_table")
		case ConfigDataTypeTraces:
			return errors.New("insert: clickhouse: must set traces_table")
		case ConfigDataTypeProfiles:
			return errors.New("insert: clickhouse: must set profiles_table")
		}
	}

	if err := c.OTel.Validate(); err != nil {
		return fmt.Errorf("otel: %w", err)
	}

	if err := c.MetricGen.Validate(); err != nil {
		return fmt.Errorf("metric_gen: %w", err)
	}

	if err := c.Metrics.Validate(); err != nil {
		return fmt.Errorf("metrics: %w", err)
	}

	if err := c.User.Validate(); err != nil {
		return fmt.Errorf("user: %w", err)
	}

	return nil
}

func LoadConfig() (*Config, string, error) {
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to YAML configuration file")
	flag.Parse()

	if configPath == "" {
		configPath = os.Getenv("CLICKCANNON_CONFIG")
	}

	if configPath == "" {
		return nil, "", errors.New("--config flag or CLICKCANNON_CONFIG env var is required")
	}

	configFileName := filepath.Base(configPath)

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, "", err
	}

	var config Config
	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		return nil, "", err
	}

	err = config.Validate()
	if err != nil {
		return nil, "", fmt.Errorf("invalid config: %w", err)
	}

	return &config, configFileName, nil
}
