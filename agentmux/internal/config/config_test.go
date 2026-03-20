package config

import "testing"

func TestApplyDefaultsSetsTmuxSocket(t *testing.T) {
	cfg := Config{
		Version: 1,
		Templates: map[string]Template{
			"worker": {Command: "echo test"},
		},
	}

	cfg.ApplyDefaults()

	if cfg.Defaults.Tmux.Socket != DefaultSocketPath {
		t.Fatalf("expected default socket %q, got %q", DefaultSocketPath, cfg.Defaults.Tmux.Socket)
	}
	if cfg.Defaults.Status.BusyTTLMS != 10000 {
		t.Fatalf("expected default busy ttl 10000, got %d", cfg.Defaults.Status.BusyTTLMS)
	}
}

func TestValidateRejectsEmptyTmuxSocket(t *testing.T) {
	cfg := Config{
		Version: 1,
		Defaults: Defaults{
			Tmux: TmuxDefaults{Socket: "   "},
		},
		Templates: map[string]Template{
			"worker": {Command: "echo test"},
		},
	}

	err := cfg.Validate()
	if err == nil || err.Error() != "tmux socket must not be empty" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsNegativeBusyTTL(t *testing.T) {
	cfg := Config{
		Version: 1,
		Defaults: Defaults{
			Tmux:   TmuxDefaults{Socket: DefaultSocketPath},
			Status: StatusDefaults{BusyTTLMS: -1},
		},
		Templates: map[string]Template{
			"worker": {Command: "echo test"},
		},
	}

	err := cfg.Validate()
	if err == nil || err.Error() != "status.busy_ttl_ms must be non-negative" {
		t.Fatalf("unexpected error: %v", err)
	}
}
