package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/config"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/ndjsonctl"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/tmuxctl"
)

// The whole point of run is that delegating a task and reading the answer is
// one call instead of four.
func TestRunDelegatesATaskAndReturnsTheAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	ctx := context.Background()
	svc := newRunTestService(t)

	res, err := svc.Run(ctx, RunInput{
		Summon:    SummonInput{TemplateName: "ndjson", Name: "runner"},
		Prompt:    "summarize the repo",
		TimeoutMS: 5000,
		Capture:   capture.Options{History: -1, Scope: capture.ScopeCurrent},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TimedOut {
		t.Fatal("the fake harness answers immediately; run must not report a timeout")
	}
	if res.Reused {
		t.Fatal("the first run creates the instance")
	}
	if res.Snapshot.Content != "service done" {
		t.Fatalf("expected the agent's answer, got %q", res.Snapshot.Content)
	}
	if res.Instance.Status != instance.StatusIdle {
		t.Fatalf("expected an idle instance after run, got %s", res.Instance.Status)
	}
	if res.ElapsedMS <= 0 {
		t.Fatalf("expected run to report its duration, got %d", res.ElapsedMS)
	}

	// The instance survives, so a follow-up run continues the same session.
	again, err := svc.Run(ctx, RunInput{
		Summon:    SummonInput{TemplateName: "ndjson", Name: "runner"},
		Prompt:    "and now the tests",
		TimeoutMS: 5000,
		Capture:   capture.Options{History: -1, Scope: capture.ScopeCurrent},
	})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !again.Reused {
		t.Fatal("a second run against the same name must reuse the instance")
	}
	if err := svc.NDJSON.Halt(ctx, again.Instance, true, 0); err != nil {
		t.Fatalf("halt: %v", err)
	}
}

// --detach is what makes a fan-out cheap: send the prompt and come straight
// back, instead of paying the full turn's wait time before the next summon.
func TestRunDetachReturnsImmediatelyWithoutWaitingOrCapturing(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	ctx := context.Background()
	svc := newRunTestService(t)

	started := time.Now()
	res, err := svc.Run(ctx, RunInput{
		Summon:    SummonInput{TemplateName: "ndjson", Name: "detached"},
		Prompt:    "summarize the repo",
		TimeoutMS: 5000,
		Detach:    true,
	})
	if err != nil {
		t.Fatalf("run --detach: %v", err)
	}
	if !res.Detached {
		t.Fatal("expected Detached to be set")
	}
	if res.Snapshot.Content != "" {
		t.Fatalf("expected no capture to have happened, got content %q", res.Snapshot.Content)
	}
	if res.TimedOut {
		t.Fatal("TimedOut is meaningless without a wait; must stay false")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("run --detach must return before the task finishes, took %s", elapsed)
	}
	if res.Instance.Status != instance.StatusBusy {
		t.Fatalf("expected the instance to be busy right after the detached prompt, got %s", res.Instance.Status)
	}

	if _, _, err := svc.Wait(ctx, "detached", 0, 5000); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if err := svc.NDJSON.Halt(ctx, res.Instance, true, 0); err != nil {
		t.Fatalf("halt: %v", err)
	}
}

func TestRunRequiresAPrompt(t *testing.T) {
	svc := newRunTestService(t)

	_, err := svc.Run(context.Background(), RunInput{Summon: SummonInput{TemplateName: "ndjson"}})
	if err == nil || !strings.Contains(err.Error(), "run requires --prompt") {
		t.Fatalf("expected a missing prompt to fail, got %v", err)
	}
}

// A busy reused instance must not receive the new task's prompt until its
// current work clears: run waits for that, inside its own timeout budget,
// instead of racing the new prompt into a screen that is still mid-turn.
func TestRunWaitsForBusyReusedInstanceBeforeSendingNewPrompt(t *testing.T) {
	tmux := &fakeTmux{
		sessions: map[string]bool{"sess-1": true},
		paneInfo: tmuxctl.PaneInfo{Width: 80, Height: 24},
		// The first two polls still see the previous turn's busy marker; the
		// third sees it go idle. Only after that must the new prompt land.
		paneTitles: []string{"⠋ old work", "⠋ old work", "✳ Ready"},
	}
	svc, registryPath := newTestService(t, tmux)
	svc.Config.Defaults.Capture.PollMS = 1
	svc.Config.Defaults.Capture.StableMS = 1
	svc.Config.Defaults.Status.PromptAckMS = intPtr(1)
	tpl := svc.Config.Templates["worker"]
	tpl.HarnessType = claudeCodeHarnessType
	svc.Config.Templates["worker"] = tpl

	now := time.Now().UTC()
	reg := instance.Registry{Instances: map[string]instance.Instance{
		"worker-A": {
			Name:            "worker-A",
			Template:        "worker",
			SessionID:       "sess-1",
			HarnessType:     claudeCodeHarnessType,
			Status:          instance.StatusBusy,
			CreatedAt:       now,
			UpdatedAt:       now,
			LastActivityAt:  now,
			BusyConfirmedAt: now,
			FirstPromptSent: true,
		},
	}}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	res, err := svc.Run(context.Background(), RunInput{
		Summon:    SummonInput{TemplateName: "worker", Name: "worker-A"},
		Prompt:    "next task",
		TimeoutMS: 5000,
		Capture:   capture.Options{History: -1, Scope: capture.ScopeCurrent},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Reused {
		t.Fatal("expected the existing busy instance to be reused")
	}
	if len(tmux.loads) != 1 || !strings.Contains(tmux.loads[0], "next task") {
		t.Fatalf("expected exactly one load carrying the new task, got %v", tmux.loads)
	}
}

// Exhausting the timeout while the reused instance is still busy must fail
// clearly instead of sending the new task into whatever is still running.
func TestRunFailsWhenBusyInstanceNeverClears(t *testing.T) {
	tmux := &fakeTmux{
		sessions:   map[string]bool{"sess-1": true},
		paneInfo:   tmuxctl.PaneInfo{Width: 80, Height: 24},
		paneTitles: []string{"⠋ still working"},
	}
	svc, registryPath := newTestService(t, tmux)
	svc.Config.Defaults.Capture.PollMS = 1
	tpl := svc.Config.Templates["worker"]
	tpl.HarnessType = claudeCodeHarnessType
	svc.Config.Templates["worker"] = tpl

	now := time.Now().UTC()
	reg := instance.Registry{Instances: map[string]instance.Instance{
		"worker-A": {
			Name:            "worker-A",
			Template:        "worker",
			SessionID:       "sess-1",
			HarnessType:     claudeCodeHarnessType,
			Status:          instance.StatusBusy,
			CreatedAt:       now,
			UpdatedAt:       now,
			LastActivityAt:  now,
			BusyConfirmedAt: now,
			FirstPromptSent: true,
		},
	}}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	_, err := svc.Run(context.Background(), RunInput{
		Summon:    SummonInput{TemplateName: "worker", Name: "worker-A"},
		Prompt:    "next task",
		TimeoutMS: 20,
		Capture:   capture.Options{History: -1, Scope: capture.ScopeCurrent},
	})
	if err == nil {
		t.Fatal("expected run to fail when the reused instance never clears")
	}
	if apperr.Code(err) != "instance_busy" {
		t.Fatalf("expected instance_busy, got %v", err)
	}
	if len(tmux.loads) != 0 {
		t.Fatalf("expected no prompt to be sent while the instance stayed busy, got %v", tmux.loads)
	}
}

func newRunTestService(t *testing.T) Service {
	t.Helper()

	svc, _ := newTestService(t, &fakeTmux{sessions: map[string]bool{}})
	fake := writeServiceFakeClaude(t, svc.Paths.StateDir)
	svc.Config.Templates["ndjson"] = config.Template{
		Command:     fake,
		HarnessType: ndjsonctl.HarnessType,
		CWD:         svc.Paths.StateDir,
		Shell:       "/bin/bash -lc",
		Env:         map[string]string{},
	}
	return svc
}
