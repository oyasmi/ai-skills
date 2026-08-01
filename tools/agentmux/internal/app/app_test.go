package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/service"
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
	name, text, key, useStdin, waitIfBusyMS, err := parsePromptArgs([]string{"demo", "--stdin"})
	if err != nil {
		t.Fatalf("parsePromptArgs: %v", err)
	}
	if name != "demo" || text != "" || key != "" || !useStdin || waitIfBusyMS != 0 {
		t.Fatalf("unexpected parsed values: %q %q %q %v %d", name, text, key, useStdin, waitIfBusyMS)
	}
}

func TestParsePromptArgsRejectsTextWithStdin(t *testing.T) {
	_, _, _, _, _, err := parsePromptArgs([]string{"demo", "--stdin", "--text", "hello"})
	if err == nil || !strings.Contains(err.Error(), "--stdin cannot be used with --text") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParsePromptArgsRejectsRemovedEnterFlag(t *testing.T) {
	_, _, _, _, _, err := parsePromptArgs([]string{"demo", "--enter"})
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined: -enter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParsePromptArgsSupportsWaitIfBusy(t *testing.T) {
	_, _, _, _, waitIfBusyMS, err := parsePromptArgs([]string{"demo", "--text", "hi", "--wait-if-busy", "2m"})
	if err != nil {
		t.Fatalf("parsePromptArgs: %v", err)
	}
	if waitIfBusyMS != 120000 {
		t.Fatalf("expected 120000ms, got %d", waitIfBusyMS)
	}

	if _, _, _, _, _, err := parsePromptArgs([]string{"demo", "--text", "hi", "--wait-if-busy", "not-a-duration"}); err == nil {
		t.Fatal("expected an invalid --wait-if-busy value to fail")
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

func TestParseCaptureArgsNewRejectsSince(t *testing.T) {
	name, opts, err := parseCaptureArgs([]string{"demo", "--new"})
	if err != nil {
		t.Fatalf("parseCaptureArgs: %v", err)
	}
	if name != "demo" || !opts.New {
		t.Fatalf("unexpected parsed values: %q %+v", name, opts)
	}

	_, _, err = parseCaptureArgs([]string{"demo", "--new", "--since", "10"})
	if err == nil || !strings.Contains(err.Error(), "--new cannot be combined with --since") {
		t.Fatalf("expected --new and --since together to fail, got %v", err)
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
	if _, _, _, _, _, err := parsePromptArgs([]string{"--text", "hi", "demo"}); err == nil ||
		!strings.Contains(err.Error(), "instance name must come before flags") {
		t.Fatalf("unexpected prompt error: %v", err)
	}
	if _, _, _, _, _, err := parseWaitArgs([]string{"--timeout", "5s", "demo"}); err == nil ||
		!strings.Contains(err.Error(), "instance name must come before flags") {
		t.Fatalf("unexpected wait error: %v", err)
	}
	if _, _, _, err := parseHaltArgs([]string{"--immediately", "demo"}); err == nil ||
		!strings.Contains(err.Error(), "instance name must come before flags") {
		t.Fatalf("unexpected halt error: %v", err)
	}
}

func TestParseWaitArgsDefaults(t *testing.T) {
	names, stableMS, timeoutMS, mode, collect, err := parseWaitArgs([]string{"demo"})
	if err != nil {
		t.Fatalf("parseWaitArgs: %v", err)
	}
	if len(names) != 1 || names[0] != "demo" || stableMS != 1500 || timeoutMS != 30000 || mode != service.WaitAll || collect {
		t.Fatalf("unexpected parsed values: %v %d %d %s %v", names, stableMS, timeoutMS, mode, collect)
	}
}

func TestParseWaitArgsAcceptsSeveralInstances(t *testing.T) {
	names, _, timeoutMS, mode, collect, err := parseWaitArgs([]string{"a", "b", "a", "--mode", "any", "--timeout", "2m", "--collect"})
	if err != nil {
		t.Fatalf("parseWaitArgs: %v", err)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("expected deduplicated names in order, got %v", names)
	}
	if mode != service.WaitAny || timeoutMS != 120000 || !collect {
		t.Fatalf("unexpected mode/timeout/collect: %s %d %v", mode, timeoutMS, collect)
	}

	if _, _, _, _, _, err := parseWaitArgs([]string{"a", "--mode", "first"}); err == nil ||
		!strings.Contains(err.Error(), "must be all or any") {
		t.Fatalf("expected an invalid mode to fail, got %v", err)
	}
	// Names after flags are a mistake worth naming: they would be silently
	// dropped otherwise.
	if _, _, _, _, _, err := parseWaitArgs([]string{"a", "--mode", "any", "b"}); err == nil ||
		!strings.Contains(err.Error(), "list every instance name first") {
		t.Fatalf("expected trailing names to fail, got %v", err)
	}
}

func TestParseRunArgsRequiresPromptAndDefaultsTimeout(t *testing.T) {
	in, useStdin, err := parseRunArgs([]string{"--template", "worker", "--prompt", "do it"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if useStdin || in.Prompt != "do it" || in.TimeoutMS != 300000 {
		t.Fatalf("unexpected run input: stdin=%v prompt=%q timeout=%d", useStdin, in.Prompt, in.TimeoutMS)
	}
	if in.Capture.History != -1 || in.Capture.Raw {
		t.Fatalf("run must inherit capture defaults, got %+v", in.Capture)
	}

	if _, _, err := parseRunArgs([]string{"--template", "worker", "--prompt", "a", "--stdin"}); err == nil {
		t.Fatal("expected --stdin with --prompt to fail")
	}
	if _, _, err := parseRunArgs([]string{"--prompt", "a"}); err == nil ||
		!strings.Contains(err.Error(), "requires --template") {
		t.Fatalf("expected a missing template to fail, got %v", err)
	}
}

func TestParseRunArgsDetach(t *testing.T) {
	in, _, err := parseRunArgs([]string{"--template", "worker", "--prompt", "do it", "--detach"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if !in.Detach {
		t.Fatal("expected Detach to be set")
	}

	for _, flag := range []string{"--history", "--trace", "--raw"} {
		args := []string{"--template", "worker", "--prompt", "do it", "--detach", flag}
		if flag == "--history" {
			args = append(args, "5")
		}
		if _, _, err := parseRunArgs(args); err == nil || !strings.Contains(err.Error(), "--detach cannot be combined") {
			t.Fatalf("expected --detach with %s to fail, got %v", flag, err)
		}
	}
}

func TestParseListArgsAcceptsAll(t *testing.T) {
	includeEnded, err := parseListArgs([]string{"--all"})
	if err != nil || !includeEnded {
		t.Fatalf("parseListArgs --all: %v %v", includeEnded, err)
	}
	if includeEnded, err := parseListArgs(nil); err != nil || includeEnded {
		t.Fatalf("list must hide tombstones by default: %v %v", includeEnded, err)
	}
	if _, err := parseListArgs([]string{"worker"}); err == nil {
		t.Fatal("expected a positional argument to fail")
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

	prevVersion, prevBuildTime := Version, BuildTime
	Version = "v1.2.3"
	BuildTime = "2026-01-01T00:00:00Z"
	t.Cleanup(func() { Version, BuildTime = prevVersion, prevBuildTime })

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"version", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected zero exit code, stderr=%q", stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, `"command": "version"`) || !strings.Contains(got, `"version": "v1.2.3"`) {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if !strings.Contains(got, `"build_time": "2026-01-01T00:00:00Z"`) {
		t.Fatalf("expected build_time in output: %q", got)
	}
	if !strings.Contains(got, `"binary_path"`) {
		t.Fatalf("expected binary_path in output: %q", got)
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
