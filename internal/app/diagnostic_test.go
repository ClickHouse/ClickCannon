package app

import (
	"slices"
	"testing"
)

func TestOTLPSizeBreakdowns(t *testing.T) {
	metrics := []string{"metrics/gauge", "metrics/sum", "metrics/histogram", "metrics/exponential_histogram", "metrics/summary"}
	tests := []struct {
		name      string
		dataType  string
		metricGen bool
		want      []string
	}{
		{"logs", ConfigDataTypeLogs, false, []string{"logs"}},
		{"traces", ConfigDataTypeTraces, false, []string{"traces"}},
		{"metrics", "", true, metrics},
		{"logs and metrics", ConfigDataTypeLogs, true, append([]string{"logs"}, metrics...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			cfg.App.DataType = tt.dataType
			cfg.App.Seed = "otlp-size"
			cfg.MetricGen.Enabled = tt.metricGen

			breakdowns, err := otlpSizeBreakdowns(&cfg)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, bd := range breakdowns {
				got = append(got, bd.Signal)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("signals = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidatePrintOTLPSize(t *testing.T) {
	tests := []struct {
		name      string
		dataType  string
		metricGen bool
		wantErr   bool
	}{
		{"logs", ConfigDataTypeLogs, false, false},
		{"traces", ConfigDataTypeTraces, false, false},
		{"metrics", "", true, false},
		{"profiles", ConfigDataTypeProfiles, false, true},
		{"no signal", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			cfg.App.PrintOTLPSize = true
			cfg.App.DataType = tt.dataType
			cfg.MetricGen.Enabled = tt.metricGen
			cfg.MetricGen.URL = "localhost:4317"

			if err := cfg.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
