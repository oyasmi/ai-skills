package service

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
	"github.com/oyasmi/agentmux/internal/config"
	"github.com/oyasmi/agentmux/internal/instance"
	"github.com/oyasmi/agentmux/internal/tmuxctl"
)

type fakeTmux struct {
	sessions             map[string]bool
	captureContent       string
	captureCalls         int
	captureSnapshotCalls int
	hasSessionCalls      []string
	paneInfoCalls        []string
	paneInfo             tmuxctl.PaneInfo
	loads                []string
	sendKeys             []string
	killed               []string
	sendKeysHook         func(*fakeTmux, string, []string)
}

func (f *fakeTmux) HasSession(_ context.Context, sessionID string) bool {
	f.hasSessionCalls = append(f.hasSessionCalls, sessionID)
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
	f.captureCalls++
	return f.captureContent, nil
}

func (f *fakeTmux) CaptureSnapshot(context.Context, string, int) (tmuxctl.CaptureSnapshot, error) {
	f.captureSnapshotCalls++
	return tmuxctl.CaptureSnapshot{
		Content: f.captureContent,
		Info:    f.paneInfo,
	}, nil
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
	if f.sendKeysHook != nil {
		f.sendKeysHook(f, "", keys)
	}
	return nil
}

func (f *fakeTmux) Attach(string) *exec.Cmd {
	return nil
}

func (f *fakeTmux) PaneInfo(context.Context, string) (tmuxctl.PaneInfo, error) {
	f.paneInfoCalls = append(f.paneInfoCalls, "pane")
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

func TestInspectClaudeCodeBusyTurnsIdleWhenPaneTitleShowsIdle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{
		sessions: map[string]bool{"live-session": true},
		paneInfo: tmuxctl.PaneInfo{PaneTitle: "✳ Ready"},
	})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker": {
				Name:           "worker",
				SessionID:      "live-session",
				Status:         instance.StatusBusy,
				HarnessType:    "claude-code",
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
	if inst.Status != instance.StatusIdle {
		t.Fatalf("expected idle from pane title, got %s", inst.Status)
	}
	if inst.PaneTitle != "✳ Ready" {
		t.Fatalf("expected pane title to persist, got %q", inst.PaneTitle)
	}
}

func TestInspectClaudeCodeBusyStaysBusyWhenPaneTitleShowsSpinner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{
		sessions: map[string]bool{"live-session": true},
		paneInfo: tmuxctl.PaneInfo{PaneTitle: "⠋ Thinking"},
	})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker": {
				Name:           "worker",
				SessionID:      "live-session",
				Status:         instance.StatusBusy,
				HarnessType:    "claude-code",
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
	if inst.PaneTitle != "⠋ Thinking" {
		t.Fatalf("expected pane title to persist, got %q", inst.PaneTitle)
	}
}

func TestInspectClaudeCodeIgnoresExpiredTTLWhenPaneTitleShowsSpinner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{
		sessions: map[string]bool{"live-session": true},
		paneInfo: tmuxctl.PaneInfo{PaneTitle: "⠋ Thinking"},
	})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker": {
				Name:           "worker",
				SessionID:      "live-session",
				Status:         instance.StatusBusy,
				HarnessType:    "claude-code",
				LastActivityAt: now.Add(-2 * time.Minute),
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
		t.Fatalf("expected busy despite expired ttl, got %s", inst.Status)
	}
}

func TestInspectClaudeCodeIgnoresExpiredTTLWhenPaneTitleIsUnknown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{
		sessions: map[string]bool{"live-session": true},
		paneInfo: tmuxctl.PaneInfo{PaneTitle: "Working on task"},
	})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker": {
				Name:           "worker",
				SessionID:      "live-session",
				Status:         instance.StatusBusy,
				HarnessType:    "claude-code",
				LastActivityAt: now.Add(-2 * time.Minute),
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
		t.Fatalf("expected busy despite expired ttl and unknown title, got %s", inst.Status)
	}
}

