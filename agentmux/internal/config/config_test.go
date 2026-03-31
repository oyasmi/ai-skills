package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
	if cfg.Defaults.Tmux.LoadUserConfig {
		t.Fatalf("expected default load_user_config false")
	}
	if cfg.Defaults.Status.BusyTTLMS == nil || *cfg.Defaults.Status.BusyTTLMS != 30000 {
		t.Fatalf("expected default busy ttl 30000, got %v", cfg.Defaults.Status.BusyTTLMS)
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
			Status: StatusDefaults{BusyTTLMS: intPtr(-1)},
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

func TestApplyDefaultsPreservesExplicitZeroBusyTTL(t *testing.T) {
	cfg := Config{
		Version: 1,
		Defaults: Defaults{
			Status: StatusDefaults{BusyTTLMS: intPtr(0)},
		},
		Templates: map[string]Template{
			"worker": {Command: "echo test"},
		},
	}

	cfg.ApplyDefaults()

	if cfg.Defaults.Status.BusyTTLMS == nil || *cfg.Defaults.Status.BusyTTLMS != 0 {
		t.Fatalf("expected explicit zero busy ttl to be preserved, got %v", cfg.Defaults.Status.BusyTTLMS)
	}
}

func TestResolveUsesTemplateHarnessTypeBeforeDefaults(t *testing.T) {
	cfg := Config{
		Version: 1,
		Defaults: Defaults{
			Tmux:        TmuxDefaults{Socket: DefaultSocketPath},
			HarnessType: "codex-cli",
		},
		Templates: map[string]Template{
			"worker": {
				Command:     "echo test",
				HarnessType: "claude-code",
			},
		},
	}

	rt, err := Resolve(cfg, "worker", Override{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rt.HarnessType != "claude-code" {
		t.Fatalf("expected template harness_type, got %q", rt.HarnessType)
	}
}

func TestResolveFallsBackToDefaultHarnessType(t *testing.T) {
	cfg := Config{
		Version: 1,
		Defaults: Defaults{
			Tmux:        TmuxDefaults{Socket: DefaultSocketPath},
			HarnessType: "codex-cli",
		},
		Templates: map[string]Template{
			"worker": {Command: "echo test"},
		},
	}

	rt, err := Resolve(cfg, "worker", Override{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rt.HarnessType != "codex-cli" {
		t.Fatalf("expected default harness_type, got %q", rt.HarnessType)
	}
}

func TestEnsureDefaultConfigWritesPrivateFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := EnsureDefaultConfig(path); err != nil {
		t.Fatalf("EnsureDefaultConfig: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected config mode 0600, got %#o", mode)
	}
}
