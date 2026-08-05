package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/config"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/output"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/service"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/tmuxctl"
)

type e2eFakeTmux struct {
	sessions       map[string]bool
	captureContent string
	paneInfo       tmuxctl.PaneInfo
	// busyTitle is reported once after a prompt is submitted, the way a real
	// TUI harness flips its title to a spinner before settling back to idle.
	busyTitle  string
	promptSent bool
	loads      []string
	sendKeys   []string
	killed     []string
}

func (f *e2eFakeTmux) HasSession(_ context.Context, sessionID string) (bool, error) {
	return f.sessions[sessionID], nil
}

func (f *e2eFakeTmux) NewSession(_ context.Context, sessionID, _ string, _ string, _ map[string]string) error {
	f.sessions[sessionID] = true
	return nil
}

func (f *e2eFakeTmux) KillSession(_ context.Context, sessionID string) error {
	f.killed = append(f.killed, sessionID)
	delete(f.sessions, sessionID)
	return nil
}

func (f *e2eFakeTmux) CapturePane(context.Context, string, int) (string, error) {
	return f.captureContent, nil
}

func (f *e2eFakeTmux) CaptureSnapshot(context.Context, string, int) (tmuxctl.CaptureSnapshot, error) {
	return tmuxctl.CaptureSnapshot{
		Content: f.captureContent,
		Info:    f.paneInfo,
	}, nil
}

func (f *e2eFakeTmux) LoadBuffer(_ context.Context, data string) error {
	f.loads = append(f.loads, data)
	return nil
}

func (f *e2eFakeTmux) PasteBuffer(context.Context, string) error {
	return nil
}

func (f *e2eFakeTmux) SendKeys(_ context.Context, _ string, keys ...string) error {
	f.sendKeys = append(f.sendKeys, keys...)
	for _, key := range keys {
		if key == "Enter" {
			f.promptSent = true
		}
	}
	return nil
}

func (f *e2eFakeTmux) Attach(string) *exec.Cmd {
	return nil
}

func (f *e2eFakeTmux) PaneInfo(context.Context, string) (tmuxctl.PaneInfo, error) {
	if f.promptSent && f.busyTitle != "" {
		f.promptSent = false
		info := f.paneInfo
		info.PaneTitle = f.busyTitle
		return info, nil
	}
	return f.paneInfo, nil
}

