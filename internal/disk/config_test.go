package disk

import "testing"

func validConfig() Config {
	return Config{
		Enabled:               true,
		Threads:               1,
		MiBytesPerSecondLimit: 1,
	}
}

func TestConfigValidate(t *testing.T) {
	disabled := Config{}
	if err := disabled.Validate(); err != nil {
		t.Fatalf("disabled config must validate: %v", err)
	}

	// paths are required per active data type at the app level, not here
	ok := validConfig()
	if err := ok.Validate(); err != nil {
		t.Fatalf("enabled config without paths must validate: %v", err)
	}
	if ok.ShiftTimestamp != ShiftTimestampNone {
		t.Fatalf("shift_timestamp default = %q, want %q", ok.ShiftTimestamp, ShiftTimestampNone)
	}

	bad := validConfig()
	bad.Threads = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("zero threads must fail validation")
	}

	bad = validConfig()
	bad.MiBytesPerSecondLimit = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("zero mb_per_second_limit must fail validation")
	}

	bad = validConfig()
	bad.ShiftTimestamp = "yesterday"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown shift_timestamp must fail validation")
	}
}
