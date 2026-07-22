package disk

import "testing"

func TestValidateRequiresOnlyCurrentDataTypePath(t *testing.T) {
	cfg := Config{
		Enabled:               true,
		Threads:               1,
		LogsPath:              "log_data",
		MiBytesPerSecondLimit: 1,
	}

	if err := cfg.Validate("logs"); err != nil {
		t.Fatalf("logs data type should only require logs_path, got error: %v", err)
	}

	cfg.TracesPath = ""
	cfg.ProfilesPath = ""
	if err := cfg.Validate("logs"); err != nil {
		t.Fatalf("logs data type should not require traces_path/profiles_path, got error: %v", err)
	}
}