func TestInspectUnknownHarnessStillUsesTTLInsteadOfPaneTitle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{
		sessions: map[string]bool{"live-session": true},
		paneInfo: tmuxctl.PaneInfo{PaneTitle: "✳ Ready"},
	})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker": {
				Name:           "worker",
				SessionID:      "live-session",
				Status:         instance.StatusBusy,
				HarnessType:    "unknown",
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
		t.Fatalf("expected busy when harness is unknown, got %s", inst.Status)
	}
}

func TestClaudeCodeTitleIsIdleSupportsWrappedMarker(t *testing.T) {
	t.Parallel()

	if !claudeCodeTitleIsIdle(" [ ✳ ] Ready") {
		t.Fatalf("expected wrapped idle marker to be accepted")
	}
}

func TestClaudeCodeTitleIsIdleRejectsSpinnerMarker(t *testing.T) {
	t.Parallel()

	if claudeCodeTitleIsIdle(" (⠋) Thinking") {
		t.Fatalf("expected spinner marker to stay non-idle")
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
				Template:        "worker",
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

func TestSummonRejectsReuseAcrossTemplates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{sessions: map[string]bool{"live-session": true}}
	svc, registryPath := newTestService(t, tmux)
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker-a": {
				Name:      "worker-a",
				Template:  "other-template",
				SessionID: "live-session",
				Status:    instance.StatusIdle,
				UpdatedAt: now,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	_, err := svc.Summon(ctx, SummonInput{TemplateName: "worker", Name: "worker-a"})
	if err == nil || !strings.Contains(err.Error(), "already exists with template") {
		t.Fatalf("unexpected error: %v", err)
	}
	if code := apperr.Code(err); code != "instance_template_mismatch" {
		t.Fatalf("unexpected error code: %s", code)
	}
}

func TestInspectReconcilesOnlyTargetInstance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{sessions: map[string]bool{"live-session": true}}
	svc, registryPath := newTestService(t, tmux)
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"worker": {
				Name:      "worker",
				SessionID: "live-session",
				Status:    instance.StatusIdle,
				UpdatedAt: now,
			},
			"other": {
				Name:      "other",
				SessionID: "other-session",
				Status:    instance.StatusIdle,
				UpdatedAt: now,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	_, err := svc.Inspect(ctx, "worker")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if got := strings.Join(tmux.hasSessionCalls, ","); got != "live-session" {
		t.Fatalf("expected only target session to be reconciled, got %q", got)
	}

	saved, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if _, ok := saved.Get("other"); !ok {
		t.Fatalf("expected unrelated instance to remain untouched")
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

	inst, snap, err := svc.Capture(ctx, "worker", 10)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if inst.Status != instance.StatusBusy {
		t.Fatalf("expected busy, got %s", inst.Status)
	}
	if snap.Content != "screen output" {
		t.Fatalf("unexpected content: %q", snap.Content)
	}
	if tmux.captureSnapshotCalls == 0 {
		t.Fatalf("expected capture to use combined snapshot sampling")
	}
	if tmux.captureCalls != 0 {
		t.Fatalf("expected capture to avoid standalone capture-pane calls, got %d", tmux.captureCalls)
	}
	saved, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	current, ok := saved.Get("worker")
	if !ok {
		t.Fatalf("expected worker to remain in registry")
	}
	if current.Status != instance.StatusBusy {
		t.Fatalf("expected capture to preserve busy status, got %s", current.Status)
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
	if tmux.captureSnapshotCalls == 0 {
		t.Fatalf("expected wait to use combined snapshot sampling")
	}
	if tmux.captureCalls != 0 {
		t.Fatalf("expected wait to avoid standalone capture-pane calls, got %d", tmux.captureCalls)
	}
}

func TestWaitClaudeCodeReturnsEarlyFromPaneTitle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{
		sessions:       map[string]bool{"live-session": true},
		captureContent: "screen output",
		paneInfo:       tmuxctl.PaneInfo{Width: 80, Height: 24, PaneTitle: "✳ Ready"},
	}
	svc, registryPath := newTestService(t, tmux)
	saveRunningInstance(t, registryPath, "worker", "live-session", instance.StatusBusy, true, time.Now().UTC())

	reg, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	inst := reg.Instances["worker"]
	inst.HarnessType = "claude-code"
	reg.Put(inst)
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	inst, snap, err := svc.Wait(ctx, "worker", 1500, 1000)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if inst.Status != instance.StatusIdle {
		t.Fatalf("expected idle, got %s", inst.Status)
	}
	if snap.PaneTitle != "✳ Ready" {
		t.Fatalf("expected pane title in snapshot, got %q", snap.PaneTitle)
	}
	if snap.StableForMS != 0 {
		t.Fatalf("expected early return before stability accounting, got %d", snap.StableForMS)
	}
	if tmux.captureCalls != 0 {
		t.Fatalf("expected claude-code wait to avoid capture-pane, got %d calls", tmux.captureCalls)
	}
}

func TestHaltGracefullySendsDoubleInterruptBeforeKilling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{sessions: map[string]bool{"live-session": true}}
	svc, registryPath := newTestService(t, tmux)
	saveRunningInstance(t, registryPath, "worker", "live-session", instance.StatusIdle, true, time.Now().UTC())

	inst, err := svc.HaltWithOptions(ctx, "worker", false, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("halt: %v", err)
	}
	if inst.Status != instance.StatusExited {
		t.Fatalf("expected exited status, got %s", inst.Status)
	}
	if got := strings.Join(tmux.sendKeys, ","); got != "C-c,C-c" {
		t.Fatalf("expected double interrupt before kill, got %s", got)
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

func TestHaltGracefullyStopsWithoutKillWhenSecondInterruptEndsSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{
		sessions: map[string]bool{"live-session": true},
		sendKeysHook: func(f *fakeTmux, _ string, keys []string) {
			if len(keys) == 1 && keys[0] == "C-c" && len(f.sendKeys) == 2 {
				delete(f.sessions, "live-session")
			}
		},
	}
	svc, registryPath := newTestService(t, tmux)
	saveRunningInstance(t, registryPath, "worker", "live-session", instance.StatusIdle, true, time.Now().UTC())

	inst, err := svc.HaltWithOptions(ctx, "worker", false, 700*time.Millisecond)
	if err != nil {
		t.Fatalf("halt: %v", err)
	}
	if inst.Status != instance.StatusExited {
		t.Fatalf("expected exited status, got %s", inst.Status)
	}
	if got := strings.Join(tmux.sendKeys, ","); got != "C-c,C-c" {
		t.Fatalf("expected two interrupts, got %s", got)
	}
	if len(tmux.killed) != 0 {
		t.Fatalf("expected no force kill, got %v", tmux.killed)
	}
	if _, ok := tmux.sessions["live-session"]; ok {
		t.Fatalf("expected session to be gone")
	}
	if saved, err := instance.Load(registryPath); err != nil {
		t.Fatalf("reload registry: %v", err)
	} else if _, ok := saved.Get("worker"); ok {
		t.Fatalf("expected worker to be deleted from registry")
	}
}

func TestHaltImmediatelyKillsSessionWithoutInterrupts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{sessions: map[string]bool{"live-session": true}}
	svc, registryPath := newTestService(t, tmux)
	saveRunningInstance(t, registryPath, "worker", "live-session", instance.StatusIdle, true, time.Now().UTC())

	inst, err := svc.HaltWithOptions(ctx, "worker", true, 0)
	if err != nil {
		t.Fatalf("halt: %v", err)
	}
	if inst.Status != instance.StatusExited {
		t.Fatalf("expected exited status, got %s", inst.Status)
	}
	if len(tmux.sendKeys) != 0 {
		t.Fatalf("expected no interrupts, got %v", tmux.sendKeys)
	}
	if len(tmux.killed) != 1 || tmux.killed[0] != "live-session" {
		t.Fatalf("unexpected killed sessions: %v", tmux.killed)
	}
}

func TestNewUsesConfiguredTmuxSocket(t *testing.T) {
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Tmux:  config.TmuxDefaults{Socket: "/tmp/custom-agentmux.sock", LoadUserConfig: true},
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
	if !client.LoadUserConfig {
		t.Fatalf("expected load_user_config to be propagated")
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
