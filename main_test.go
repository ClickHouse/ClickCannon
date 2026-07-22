package main

import (
	"testing"

	"clickcannon/internal/app"
)

func TestResolveMetricsClickHouseDSNUsesInsertCredentials(t *testing.T) {
	cfg := &app.Config{}
	cfg.Metrics.Enabled = true
	cfg.Metrics.CreateSchema = true
	cfg.Metrics.ClickHouseDSN = "tcp://metrics-host:9000"
	cfg.Insert.Enabled = true
	cfg.Insert.ClickHouse.Address = "insert-host:9000"
	cfg.Insert.ClickHouse.User = "insert-user"
	cfg.Insert.ClickHouse.Password = "insert-pass"

	dsn := resolveMetricsClickHouseDSN(cfg)
	if dsn == "" {
		t.Fatal("expected resolved DSN, got empty string")
	}
	if dsn != "tcp://metrics-host:9000?password=insert-pass&username=insert-user" {
		t.Fatalf("unexpected DSN: %s", dsn)
	}
}

func TestResolveMetricsClickHouseDSNFallsBackToInsertConfig(t *testing.T) {
	cfg := &app.Config{}
	cfg.Metrics.Enabled = true
	cfg.Metrics.CreateSchema = true
	cfg.Insert.Enabled = true
	cfg.Insert.ClickHouse.Address = "insert-host:9000"
	cfg.Insert.ClickHouse.User = "insert-user"
	cfg.Insert.ClickHouse.Password = "insert-pass"

	dsn := resolveMetricsClickHouseDSN(cfg)
	if dsn != "tcp://insert-host:9000?password=insert-pass&username=insert-user" {
		t.Fatalf("unexpected fallback DSN: %s", dsn)
	}
}
