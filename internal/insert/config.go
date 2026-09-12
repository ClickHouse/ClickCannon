package insert

import (
	"errors"
	"fmt"

	"github.com/ClickHouse/ch-go"
)

type Config struct {
	Enabled bool `yaml:"enabled"`

	// How many insert threads (one connection per thread)
	Threads int `yaml:"threads"`
	// How many rows per INSERT command. Blocks will be streamed until this limit, then a new INSERT will start.
	// After this limit is reached, the current block will finish sending and then end the INSERT, therefore it's not
	// an exact limit.
	BatchSize int `yaml:"batch_size"`
	// How many batches a worker sends before retiring and reconnecting. The ch-go encoder accumulates
	// buffer allocations over time; retiring workers periodically bounds this memory growth.
	// Workers are staggered across the retirement window using their ID so they don't all restart at once.
	// Set to 0 to disable retirement.
	WorkerRetirementBatches int `yaml:"worker_retirement_batches"`

	// Distributes the connections across the cluster nodes.
	// Requires multiple reconnects for hosts hidden behind a load balancer.
	BalanceNodes bool `yaml:"balance_nodes"`

	ClickHouse ClickHouseConfig `yaml:"clickhouse"`
}

type ClickHouseConfig struct {
	Address string `yaml:"address"`

	Secure      bool   `yaml:"secure"`
	Compression string `yaml:"compression"`

	User     string `yaml:"user"`
	Password string `yaml:"password"`

	Database      string `yaml:"database"`
	LogsTable     string `yaml:"logs_table"`
	TracesTable   string `yaml:"traces_table"`
	ProfilesTable string `yaml:"profiles_table"`
}

func (c *ClickHouseConfig) Validate() error {
	if c.Address == "" {
		return errors.New("must set address")
	}
	if c.Compression != "" {
		_, compressErr := ch.CompressionString(c.Compression)
		if compressErr != nil {
			return fmt.Errorf("invalid compression value: %w", compressErr)
		}
	}

	if c.Database == "" {
		return errors.New("must set database")
	}

	return nil
}

func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Threads < 1 {
		return errors.New("must set threads to a value greater than zero")
	}

	if c.BatchSize < 1 {
		return errors.New("must set batch_size to a value greater than zero")
	}

	if c.WorkerRetirementBatches < 0 {
		return errors.New("worker_retirement_batches must be >= 0")
	}

	if err := c.ClickHouse.Validate(); err != nil {
		return fmt.Errorf("clickhouse: %w", err)
	}

	return nil
}
