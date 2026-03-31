package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oyasmi/agentmux/internal/config"
	"github.com/oyasmi/agentmux/internal/instance"
	"github.com/oyasmi/agentmux/internal/service"
	"github.com/oyasmi/agentmux/internal/tmuxctl"
)

type e2eFakeTmux struct {
	sessions       map[string]bool
	captureContent string
	paneInfo       tmuxctl.PaneInfo
	loads          []string
	sendKeys       []string
	killed         []string
}

func (f *e2eFakeTmux) HasSession(_ context.Context, sessionID string) bool {
	return f.sessions[sessionID]
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
	return nil
}

func (f *e2eFakeTmux) Attach(string) *exec.Cmd {
	return nil
}

func (f *e2eFakeTmux) PaneInfo(context.Context, string) (tmuxctl.PaneInfo, error) {
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

	stdout, stderr, code := runJSON("summon", "--template", "深度编码专家", "--name", "e2e-agent", "--prompt", "hello", "--json")
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
	if inst.HarnessType != "" {
		t.Fatalf("unexpected default harness type for codex template: %q", inst.HarnessType)
	}

	stdout, stderr, code = runJSON("capture", "e2e-agent", "--stable", "1", "--timeout", "1s", "--json")
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
