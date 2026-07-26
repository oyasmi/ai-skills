package service

import (
	"context"
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
}

type RunResult struct {
	Instance  instance.Instance
	Snapshot  capture.Snapshot
	Reused    bool
	TimedOut  bool
	ElapsedMS int
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
func (s Service) Run(ctx context.Context, in RunInput) (RunResult, error) {
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return RunResult{}, apperr.New("invalid_arguments", "run requires --prompt, --prompt-file, or --stdin")
	}
	started := time.Now()

	summon := in.Summon
	summon.Prompt = &prompt
	res, err := s.Summon(ctx, summon)
	if err != nil {
		return RunResult{}, err
	}
	name := res.Instance.Name

	inst, waitSnap, err := s.Wait(ctx, name, s.Config.Defaults.Capture.StableMS, in.TimeoutMS)
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
	}, nil
}
