package service

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oyasmi/agentmux/internal/config"
	"github.com/oyasmi/agentmux/internal/instance"
	"github.com/oyasmi/agentmux/internal/tmuxctl"
)

type fakeTmux struct {
	sessions       map[string]bool
	captureContent string
	paneInfo       tmuxctl.PaneInfo
	loads          []string
	sendKeys       []string
	killed         []string
}

func (f *fakeTmux) HasSession(_ context.Context, sessionID string) bool {
	return f.sessions[sessionID]
}

func (f *fakeTmux) NewSession(_ context.Context, sessionID, _ string, _ string, _ map[string]string) error {
	f.sessions[sessionID] = true
	return nil
}

func (f *fakeTmux) KillSession(_ context.Context, sessionID string) error {
	f.killed = append(f.killed, sessionID)
	delete(f.sessions, sessionID)
	return nil
}

func (f *fakeTmux) CapturePane(context.Context, string, int) (string, error) {
	return f.captureContent, nil
}

func (f *fakeTmux) LoadBuffer(_ context.Context, data string) error {
	f.loads = append(f.loads, data)
	return nil
}

func (f *fakeTmux) PasteBuffer(context.Context, string) error {
	return nil
}

func (f *fakeTmux) SendKeys(_ context.Context, _ string, keys ...string) error {
	f.sendKeys = append(f.sendKeys, keys...)
	return nil
}

func (f *fakeTmux) Attach(string) *exec.Cmd {
	return nil
}

func (f *fakeTmux) PaneInfo(context.Context, string) (tmuxctl.PaneInfo, error) {
	return f.paneInfo, nil
}

func TestListPrunesMissingSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{
		sessions: map[string]bool{"live-session": true},
	})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"live": {
				Name:      "live",
				SessionID: "live-session",
				Status:    instance.StatusIdle,
				UpdatedAt: now,
			},
			"stale": {
				Name:      "stale",
				SessionID: "stale-session",
				Status:    instance.StatusIdle,
				UpdatedAt: now,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	items, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 live instance, got %d", len(items))
	}
	if items[0].Name != "live" {
		t.Fatalf("expected live instance to remain, got %q", items[0].Name)
	}

	saved, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if _, ok := saved.Get("stale"); ok {
		t.Fatalf("stale instance should have been removed from registry")
	}
}

func TestSummonIgnoresStaleInstancesForLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{sessions: map[string]bool{}})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"stale": {
				Name:      "stale",
				SessionID: "stale-session",
				Status:    instance.StatusIdle,
				UpdatedAt: now,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	res, err := svc.Summon(ctx, SummonInput{TemplateName: "worker", Name: "fresh"})
	if err != nil {
		t.Fatalf("summon: %v", err)
	}
	if res.Instance.Name != "fresh" {
		t.Fatalf("expected fresh instance, got %q", res.Instance.Name)
	}

	saved, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if len(saved.Instances) != 1 {
		t.Fatalf("expected only the fresh instance in registry, got %d", len(saved.Instances))
	}
	if _, ok := saved.Get("stale"); ok {
		t.Fatalf("stale instance should have been removed before summon")
	}
}

func TestInspectDowngradesExpiredBusyToIdle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{
		sessions: map[string]bool{"live-session": true},
	})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker": {
				Name:           "worker",
				SessionID:      "live-session",
				Status:         instance.StatusBusy,
				LastActivityAt: now.Add(-11 * time.Second),
				UpdatedAt:      now,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	inst, err := svc.Inspect(ctx, "worker")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inst.Status != instance.StatusIdle {
		t.Fatalf("expected idle after busy ttl expiry, got %s", inst.Status)
	}
}

func TestInspectKeepsBusyBeforeTTLExpires(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{
		sessions: map[string]bool{"live-session": true},
	})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker": {
				Name:           "worker",
				SessionID:      "live-session",
				Status:         instance.StatusBusy,
				LastActivityAt: now.Add(-2 * time.Second),
				UpdatedAt:      now,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	inst, err := svc.Inspect(ctx, "worker")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inst.Status != instance.StatusBusy {
		t.Fatalf("expected busy before ttl expiry, got %s", inst.Status)
	}
}

func TestInspectKeepsBusyWhenBusyTTLIsZero(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{
		sessions: map[string]bool{"live-session": true},
	})
	zero := 0
	svc.Config.Defaults.Status.BusyTTLMS = &zero
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker": {
				Name:           "worker",
				SessionID:      "live-session",
				Status:         instance.StatusBusy,
				LastActivityAt: now.Add(-5 * time.Minute),
				UpdatedAt:      now,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	inst, err := svc.Inspect(ctx, "worker")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inst.Status != instance.StatusBusy {
		t.Fatalf("expected busy when ttl is zero, got %s", inst.Status)
	}
}

func TestSummonReuseReturnsReusedAndSendsPrompt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{sessions: map[string]bool{"live-session": true}}
	svc, registryPath := newTestService(t, tmux)
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker-a": {
				Name:            "worker-a",
				SessionID:       "live-session",
				Status:          instance.StatusIdle,
				UpdatedAt:       now,
				LastActivityAt:  now,
				FirstPromptSent: true,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	res, err := svc.Summon(ctx, SummonInput{TemplateName: "worker", Name: "worker-a", Prompt: strPtr("continue")})
	if err != nil {
		t.Fatalf("summon: %v", err)
	}
	if !res.Reused {
		t.Fatalf("expected reused result")
	}
	if len(tmux.loads) != 1 || tmux.loads[0] != "continue" {
		t.Fatalf("expected prompt to be sent on reuse, got %v", tmux.loads)
	}
}

