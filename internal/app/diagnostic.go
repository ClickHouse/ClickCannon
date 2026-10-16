package app

import (
	"context"
	"log/slog"

	"clickcannon/internal/metricgen"
	otelexport "clickcannon/internal/otel"
	"clickcannon/internal/otlpsize"
)

func printOTLPSize(log *slog.Logger, cfg *Config) {
	breakdowns, err := otlpSizeBreakdowns(cfg)
	if err != nil {
		log.Error("failed to calculate otlp size", "err", err)
		return
	}
	for _, bd := range breakdowns {
		log.LogAttrs(context.Background(), slog.LevelInfo, "otlp attr size breakdown", bd.Attrs()...)
	}
}

func otlpSizeBreakdowns(cfg *Config) ([]otlpsize.Breakdown, error) {
	var out []otlpsize.Breakdown
	if cfg.App.DataType == ConfigDataTypeLogs || cfg.App.DataType == ConfigDataTypeTraces {
		bd, err := otelexport.SyntheticSize(cfg.App.DataType, cfg.Generate, cfg.App.Seed)
		if err != nil {
			return nil, err
		}
		out = append(out, bd)
	}
	if cfg.MetricGen.Enabled {
		bds, err := metricgen.SyntheticSize(cfg.MetricGen)
		if err != nil {
			return nil, err
		}
		out = append(out, bds...)
	}
	return out, nil
}
