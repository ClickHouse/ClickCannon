package app

import (
	"clickcannon/internal/disk"
	"clickcannon/internal/generate"
	"clickcannon/internal/insert"
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
const ConfigDataTypeMetrics = "metrics"

// Metric types (sub-type of data_type: metrics). Each maps to its own
// OTel exporter table schema.
const (
	MetricsTypeGauge                = "gauge"
	MetricsTypeSum                  = "sum"
	MetricsTypeHistogram            = "histogram"
	MetricsTypeExponentialHistogram = "exponential_histogram"
	MetricsTypeSummary              = "summary"
)

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
		MetricsType  string `yaml:"metrics_type"`
		Seed         string `yaml:"seed"`
	} `yaml:"app"`

	Pprof    PprofConfig     `yaml:"pprof"`
	Disk     disk.Config     `yaml:"disk"`
	Generate generate.Config `yaml:"generate"`
	Insert   insert.Config   `yaml:"insert"`
	OTel     otel.Config     `yaml:"otel"`
	Metrics  metrics.Config  `yaml:"metrics"`
	User     user.Config     `yaml:"user"`
}

func (c Config) GetDataFolder() string {
	switch c.App.DataType {
	case ConfigDataTypeLogs:
		return c.Disk.LogsPath
	case ConfigDataTypeTraces:
		return c.Disk.TracesPath
	case ConfigDataTypeProfiles:
		return c.Disk.ProfilesPath
	case ConfigDataTypeMetrics:
		switch c.App.MetricsType {
		case MetricsTypeGauge:
			return c.Disk.MetricsGaugePath
		case MetricsTypeSum:
			return c.Disk.MetricsSumPath
		case MetricsTypeHistogram:
			return c.Disk.MetricsHistogramPath
		case MetricsTypeExponentialHistogram:
			return c.Disk.MetricsExpHistogramPath
		case MetricsTypeSummary:
			return c.Disk.MetricsSummaryPath
		default:
			return ""
		}
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
	case ConfigDataTypeMetrics:
		switch c.App.MetricsType {
		case MetricsTypeGauge:
			return c.Insert.ClickHouse.MetricsGaugeTable
		case MetricsTypeSum:
			return c.Insert.ClickHouse.MetricsSumTable
		case MetricsTypeHistogram:
			return c.Insert.ClickHouse.MetricsHistogramTable
		case MetricsTypeExponentialHistogram:
			return c.Insert.ClickHouse.MetricsExpHistogramTable
		case MetricsTypeSummary:
			return c.Insert.ClickHouse.MetricsSummaryTable
		default:
			return ""
		}
	default:
		return ""
	}
}

func (c Config) IsLogsData() bool {
	return c.App.DataType == ConfigDataTypeLogs
}

func (c *Config) Validate() error {
	switch c.App.DataType {
	case ConfigDataTypeLogs, ConfigDataTypeTraces, ConfigDataTypeProfiles, ConfigDataTypeMetrics:
	default:
		return errors.New("app: data_type must be one of: logs, traces, profiles, metrics")
	}

	if c.App.DataType == ConfigDataTypeMetrics {
		switch c.App.MetricsType {
		case MetricsTypeGauge, MetricsTypeSum, MetricsTypeHistogram, MetricsTypeExponentialHistogram, MetricsTypeSummary:
		default:
			return errors.New("app: metrics_type must be one of: gauge, sum, histogram, exponential_histogram, summary")
		}
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

	if err := c.Generate.Validate(); err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	if err := c.Insert.Validate(); err != nil {
		return fmt.Errorf("insert: %w", err)
	}

	// Only the table for the active data type is required.
	if c.Insert.Enabled && c.GetInsertTable() == "" {
		switch c.App.DataType {
		case ConfigDataTypeLogs:
			return errors.New("insert: clickhouse: must set logs_table")
		case ConfigDataTypeTraces:
			return errors.New("insert: clickhouse: must set traces_table")
		case ConfigDataTypeProfiles:
			return errors.New("insert: clickhouse: must set profiles_table")
		default:
			return errors.New("insert: clickhouse: must set the table for the active data type")
		}
	}

	if err := c.OTel.Validate(); err != nil {
		return fmt.Errorf("otel: %w", err)
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