func TestPromptSendsTextAndEnter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{sessions: map[string]bool{"live-session": true}}
	svc, registryPath := newTestService(t, tmux)
	saveRunningInstance(t, registryPath, "worker", "live-session", instance.StatusIdle, true, time.Now().UTC())

	inst, err := svc.Prompt(ctx, "worker", "fix it", "", true)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if inst.Status != instance.StatusBusy {
		t.Fatalf("expected busy status, got %s", inst.Status)
	}
	if len(tmux.loads) != 1 || tmux.loads[0] != "fix it" {
		t.Fatalf("unexpected load buffer calls: %v", tmux.loads)
	}
	if got := strings.Join(tmux.sendKeys, ","); got != "Enter,Enter" {
		t.Fatalf("unexpected send keys: %s", got)
	}
}

func TestPromptRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{sessions: map[string]bool{"live-session": true}})
	saveRunningInstance(t, registryPath, "worker", "live-session", instance.StatusIdle, true, time.Now().UTC())

	_, err := svc.Prompt(ctx, "worker", "", "BadKey", false)
	if err == nil || !strings.Contains(err.Error(), "unsupported key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCaptureStableMarksIdleAndReturnsContent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{
		sessions:       map[string]bool{"live-session": true},
		captureContent: "screen output",
		paneInfo:       tmuxctl.PaneInfo{Width: 80, Height: 24},
	}
	svc, registryPath := newTestService(t, tmux)
	svc.Config.Defaults.Capture.PollMS = 1
	saveRunningInstance(t, registryPath, "worker", "live-session", instance.StatusBusy, true, time.Now().UTC().Add(-2*time.Second))

	inst, snap, err := svc.Capture(ctx, "worker", 10, 1, 1000)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if inst.Status != instance.StatusIdle {
		t.Fatalf("expected idle, got %s", inst.Status)
	}
	if snap.Content != "screen output" {
		t.Fatalf("unexpected content: %q", snap.Content)
	}
}

func TestWaitOmitsContent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{
		sessions:       map[string]bool{"live-session": true},
		captureContent: "screen output",
		paneInfo:       tmuxctl.PaneInfo{Width: 80, Height: 24},
	}
	svc, registryPath := newTestService(t, tmux)
	svc.Config.Defaults.Capture.PollMS = 1
	saveRunningInstance(t, registryPath, "worker", "live-session", instance.StatusBusy, true, time.Now().UTC().Add(-2*time.Second))

	_, snap, err := svc.Wait(ctx, "worker", 1, 1000)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if snap.Content != "" {
		t.Fatalf("expected empty content for wait, got %q", snap.Content)
	}
}

func TestHaltKillsSessionAndDeletesRegistryEntry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{sessions: map[string]bool{"live-session": true}}
	svc, registryPath := newTestService(t, tmux)
	saveRunningInstance(t, registryPath, "worker", "live-session", instance.StatusIdle, true, time.Now().UTC())

	inst, err := svc.Halt(ctx, "worker")
	if err != nil {
		t.Fatalf("halt: %v", err)
	}
	if inst.Status != instance.StatusExited {
		t.Fatalf("expected exited status, got %s", inst.Status)
	}
	if len(tmux.killed) != 1 || tmux.killed[0] != "live-session" {
		t.Fatalf("unexpected killed sessions: %v", tmux.killed)
	}
	saved, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if _, ok := saved.Get("worker"); ok {
		t.Fatalf("expected worker to be deleted from registry")
	}
}

func TestNewUsesConfiguredTmuxSocket(t *testing.T) {
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Tmux:  config.TmuxDefaults{Socket: "/tmp/custom-agentmux.sock"},
			Shell: "/bin/bash -lc",
		},
		Templates: map[string]config.Template{
			"worker": {Command: "echo test"},
		},
	}

	svc := New(config.Paths{}, cfg)
	client, ok := svc.Tmux.(tmuxctl.Client)
	if !ok {
		t.Fatalf("unexpected tmux client type %T", svc.Tmux)
	}
	if client.Socket != "/tmp/custom-agentmux.sock" {
		t.Fatalf("expected configured socket, got %q", client.Socket)
	}
}

func newTestService(t *testing.T, tmux tmuxClient) (Service, string) {
	t.Helper()

	dir := t.TempDir()
	registryPath := filepath.Join(dir, "instances.json")
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Tmux:         config.TmuxDefaults{Socket: config.DefaultSocketPath},
			Status:       config.StatusDefaults{BusyTTLMS: intPtr(10000)},
			Shell:        "/bin/bash -lc",
			CWD:          dir,
			Env:          map[string]string{},
			Capture:      config.CaptureDefaults{History: 120, StableMS: 1500, PollMS: 250},
			MaxInstances: 1,
		},
		Templates: map[string]config.Template{
			"worker": {
				Command: "echo test",
				Model:   "openai/gpt-5.4",
				CWD:     dir,
				Shell:   "/bin/bash -lc",
				Env:     map[string]string{},
			},
		},
	}

	return Service{
		Paths:  config.Paths{Registry: registryPath},
		Config: cfg,
		Tmux:   tmux,
	}, registryPath
}

func intPtr(v int) *int {
	return &v
}

func strPtr(v string) *string {
	return &v
}

func saveRunningInstance(t *testing.T, registryPath, name, sessionID string, status instance.Status, firstPromptSent bool, lastActivityAt time.Time) {
	t.Helper()

	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			name: {
				Name:            name,
				SessionID:       sessionID,
				Status:          status,
				UpdatedAt:       lastActivityAt,
				LastActivityAt:  lastActivityAt,
				FirstPromptSent: firstPromptSent,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}
}
