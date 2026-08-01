package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDoctorConfig(t *testing.T, configHome, body string) {
	t.Helper()
	dir := filepath.Join(configHome, "agentmux")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestRunDoctorAllTemplatesResolvable(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	writeDoctorConfig(t, configHome, "version: 1\ntemplates:\n  worker:\n    command: echo hi\n    harness_type: codex-cli-execjson\n")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"doctor", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected zero exit code, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, `"ok": true`) {
		t.Fatalf("expected ok true, got %q", got)
	}
	if !strings.Contains(got, `"name": "template:worker"`) || !strings.Contains(got, `"status": "ok"`) {
		t.Fatalf("expected template:worker check to pass, got %q", got)
	}
	// No TUI template is configured, so tmux must be reported as skipped, not
	// checked for real: it would make the test depend on the host having tmux.
	if !strings.Contains(got, `"name": "tmux"`) || !strings.Contains(got, `"status": "skip"`) {
		t.Fatalf("expected tmux check to be skipped, got %q", got)
	}
}

func TestRunDoctorReportsMissingTemplateCommand(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	writeDoctorConfig(t, configHome, "version: 1\ntemplates:\n  worker:\n    command: agentmux-doctor-test-binary-that-does-not-exist --flag\n")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"doctor", "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit code when a template command is missing, stdout=%q", stdout.String())
	}
	got := stdout.String()
	if !strings.Contains(got, `"ok": false`) {
		t.Fatalf("expected ok false, got %q", got)
	}
	if !strings.Contains(got, "agentmux-doctor-test-binary-that-does-not-exist") || !strings.Contains(got, `"status": "fail"`) {
		t.Fatalf("expected the missing command to be named in a failing check, got %q", got)
	}
}

func TestRunDoctorReportsInvalidConfig(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	writeDoctorConfig(t, configHome, "version: 2\ntemplates: {}\n")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"doctor", "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit code for invalid config")
	}
	if got := stdout.String(); !strings.Contains(got, `"name": "config"`) || !strings.Contains(got, `"status": "fail"`) {
		t.Fatalf("expected config check to fail, got %q", got)
	}
}

func TestRunDoctorTextModeDoesNotCrashOnEmptyDetail(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	writeDoctorConfig(t, configHome, "version: 1\ntemplates:\n  worker:\n    command: echo hi\n")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"doctor"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected zero exit code, stderr=%q", stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "STATUS") || !strings.Contains(got, "binary") {
		t.Fatalf("unexpected text output: %q", got)
	}
}

func TestRunDoctorRejectsPositionalArguments(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"doctor", "extra"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit code")
	}
	if !strings.Contains(stderr.String(), "does not accept positional arguments") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}