func TestRunE2ELifecycleJSON(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	tmux := &e2eFakeTmux{
		sessions:       map[string]bool{},
		captureContent: "ready\n> ",
		paneInfo:       tmuxctl.PaneInfo{Width: 80, Height: 24, PaneTitle: "✳ Ready"},
		busyTitle:      "⠋ Working",
	}
	prevFactory := newService
	newService = func(paths config.Paths, cfg config.Config) service.Service {
		cfg.Defaults.Capture.PollMS = 1
		svc := service.New(paths, cfg)
		svc.Tmux = tmux
		return svc
	}
	t.Cleanup(func() { newService = prevFactory })

	ctx := context.Background()
	runJSON := func(args ...string) (string, string, int) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(ctx, args, &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}

	stdout, stderr, code := runJSON("summon", "--template", "claude-code-tui", "--name", "e2e-agent", "--prompt", "hello", "--json")
	if code != 0 {
		t.Fatalf("summon failed: code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, `"command": "summon"`) || !strings.Contains(stdout, `"instance": "e2e-agent"`) || !strings.Contains(stdout, `"status": "busy"`) {
		t.Fatalf("unexpected summon stdout: %q", stdout)
	}
	if len(tmux.loads) != 1 || !strings.Contains(tmux.loads[0], "hello") {
		t.Fatalf("expected summon prompt to reach tmux, got %v", tmux.loads)
	}

	registryPath := filepath.Join(stateHome, "agentmux", "instances.json")
	reg, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("load registry after summon: %v", err)
	}
	inst, ok := reg.Get("e2e-agent")
	if !ok {
		t.Fatalf("expected e2e-agent in registry")
	}
	if inst.Status != instance.StatusBusy || !inst.FirstPromptSent {
		t.Fatalf("unexpected registry instance after summon: %+v", inst)
	}
	if inst.HarnessType != "claude-code" {
		t.Fatalf("unexpected default harness type for claude-code-tui template: %q", inst.HarnessType)
	}

	// summon observed the harness start working, so the idle title it reports
	// now really is this turn finishing.
	if inst.BusyConfirmedAt.IsZero() {
		t.Fatalf("expected summon --prompt to confirm the harness started: %+v", inst)
	}
	stdout, stderr, code = runJSON("capture", "e2e-agent", "--json")
	if code != 0 {
		t.Fatalf("capture failed: code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, `"command": "capture"`) || !strings.Contains(stdout, `"status": "idle"`) || !strings.Contains(stdout, `"content": "ready\n\u003e "`) || !strings.Contains(stdout, `"pane_title": "✳ Ready"`) {
		t.Fatalf("unexpected capture stdout: %q", stdout)
	}

	stdout, stderr, code = runJSON("inspect", "e2e-agent", "--json")
	if code != 0 {
		t.Fatalf("inspect failed: code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, `"pane_title": "✳ Ready"`) {
		t.Fatalf("unexpected inspect stdout: %q", stdout)
	}

	stdout, stderr, code = runJSON("halt", "e2e-agent", "--immediately", "--json")
	if code != 0 {
		t.Fatalf("halt failed: code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, `"command": "halt"`) || !strings.Contains(stdout, `"status": "exited"`) {
		t.Fatalf("unexpected halt stdout: %q", stdout)
	}
	if len(tmux.killed) != 1 {
		t.Fatalf("expected one killed session, got %v", tmux.killed)
	}

	stdout, stderr, code = runJSON("list", "--json")
	if code != 0 {
		t.Fatalf("list failed: code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, `"instances": []`) {
		t.Fatalf("expected empty instance list after halt, got %q", stdout)
	}

	if _, err := os.Stat(filepath.Join(configHome, "agentmux", "config.yaml")); err != nil {
		t.Fatalf("expected default config to be created: %v", err)
	}
}

// run has to work as one call from argv to payload: summon, prompt, wait, and
// read back, with a single exit code.
func TestRunE2EDelegatesInOneCall(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	tmux := &e2eFakeTmux{
		sessions:       map[string]bool{},
		captureContent: "task finished\n> ",
		paneInfo:       tmuxctl.PaneInfo{Width: 80, Height: 24, PaneTitle: "✳ Ready"},
		busyTitle:      "⠋ Working",
	}
	prevFactory := newService
	newService = func(paths config.Paths, cfg config.Config) service.Service {
		cfg.Defaults.Capture.PollMS = 1
		svc := service.New(paths, cfg)
		svc.Tmux = tmux
		return svc
	}
	t.Cleanup(func() { newService = prevFactory })

	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	registryPath := filepath.Join(stateHome, "agentmux", "instances.json")
	code := Run(ctx, []string{"run", "--template", "claude-code-tui", "--name", "one-shot", "--prompt", "do the task", "--timeout", "5s", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run failed: code=%d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"command": "run"`, `"instance": "one-shot"`, `"reused": false`, `"timed_out": false`, `"content": "task finished`} {
		if !strings.Contains(out, want) {
			t.Fatalf("run output missing %s: %q", want, out)
		}
	}
	if len(tmux.loads) != 1 || !strings.Contains(tmux.loads[0], "do the task") {
		t.Fatalf("expected the task to reach the harness, got %v", tmux.loads)
	}
	stdout.Reset()
	if code := Run(ctx, []string{"list", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("active list failed: %s", stderr.String())
	}
	if strings.Contains(stdout.String(), `"system_prompt"`) || strings.Contains(stdout.String(), `"env"`) || strings.Contains(stdout.String(), "0001-01-01") {
		t.Fatalf("active list JSON leaked internal or zero-value fields: %q", stdout.String())
	}

	// The instance survives the call, and halting it leaves a tombstone that
	// list hides but list --all still reports.
	stdout.Reset()
	if code := Run(ctx, []string{"halt", "one-shot", "--timeout", "20ms", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("halt failed: %s", stderr.String())
	}
	stdout.Reset()
	if code := Run(ctx, []string{"list", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("list failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), `"instances": []`) {
		t.Fatalf("list must hide tombstones: %q", stdout.String())
	}
	stdout.Reset()
	if code := Run(ctx, []string{"list", "--all", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("list --all failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), `"end_reason": "halted"`) {
		t.Fatalf("list --all must report the tombstone: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), `"system_prompt"`) || strings.Contains(stdout.String(), `"env"`) {
		t.Fatalf("list --json must not expose internal prompt or env fields: %q", stdout.String())
	}
	tombstoneReg, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("reload tombstone: %v", err)
	}
	tombstone, ok := tombstoneReg.Get("one-shot")
	if !ok {
		t.Fatal("expected one-shot tombstone")
	}
	if !strings.Contains(stdout.String(), output.LocalTime(tombstone.CreatedAt)) || !strings.Contains(stdout.String(), output.LocalTime(tombstone.EndedAt)) {
		t.Fatalf("list --json must use the local formatted lifecycle timestamps: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(ctx, []string{"wait", "one-shot", "missing", "--mode", "all", "--timeout", "1ms", "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("wait with failed instances must return non-zero, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	var waitResult struct {
		OK   bool `json:"ok"`
		Data struct {
			TimedOut bool     `json:"timed_out"`
			Failed   []string `json:"failed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &waitResult); err != nil {
		t.Fatalf("decode wait result: %v; output=%q", err, stdout.String())
	}
	if waitResult.OK || waitResult.Data.TimedOut || len(waitResult.Data.Failed) != 2 {
		t.Fatalf("unexpected wait failure result: %+v output=%q", waitResult, stdout.String())
	}
}

// Text-mode run must still tell the caller which instance it talked to: the
// content alone does not say whether a generated name was used, and a
// follow-up prompt needs that name.
func TestRunE2ETextModeReportsInstanceOnStderr(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	tmux := &e2eFakeTmux{
		sessions:       map[string]bool{},
		captureContent: "task finished",
		paneInfo:       tmuxctl.PaneInfo{Width: 80, Height: 24, PaneTitle: "✳ Ready"},
		busyTitle:      "⠋ Working",
	}
	prevFactory := newService
	newService = func(paths config.Paths, cfg config.Config) service.Service {
		cfg.Defaults.Capture.PollMS = 1
		svc := service.New(paths, cfg)
		svc.Tmux = tmux
		return svc
	}
	t.Cleanup(func() { newService = prevFactory })

	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"run", "--template", "claude-code-tui", "--name", "text-mode", "--prompt", "do the task", "--timeout", "5s"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run failed: code=%d stderr=%q", code, stderr.String())
	}
	if stdout.String() != "task finished\n" {
		t.Fatalf("expected content with a trailing newline, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "instance: text-mode") {
		t.Fatalf("expected instance name on stderr, got %q", stderr.String())
	}
}

// Without --wait-if-busy, prompt must keep its historical immediate-send
// behavior and report queued_ms: 0 rather than blocking.
func TestRunE2EPromptDefaultDoesNotWaitAndReportsQueuedMS(t *testing.T) {
	stateHome, configHome := setupXDGHome(t)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	tmux := &e2eFakeTmux{
		sessions:       map[string]bool{},
		captureContent: "ready\n> ",
		paneInfo:       tmuxctl.PaneInfo{Width: 80, Height: 24, PaneTitle: "✳ Ready"},
		busyTitle:      "⠋ Working",
	}
	prevFactory := newService
	newService = func(paths config.Paths, cfg config.Config) service.Service {
		cfg.Defaults.Capture.PollMS = 1
		svc := service.New(paths, cfg)
		svc.Tmux = tmux
		return svc
	}
	t.Cleanup(func() { newService = prevFactory })

	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"summon", "--template", "claude-code-tui", "--name", "wait-flag", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("summon failed: code=%d stderr=%q", code, stderr.String())
	}

	stdout.Reset()
	code = Run(ctx, []string{"prompt", "wait-flag", "--text", "hello", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("prompt failed: code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"queued_ms": 0`) {
		t.Fatalf("expected queued_ms 0 without --wait-if-busy, got %q", stdout.String())
	}
}
