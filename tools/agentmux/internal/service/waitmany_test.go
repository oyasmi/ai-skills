package service

import (
	"context"
	"testing"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/tmuxctl"
)

// Fanning work out is only useful if the orchestrator can act on whichever
// shard lands first instead of blocking on the slowest one.
func TestWaitManyAnyReturnsOnFirstCompletion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{
		sessions: map[string]bool{"fast-session": true, "slow-session": true},
		paneInfo: tmuxctl.PaneInfo{Width: 80, Height: 24},
		paneTitleFor: map[string]string{
			"fast-session:0.0": "✳ Ready",
			"slow-session:0.0": "⠋ Working",
		},
	}
	svc, registryPath := newTestService(t, tmux)
	svc.Config.Defaults.Capture.PollMS = 1
	seedTwoWorkers(t, registryPath)

	started := time.Now()
	outcomes, ok, err := svc.WaitMany(ctx, []string{"fast", "slow"}, 1500, 5000, WaitAny)
	if err != nil {
		t.Fatalf("wait many: %v", err)
	}
	if !ok {
		t.Fatal("expected mode=any to be satisfied by the finished instance")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("mode=any must not block on the slow instance, took %s", elapsed)
	}
	if !outcomes[0].Done || outcomes[0].Name != "fast" {
		t.Fatalf("expected fast to be done: %+v", outcomes[0])
	}
	if outcomes[1].Done {
		t.Fatal("the still-working instance must not be reported as done")
	}
	// A cut-short wait still owes the caller a status.
	if outcomes[1].Instance.Status != instance.StatusBusy {
		t.Fatalf("expected the pending instance status, got %q", outcomes[1].Instance.Status)
	}
}

// mode=all reports each instance separately: the caller needs to know which
// shards are still running, not just that something is unfinished.
func TestWaitManyAllReportsPendingInstances(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{
		sessions: map[string]bool{"fast-session": true, "slow-session": true},
		paneInfo: tmuxctl.PaneInfo{Width: 80, Height: 24},
		paneTitleFor: map[string]string{
			"fast-session:0.0": "✳ Ready",
			"slow-session:0.0": "⠋ Working",
		},
	}
	svc, registryPath := newTestService(t, tmux)
	svc.Config.Defaults.Capture.PollMS = 1
	seedTwoWorkers(t, registryPath)

	outcomes, ok, err := svc.WaitMany(ctx, []string{"fast", "slow"}, 1500, 100, WaitAll)
	if err != nil {
		t.Fatalf("wait many: %v", err)
	}
	if ok {
		t.Fatal("mode=all must not be satisfied while one instance still works")
	}
	if !outcomes[0].Done || outcomes[1].Done {
		t.Fatalf("unexpected completion split: %+v", outcomes)
	}
	if !outcomes[1].Snapshot.TimedOut {
		t.Fatal("expected the unfinished instance to report its timeout")
	}
}

// One bad name must not discard the results of the others.
func TestWaitManyReportsPerInstanceFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmux := &fakeTmux{
		sessions:     map[string]bool{"fast-session": true},
		paneInfo:     tmuxctl.PaneInfo{Width: 80, Height: 24},
		paneTitleFor: map[string]string{"fast-session:0.0": "✳ Ready"},
	}
	svc, registryPath := newTestService(t, tmux)
	svc.Config.Defaults.Capture.PollMS = 1
	saveRunningInstance(t, registryPath, "fast", "fast-session", instance.StatusBusy, true, time.Now().UTC().Add(-5*time.Second))
	setHarnessType(t, registryPath, "fast", "claude-code")

	outcomes, ok, err := svc.WaitMany(ctx, []string{"fast", "ghost"}, 1500, 100, WaitAll)
	if err != nil {
		t.Fatalf("wait many: %v", err)
	}
	if ok {
		t.Fatal("a missing instance cannot satisfy mode=all")
	}
	if !outcomes[0].Done {
		t.Fatalf("the healthy instance must still be waited on: %+v", outcomes[0])
	}
	if outcomes[1].ErrorCode != "instance_not_found" {
		t.Fatalf("expected instance_not_found for the missing name, got %q", outcomes[1].ErrorCode)
	}
}

func seedTwoWorkers(t *testing.T, registryPath string) {
	t.Helper()

	past := time.Now().UTC().Add(-5 * time.Second)
	reg := instance.Registry{Instances: map[string]instance.Instance{
		"fast": {
			Name: "fast", SessionID: "fast-session", HarnessType: "claude-code",
			Status: instance.StatusBusy, UpdatedAt: past, LastActivityAt: past, FirstPromptSent: true,
		},
		"slow": {
			Name: "slow", SessionID: "slow-session", HarnessType: "claude-code",
			Status: instance.StatusBusy, UpdatedAt: past, LastActivityAt: past, FirstPromptSent: true,
		},
	}}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}
}
