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

func TestParsePromptArgsSupportsStdin(t *testing.T) {
	name, text, key, enter, useStdin, err := parsePromptArgs([]string{"demo", "--stdin", "--enter"})
	if err != nil {
		t.Fatalf("parsePromptArgs: %v", err)
	}
	if name != "demo" || text != "" || key != "" || !enter || !useStdin {
		t.Fatalf("unexpected parsed values: %q %q %q %v %v", name, text, key, enter, useStdin)
	}
}

func TestParsePromptArgsRejectsTextWithStdin(t *testing.T) {
	_, _, _, _, _, err := parsePromptArgs([]string{"demo", "--stdin", "--text", "hello"})
	if err == nil || !strings.Contains(err.Error(), "--stdin cannot be used with --text") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseCaptureArgsSupportsDurationAndRejectsNegative(t *testing.T) {
	name, history, stableMS, timeoutMS, err := parseCaptureArgs([]string{"demo", "--history", "120", "--stable", "1.5s", "--timeout", "500ms"})
	if err != nil {
		t.Fatalf("parseCaptureArgs: %v", err)
	}
	if name != "demo" || history != 120 || stableMS != 1500 || timeoutMS != 500 {
		t.Fatalf("unexpected parsed values: %q %d %d %d", name, history, stableMS, timeoutMS)
	}

	_, _, _, _, err = parseCaptureArgs([]string{"demo", "--stable", "-1"})
	if err == nil || !strings.Contains(err.Error(), "must be non-negative") {
		t.Fatalf("expected negative stable to fail, got %v", err)
	}
}

func TestParseWaitArgsDefaults(t *testing.T) {
	name, stableMS, timeoutMS, err := parseWaitArgs([]string{"demo"})
	if err != nil {
		t.Fatalf("parseWaitArgs: %v", err)
	}
	if name != "demo" || stableMS != 1500 || timeoutMS != 30000 {
		t.Fatalf("unexpected parsed values: %q %d %d", name, stableMS, timeoutMS)
	}
}

func TestReadPromptText(t *testing.T) {
	text, err := readPromptText(strings.NewReader("hello\nworld"))
	if err != nil {
		t.Fatalf("readPromptText: %v", err)
	}
	if text != "hello\nworld" {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestRunVersionJSON(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	prev := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = prev })

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"version", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected zero exit code, stderr=%q", stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, `"command": "version"`) || !strings.Contains(got, `"version": "v1.2.3"`) {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestRunPromptRejectsMissingInput(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	originalStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = originalStdin
		_ = r.Close()
	})

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"prompt", "demo", "--stdin"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit code")
	}
	if !strings.Contains(stderr.String(), "instance \"demo\" not found") && !strings.Contains(stdout.String(), `"instance":"demo"`) {
		t.Fatalf("unexpected output, stdout=%q stderr=%q", stdout.String(), stderr.String())
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
