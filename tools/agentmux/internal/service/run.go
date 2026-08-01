package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
)

// RunInput describes one delegated task: where to run it, what to ask for, and
// how long to stay on it.
type RunInput struct {
	Summon    SummonInput
	Prompt    string
	TimeoutMS int
	Capture   capture.Options
	// Detach sends the prompt and returns immediately, without waiting or
	// capturing: parallel fan-out is "run --detach x N, then wait --collect"
	// instead of summon + prompt spelled out by hand.
	Detach bool
}

type RunResult struct {
	Instance  instance.Instance
	Snapshot  capture.Snapshot
	Reused    bool
	TimedOut  bool
	ElapsedMS int
	// QueuedMS is how much of TimeoutMS was spent waiting for a reused
	// instance to finish earlier, unrelated work before this task's prompt
	// was even sent. 0 means the instance was free immediately.
	QueuedMS int
	// Detached mirrors RunInput.Detach: when true, Snapshot and TimedOut are
	// zero values, since no wait or capture happened.
	Detached bool
	// Warnings carries Summon's non-blocking warnings, such as another active
	// instance already pointed at the same cwd.
	Warnings []string
}

// Run is the whole delegation loop in one call: summon or reuse an instance,
// send the task, wait for it, and read the answer back.
//
// It exists because that sequence is what orchestrating an agent almost always
// means, and stitching it together from four commands makes every caller
// re-implement the same timeout, status, and payload handling.
//
// A timeout is not a failure and does not stop the agent: the instance keeps
// working and the caller can wait on it again, so partial output is returned
// with TimedOut set.
//
// Reusing a busy instance does not send the prompt straight away: it first
// waits, inside the same timeout budget, for the instance's current turn to
// clear. Without that, codex-cli-execjson would reject the prompt outright,
// while claude-code-ndjson and pi-rpc would silently queue it behind
// unrelated work -- and either way a single completion signal afterward
// could not tell the caller which task it belonged to.
func (s Service) Run(ctx context.Context, in RunInput) (RunResult, error) {
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return RunResult{}, apperr.New("invalid_arguments", "run requires --prompt, --prompt-file, or --stdin")
	}
	started := time.Now()

	// The task prompt is sent explicitly below, after any busy wait, so the
	// initial summon must not also fire a template's own default prompt: an
	// empty override replaces it instead of leaving it to fall through.
	summon := in.Summon
	noPrompt := ""
	summon.Prompt = &noPrompt
	res, err := s.Summon(ctx, summon)
	if err != nil {
		return RunResult{}, err
	}
	name := res.Instance.Name

	queuedMS, err := s.WaitIfBusy(ctx, name, in.TimeoutMS)
	if err != nil {
		return RunResult{}, err
	}

	sent, err := s.Prompt(ctx, name, prompt, "")
	if err != nil {
		return RunResult{}, err
	}

	if in.Detach {
		return RunResult{
			Instance:  sent,
			Reused:    res.Reused,
			ElapsedMS: int(time.Since(started).Milliseconds()),
			QueuedMS:  queuedMS,
			Detached:  true,
			Warnings:  res.Warnings,
		}, nil
	}

	remaining := in.TimeoutMS - queuedMS
	if remaining < 0 {
		remaining = 0
	}
	inst, waitSnap, err := s.Wait(ctx, name, s.Config.Defaults.Capture.StableMS, remaining)
	if err != nil {
		return RunResult{}, err
	}

	// Read the result even after a timeout: partial output is what tells the
	// caller whether the agent is making progress or stuck.
	inst, snap, err := s.Capture(ctx, name, in.Capture)
	if err != nil {
		return RunResult{}, err
	}
	return RunResult{
		Instance:  inst,
		Snapshot:  snap,
		Reused:    res.Reused,
		TimedOut:  waitSnap.TimedOut,
		ElapsedMS: int(time.Since(started).Milliseconds()),
		QueuedMS:  queuedMS,
		Warnings:  res.Warnings,
	}, nil
}

// WaitIfBusy blocks, budgeted by budgetMS, until a reused instance's current
// work clears. It is a no-op for an instance that is not busy right now,
// which covers every freshly summoned instance. budgetMS <= 0 means "do not
// wait": the busy check is skipped entirely, matching the historical
// immediate-send behavior of a bare prompt call.
func (s Service) WaitIfBusy(ctx context.Context, name string, budgetMS int) (queuedMS int, err error) {
	if budgetMS <= 0 {
		return 0, nil
	}
	inst, err := s.getInstanceForCapture(ctx, name)
	if err != nil {
		return 0, err
	}
	if inst.Status != instance.StatusBusy {
		return 0, nil
	}
	_, snap, err := s.Wait(ctx, name, s.Config.Defaults.Capture.StableMS, budgetMS)
	if err != nil {
		return 0, err
	}
	if snap.TimedOut {
		return snap.ElapsedMS, apperr.New("instance_busy", fmt.Sprintf("instance %q is still finishing earlier work after %dms; increase the timeout or use a different instance", name, snap.ElapsedMS))
	}
	return snap.ElapsedMS, nil
}
