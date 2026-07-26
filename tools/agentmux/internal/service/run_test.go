package service

import (
	"context"
	"strings"
	"testing"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/config"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/ndjsonctl"
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

func TestRunRequiresAPrompt(t *testing.T) {
	svc := newRunTestService(t)

	_, err := svc.Run(context.Background(), RunInput{Summon: SummonInput{TemplateName: "ndjson"}})
	if err == nil || !strings.Contains(err.Error(), "run requires --prompt") {
		t.Fatalf("expected a missing prompt to fail, got %v", err)
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
