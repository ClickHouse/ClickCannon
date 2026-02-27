package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/goccy/go-yaml"
)

const ConfigDataTypeLogs = "logs"
const ConfigDataTypeTraces = "traces"

const ConfigShiftTimestampNone = "none" // Does not shift input data timestamp, replays data as written.
const ConfigShiftTimestampDate = "date" // Shifts only the date of the input data's timestamp to be relative to the current date.
const ConfigShiftTimestampAll = "all"   // Shifts the input data's timestamp to be relative to the current time.

type Config struct {
	Label string `yaml:"label"`

	Read struct {
		DataType   string `yaml:"data_type"`
		LogsPath   string `yaml:"logs_path"`
		TracesPath string `yaml:"traces_path"`
		// How many read threads
		Threads int `yaml:"threads"`
		// How many MiB decompressed bytes per second can be read from disk
		MiBytesPerSecondLimit uint64 `yaml:"mb_per_second_limit"`
		// Set to true if you're only testing read performance off of disk, this will not send data to the insert queue
		Passthrough    bool   `yaml:"passthrough"`
		ShiftTimestamp string `yaml:"shift_timestamp"`
	} `yaml:"read"`

	Insert struct {
		// How many insert threads (one connection per thread)
		Threads int `yaml:"threads"`
		// How many rows per INSERT command. Blocks will be streamed until this limit, then a new INSERT will start.
		// After this limit is reached, the current block will finish sending and then end the INSERT, therefore it's not
		// an exact limit.
		MaxRowsPerInsert int `yaml:"max_rows_per_insert"`

		// Distributes the connections across the cluster nodes.
		// Requires multiple reconnects for hosts hidden behind a load balancer.
		BalanceNodes bool `yaml:"balance_nodes"`

		ClickHouse ClickHouseConfig `yaml:"clickhouse"`
	} `yaml:"insert"`

	User UserConfig `yaml:"user"`

	Metrics struct {
		ClickHouseDSN string `yaml:"clickhouse_dsn"`
		Database      string `yaml:"database"`
		RunTable      string `yaml:"run_table"`
		MetricsTable  string `yaml:"metrics_table"`
	} `yaml:"metrics"`
}

type ClickHouseConfig struct {
	Address string `yaml:"address"`

	Secure      bool   `yaml:"secure"`
	Compression string `yaml:"compression"`

	User     string `yaml:"user"`
	Password string `yaml:"password"`

	Database    string `yaml:"database"`
	LogsTable   string `yaml:"logs_table"`
	TracesTable string `yaml:"traces_table"`
}

type UserConfig struct {
	// How many "users" to simulate (one user per thread)
	Threads            int               `yaml:"threads"`
	ConnectionsPerUser int               `yaml:"connections_per_user"`
	MinQueryWait       time.Duration     `yaml:"min_query_wait"`
	MaxQueryWait       time.Duration     `yaml:"max_query_wait"`
	Queries            []UserQueryConfig `yaml:"queries"`
}

type UserQueryConfig struct {
	Name      string        `yaml:"name"`
	SQL       string        `yaml:"sql"`
	TimeRange time.Duration `yaml:"time_range"`
}

func (c *Config) GetDataFolder() string {
	switch c.Read.DataType {
	case ConfigDataTypeLogs:
		return c.Read.LogsPath
	case ConfigDataTypeTraces:
		return c.Read.TracesPath
	default:
		return ""
	}
}

func (c *Config) GetInsertTable() string {
	switch c.Read.DataType {
	case ConfigDataTypeLogs:
		return c.Insert.ClickHouse.LogsTable
	case ConfigDataTypeTraces:
		return c.Insert.ClickHouse.TracesTable
	default:
		return ""
	}
}

func (c *Config) IsLogsData() bool {
	return c.Read.DataType == ConfigDataTypeLogs
}

func (c *Config) Validate() error {
	if c.Read.DataType == "" || (c.Read.DataType != ConfigDataTypeLogs && c.Read.DataType != ConfigDataTypeTraces) {
		return errors.New("must set read.data_type in config to one of: logs, traces")
	}

	if c.Read.DataType == ConfigDataTypeLogs && c.Read.LogsPath == "" {
		return errors.New("must set read.logs_path in config for reading logs")
	} else if c.Read.DataType == ConfigDataTypeTraces && c.Read.TracesPath == "" {
		return errors.New("must set read.traces_path in config for reading traces")
	}

	if c.Read.Threads <= 0 {
		return errors.New("must set read.threads in config")
	}

	if c.Read.MiBytesPerSecondLimit == 0 {
		return errors.New("must set read.mb_per_second_limit in config to a non-zero value")
	}

	if c.Read.ShiftTimestamp == "" {
		c.Read.ShiftTimestamp = ConfigShiftTimestampNone
	} else if c.Read.ShiftTimestamp != ConfigShiftTimestampNone && c.Read.ShiftTimestamp != ConfigShiftTimestampDate && c.Read.ShiftTimestamp != ConfigShiftTimestampAll {
		return errors.New("must set read.shift_timestamp in config to one of: <empty>, none, date, all")
	}

	if c.Insert.Threads <= 0 {
		return errors.New("must set insert.threads in config")
	}

	if c.Insert.MaxRowsPerInsert == 0 {
		c.Insert.MaxRowsPerInsert = 8_000
	}

	if c.Insert.ClickHouse.Address == "" {
		return errors.New("must set insert.clickhouse.address in config")
	}
	if c.Insert.ClickHouse.Compression != "" {
		_, compressErr := ch.CompressionString(c.Insert.ClickHouse.Compression)
		if compressErr != nil {
			return fmt.Errorf("invalid compression in insert.clickhouse.compression: %w", compressErr)
		}
	}

	if c.Insert.ClickHouse.Database == "" {
		c.Insert.ClickHouse.Database = "default"
	}

	if c.Read.DataType == ConfigDataTypeLogs && c.Insert.ClickHouse.LogsTable == "" {
		return errors.New("must set insert.clickhouse.logs_table in config for inserting logs")
	} else if c.Read.DataType == ConfigDataTypeTraces && c.Insert.ClickHouse.TracesTable == "" {
		return errors.New("must set insert.clickhouse.traces_table in config for inserting traces")
	}

	if c.User.MinQueryWait < 0 {
		return errors.New("must set user.min_query_wait to a non-negative value")
	}
	if c.User.MaxQueryWait < time.Millisecond {
		return errors.New("must set user.max_query_wait to a duration higher than 1ms")
	}
	if c.User.MinQueryWait > c.User.MaxQueryWait {
		return errors.New("must set user.max_query_wait to a value higher than user.min_query_wait")
	}
	if c.User.ConnectionsPerUser <= 0 {
		return errors.New("must set user.connections_per_user to a value above 0")
	}

	for i, query := range c.User.Queries {
		if query.SQL == "" {
			return fmt.Errorf("user.queries[%d] SQL is empty", i)
		}
	}

	if c.Metrics.ClickHouseDSN != "" {
		if c.Metrics.Database == "" {
			c.Metrics.Database = "otelspam"
		}

		if c.Metrics.RunTable == "" {
			c.Metrics.Database = "runs"
		}

		if c.Metrics.MetricsTable == "" {
			c.Metrics.Database = "perf"
		}
	}

	return nil
}

func loadConfig() (*Config, error) {
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to YAML configuration file")
	flag.Parse()

	if configPath == "" {
		return nil, errors.New("--config flag is required")
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		return nil, err
	}

	err = config.Validate()
	if err != nil {
		return nil, err
	}

	return &config, nil
}
