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
	if got := stdout.String(); !strings.Contains(got, "NAME") || !strings.Contains(got, "MODEL") || !strings.Contains(got, "HARNESS") || !strings.Contains(got, "claude-code") {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestParsePromptArgsSupportsStdin(t *testing.T) {
	name, text, key, useStdin, err := parsePromptArgs([]string{"demo", "--stdin"})
	if err != nil {
		t.Fatalf("parsePromptArgs: %v", err)
	}
	if name != "demo" || text != "" || key != "" || !useStdin {
		t.Fatalf("unexpected parsed values: %q %q %q %v", name, text, key, useStdin)
	}
}

func TestParsePromptArgsRejectsTextWithStdin(t *testing.T) {
	_, _, _, _, err := parsePromptArgs([]string{"demo", "--stdin", "--text", "hello"})
	if err == nil || !strings.Contains(err.Error(), "--stdin cannot be used with --text") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParsePromptArgsRejectsRemovedEnterFlag(t *testing.T) {
	_, _, _, _, err := parsePromptArgs([]string{"demo", "--enter"})
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined: -enter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseCaptureArgsRejectsLegacyWaitFlags(t *testing.T) {
	name, opts, err := parseCaptureArgs([]string{"demo", "--history", "120"})
	if err != nil {
		t.Fatalf("parseCaptureArgs: %v", err)
	}
	if name != "demo" || opts.History != 120 || opts.Scope != "current" {
		t.Fatalf("unexpected parsed values: %q %+v", name, opts)
	}

	_, _, err = parseCaptureArgs([]string{"demo", "--stable", "1500"})
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("expected legacy stable flag to fail, got %v", err)
	}
}

func TestParseCaptureArgsSupportsScope(t *testing.T) {
	name, opts, err := parseCaptureArgs([]string{"demo", "--scope", "session"})
	if err != nil {
		t.Fatalf("parseCaptureArgs: %v", err)
	}
	if name != "demo" || opts.History != -1 || opts.Scope != "session" {
		t.Fatalf("unexpected parsed values: %q %+v", name, opts)
	}

	_, _, err = parseCaptureArgs([]string{"demo", "--scope", "all"})
	if err == nil || !strings.Contains(err.Error(), "current or session") {
		t.Fatalf("expected invalid scope to fail, got %v", err)
	}
}

func TestParseCaptureArgsRawIsOptIn(t *testing.T) {
	_, opts, err := parseCaptureArgs([]string{"demo"})
	if err != nil {
		t.Fatalf("parseCaptureArgs: %v", err)
	}
	if opts.Raw {
		t.Fatal("raw protocol payloads must be opt-in")
	}
	_, opts, err = parseCaptureArgs([]string{"demo", "--raw"})
	if err != nil {
		t.Fatalf("parseCaptureArgs --raw: %v", err)
	}
	if !opts.Raw {
		t.Fatal("expected --raw to be parsed")
	}
}

// `agentmux capture --history 40 worker` used to treat "--history" as the
// instance name and fail with an unrelated message.
func TestParseArgsRejectFlagInInstanceNamePosition(t *testing.T) {
	if _, _, err := parseCaptureArgs([]string{"--history", "40", "demo"}); err == nil ||
		!strings.Contains(err.Error(), "instance name must come before flags") {
		t.Fatalf("unexpected capture error: %v", err)
	}
	if _, _, _, _, err := parsePromptArgs([]string{"--text", "hi", "demo"}); err == nil ||
		!strings.Contains(err.Error(), "instance name must come before flags") {
		t.Fatalf("unexpected prompt error: %v", err)
	}
	if _, _, _, err := parseWaitArgs([]string{"--timeout", "5s", "demo"}); err == nil ||
		!strings.Contains(err.Error(), "instance name must come before flags") {
		t.Fatalf("unexpected wait error: %v", err)
	}
	if _, _, _, err := parseHaltArgs([]string{"--immediately", "demo"}); err == nil ||
		!strings.Contains(err.Error(), "instance name must come before flags") {
		t.Fatalf("unexpected halt error: %v", err)
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

func TestParseHaltArgsDefaults(t *testing.T) {
	name, immediately, timeoutMS, err := parseHaltArgs([]string{"demo"})
	if err != nil {
		t.Fatalf("parseHaltArgs: %v", err)
	}
	if name != "demo" || immediately || timeoutMS != 5000 {
		t.Fatalf("unexpected parsed values: %q %v %d", name, immediately, timeoutMS)
	}
}

func TestParseHaltArgsImmediatelyRejectsTimeout(t *testing.T) {
	_, _, _, err := parseHaltArgs([]string{"demo", "--immediately", "--timeout", "1s"})
	if err == nil || !strings.Contains(err.Error(), "--timeout cannot be used with --immediately") {
		t.Fatalf("unexpected error: %v", err)
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

func TestReadPromptTextRejectsOversizeInput(t *testing.T) {
	oversize := strings.Repeat("a", maxPromptInputBytes+1)
	_, err := readPromptText(strings.NewReader(oversize))
	if err == nil || !strings.Contains(err.Error(), "3 MiB limit") {
		t.Fatalf("unexpected error: %v", err)
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
	if !strings.Contains(stderr.String(), "prompt requires --text or --key") {
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
