package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsListPositionalArguments(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"list", "templates"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit code")
	}
	if got := stderr.String(); !strings.Contains(got, "list does not accept positional arguments") {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestRunTemplateListStillWorks(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"template", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected zero exit code, stderr=%q", stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "NAME") || !strings.Contains(got, "MODEL") || !strings.Contains(got, "深度编码专家") {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func setupXDGHome(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	stateHome := filepath.Join(root, "state")
	configHome := filepath.Join(root, "config")
	if err := os.MkdirAll(stateHome, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.MkdirAll(configHome, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	return stateHome, configHome
}
