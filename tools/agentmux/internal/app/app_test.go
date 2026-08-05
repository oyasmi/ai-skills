package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/output"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/service"
)

func TestSummarizeDescriptionKeepsUsefulChineseContext(t *testing.T) {
	got := summarizeDescription("方案负责人（高成本）。\n负责复杂方案拆解、风险识别和验收标准设计。\n\n后续段落。")
	if !strings.Contains(got, "负责复杂方案拆解") {
		t.Fatalf("summary lost the second sentence: %q", got)
	}
	if strings.Contains(got, "。 ") {
		t.Fatalf("summary inserted a space after Chinese punctuation: %q", got)
	}
	if strings.Contains(got, "...") || strings.Contains(got, "…") {
		t.Fatalf("summary should leave truncation to the table renderer: %q", got)
	}
}

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
	if got := stdout.String(); !strings.Contains(got, "Name") || !strings.Contains(got, "Model") || !strings.Contains(got, "Harness") || !strings.Contains(got, "claude-code") {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestTemplateListDoesNotInitializeState(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"template", "list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("template list failed: code=%d stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(configHome, "agentmux", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("template list unexpectedly wrote config: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(stateHome, "agentmux")); !os.IsNotExist(err) {
		t.Fatalf("template list unexpectedly created state directory: err=%v", err)
	}
}

func TestRunInvalidFlagUsesInvalidArgumentsInJSON(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"list", "--bogus", "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected invalid flag to fail")
	}
	got := stdout.String()
	if !strings.Contains(got, `"error_code": "invalid_arguments"`) || strings.Contains(got, "Usage:") {
		t.Fatalf("unexpected JSON error: %q", got)
	}
}

func TestRunRejectsInspectExtraArguments(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"inspect", "demo", "extra", "--json"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stdout.String(), `"error_code": "invalid_arguments"`) {
		t.Fatalf("expected strict inspect arity, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunRejectsJSONForAttach(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"attach", "--json"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stdout.String(), `"error_code": "invalid_arguments"`) {
		t.Fatalf("expected attach --json to fail explicitly, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunUnknownHelpTopicFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"help", "nope"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "unknown help topic") {
		t.Fatalf("expected unknown help topic to fail, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
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
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --enter") {
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

func TestParseCaptureArgsRejectsUnknownFlags(t *testing.T) {
	name, opts, err := parseCaptureArgs([]string{"demo", "--history", "120"})
	if err != nil {
		t.Fatalf("parseCaptureArgs: %v", err)
	}
	if name != "demo" || opts.History != 120 || opts.Scope != "current" {
		t.Fatalf("unexpected parsed values: %q %+v", name, opts)
	}

	_, _, err = parseCaptureArgs([]string{"demo", "--stable=1500"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --stable") {
		t.Fatalf("expected unknown stable flag to fail, got %v", err)
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

func TestParseArgsAcceptsFlagsBeforeInstanceName(t *testing.T) {
	name, opts, err := parseCaptureArgs([]string{"--history", "40", "demo"})
	if err != nil || name != "demo" || opts.History != 40 {
		t.Fatalf("unexpected capture result: name=%q opts=%+v err=%v", name, opts, err)
	}
	name, text, _, _, _, err := parsePromptArgs([]string{"--text", "hi", "demo"})
	if err != nil || name != "demo" || text != "hi" {
		t.Fatalf("unexpected prompt result: name=%q text=%q err=%v", name, text, err)
	}
	names, _, timeout, _, _, err := parseWaitArgs([]string{"--timeout", "5s", "demo"})
	if err != nil || len(names) != 1 || names[0] != "demo" || timeout != 5000 {
		t.Fatalf("unexpected wait result: names=%v timeout=%d err=%v", names, timeout, err)
	}
	name, immediately, _, err := parseHaltArgs([]string{"--immediately", "demo"})
	if err != nil || name != "demo" || !immediately {
		t.Fatalf("unexpected halt result: name=%q immediately=%v err=%v", name, immediately, err)
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
	names, _, _, mode, _, err = parseWaitArgs([]string{"a", "--mode", "any", "b"})
	if err != nil || len(names) != 2 || mode != service.WaitAny {
		t.Fatalf("expected names and flags to be accepted in either order, names=%v mode=%s err=%v", names, mode, err)
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

func TestParseEffortOverride(t *testing.T) {
	in, err := parseSummonArgs([]string{"--template", "builder", "--effort", "xhigh"})
	if err != nil {
		t.Fatalf("parseSummonArgs: %v", err)
	}
	if in.Effort == nil || *in.Effort != "xhigh" {
		t.Fatalf("expected --effort to reach the summon input, got %v", in.Effort)
	}
	equalsIn, err := parseSummonArgs([]string{"--template=builder", "--effort=xhigh"})
	if err != nil || equalsIn.TemplateName != "builder" || equalsIn.Effort == nil || *equalsIn.Effort != "xhigh" {
		t.Fatalf("expected --flag=value summon syntax, input=%+v err=%v", equalsIn, err)
	}

	// run forwards every summon flag, so a role's strength is adjustable in the
	// one-shot path too.
	runIn, _, err := parseRunArgs([]string{"--template", "builder", "--prompt", "x", "--effort", "max", "--model", "opus"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if runIn.Summon.Effort == nil || *runIn.Summon.Effort != "max" {
		t.Fatalf("expected run to forward --effort, got %v", runIn.Summon.Effort)
	}
	if runIn.Summon.Model == nil || *runIn.Summon.Model != "opus" {
		t.Fatalf("expected run to forward --model, got %v", runIn.Summon.Model)
	}
	equalsRun, _, err := parseRunArgs([]string{"--template=builder", "--prompt=x", "--detach=true"})
	if err != nil || equalsRun.Summon.TemplateName != "builder" || !equalsRun.Detach {
		t.Fatalf("expected --flag=value run syntax, input=%+v err=%v", equalsRun, err)
	}

	// An unrecognised level would otherwise fall through to the harness default
	// and read as a working override.
	_, err = parseSummonArgs([]string{"--template", "builder", "--effort", "ultra"})
	if err == nil || !strings.Contains(err.Error(), "must be one of") {
		t.Fatalf("expected an unknown effort to be rejected with the valid list, got %v", err)
	}
	if _, err := parseSummonArgs([]string{"--template", "builder", "--effort"}); err == nil {
		t.Fatal("expected a missing --effort value to fail")
	}
	if _, err := parseSummonArgs([]string{"worker"}); err == nil || !strings.Contains(err.Error(), "unexpected argument: worker") {
		t.Fatalf("expected a positional summon argument to be diagnosed clearly, got %v", err)
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
	expectedBuildTime := output.LocalizeTimestamp("2026-01-01T00:00:00Z")
	if !strings.Contains(got, `"build_time": "`+expectedBuildTime+`"`) {
		t.Fatalf("expected local build_time %q in output: %q", expectedBuildTime, got)
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
