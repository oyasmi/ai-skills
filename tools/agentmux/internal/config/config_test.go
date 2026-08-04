package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

func TestApplyDefaultsSetsTmuxSocket(t *testing.T) {
	cfg := Config{
		Version: 1,
		Templates: map[string]Template{
			"worker": {Command: "echo test"},
		},
	}

	cfg.ApplyDefaults()

	if cfg.Defaults.Tmux.Socket != RecommendedSocketPath() {
		t.Fatalf("expected recommended socket %q, got %q", RecommendedSocketPath(), cfg.Defaults.Tmux.Socket)
	}
	if cfg.Defaults.Tmux.LoadUserConfig {
		t.Fatalf("expected default load_user_config false")
	}
	if cfg.Defaults.Status.BusyTTLMS == nil || *cfg.Defaults.Status.BusyTTLMS != 30000 {
		t.Fatalf("expected default busy ttl 30000, got %v", cfg.Defaults.Status.BusyTTLMS)
	}
}

func TestLoadAppliesDefaultsBeforeValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("version: 1\ntemplates:\n  worker:\n    command: echo test\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config with omitted defaults: %v", err)
	}
	if cfg.Defaults.Tmux.Socket != RecommendedSocketPath() {
		t.Fatalf("expected recommended socket %q, got %q", RecommendedSocketPath(), cfg.Defaults.Tmux.Socket)
	}
	if cfg.Defaults.Capture.PollMS != 250 || cfg.Defaults.Shell != "/bin/bash -lc" {
		t.Fatalf("expected defaults to be applied, got %+v", cfg.Defaults)
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

// An effort typo resolves to "whatever the harness defaults to", which looks
// exactly like the role working. Fail the load instead.
func TestValidateRejectsAnUnknownEffort(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Version:  1,
		Defaults: Defaults{Tmux: TmuxDefaults{Socket: DefaultSocketPath}},
		Templates: map[string]Template{
			"planner": {Command: "claude", HarnessType: "claude-code-ndjson", Effort: "maximum"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an unknown effort to be rejected")
	}
	if code := apperr.Code(err); code != "config_invalid" {
		t.Fatalf("expected config_invalid, got %s", code)
	}
	if !strings.Contains(err.Error(), "xhigh") {
		t.Fatalf("error must list the valid levels, got: %v", err)
	}
}

func TestResolveAppliesEffortAndItsOverride(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Version:  1,
		Defaults: Defaults{Tmux: TmuxDefaults{Socket: DefaultSocketPath}},
		Templates: map[string]Template{
			"planner": {Command: "claude", HarnessType: "claude-code-ndjson", Model: "opus", Effort: " max "},
		},
	}

	rt, err := Resolve(cfg, "planner", Override{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rt.Effort != "max" {
		t.Fatalf("expected effort to resolve trimmed to max, got %q", rt.Effort)
	}

	low := "low"
	rt, err = Resolve(cfg, "planner", Override{Effort: &low})
	if err != nil {
		t.Fatalf("resolve with override: %v", err)
	}
	if rt.Effort != "low" {
		t.Fatalf("expected override to win, got %q", rt.Effort)
	}
}

// The shipped default config is what a new install runs, so every role in it
// must be loadable and internally consistent.
func TestDefaultConfigRolesAreValid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := EnsureDefaultConfig(path); err != nil {
		t.Fatalf("EnsureDefaultConfig: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the shipped default config must load: %v", err)
	}
	for _, role := range []string{"planner", "builder", "reviewer", "scout", "documenter"} {
		tpl, ok := cfg.Templates[role]
		if !ok {
			t.Errorf("default config is missing the %q role", role)
			continue
		}
		if strings.TrimSpace(tpl.Description) == "" {
			t.Errorf("role %q must describe when and how to use it", role)
		}
		if tpl.Effort == "" {
			t.Errorf("role %q must declare an effort; that is half of what makes it a role", role)
		}
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
