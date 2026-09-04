package otel

import "testing"

func TestConfigValidateProtocol(t *testing.T) {
	base := func(protocol, url string) Config {
		return Config{
			Enabled:   true,
			Protocol:  protocol,
			URL:       url,
			Threads:   1,
			BatchSize: 1,
		}
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "empty protocol defaults ok", cfg: base("", "localhost:4317")},
		{name: "grpc ok", cfg: base(ProtocolGRPC, "localhost:4317")},
		{name: "http ok", cfg: base(ProtocolHTTP, "http://localhost:4318")},
		{name: "http with bare host ok", cfg: base(ProtocolHTTP, "localhost:4318")},
		{name: "grpc with grpc scheme ok", cfg: base(ProtocolGRPC, "grpc://localhost:4317")},
		{name: "unknown protocol rejected", cfg: base("http/json", "http://localhost:4318"), wantErr: true},
		{name: "http with grpc scheme rejected", cfg: base(ProtocolHTTP, "grpc://localhost:4317"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

// A disabled exporter must not be validated at all, so an otherwise-invalid
// protocol is tolerated when enabled is false.
func TestConfigValidateSkippedWhenDisabled(t *testing.T) {
	cfg := Config{Protocol: "nonsense"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() on disabled config = %v, want nil", err)
	}
}

func TestConfigWithDefaults(t *testing.T) {
	got := Config{}.withDefaults()
	if got.Protocol != ProtocolGRPC {
		t.Errorf("Protocol = %q, want %q", got.Protocol, ProtocolGRPC)
	}
	if got.FlushInterval != defaultFlushInterval {
		t.Errorf("FlushInterval = %v, want %v", got.FlushInterval, defaultFlushInterval)
	}
	if got.Timeout != defaultTimeout {
		t.Errorf("Timeout = %v, want %v", got.Timeout, defaultTimeout)
	}

	explicit := Config{Protocol: ProtocolHTTP}.withDefaults()
	if explicit.Protocol != ProtocolHTTP {
		t.Errorf("explicit Protocol = %q, want %q", explicit.Protocol, ProtocolHTTP)
	}
}
