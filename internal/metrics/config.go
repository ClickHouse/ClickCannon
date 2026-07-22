package metrics

import "errors"

type Config struct {
	Enabled       bool              `yaml:"enabled"`
	ClickHouseDSN string            `yaml:"clickhouse_dsn"`
	Database      string            `yaml:"database"`
	RunTable      string            `yaml:"run_table"`
	MetricsTable  string            `yaml:"metrics_table"`
	CreateSchema  bool              `yaml:"create_schema"`
	Attributes    map[string]string `yaml:"attributes"`
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Database == "" {
		return errors.New("must set database")
	}

	if c.RunTable == "" {
		return errors.New("must set run_table")
	}

	if c.MetricsTable == "" {
		return errors.New("must set metrics_table")
	}

	return nil
}
