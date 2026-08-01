package service

import (
	"context"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
)

// WaitMode selects when a multi-instance wait is satisfied.
type WaitMode string

const (
	// WaitAll returns once every instance has finished, or the deadline passes.
	WaitAll WaitMode = "all"
	// WaitAny returns as soon as one instance finishes. This is what makes a
	// fan-out usable: the orchestrator handles whichever shard lands first
	// instead of blocking on the slowest one.
	WaitAny WaitMode = "any"
)

// WaitOutcome is one instance's result inside a multi-instance wait.
type WaitOutcome struct {
	Name      string
	Instance  instance.Instance
	Snapshot  capture.Snapshot
	Done      bool
	ErrorCode string
	Error     string
}

// WaitMany waits on several instances at once, and when collect is true,
// also reads back a lean capture for every instance that finished -- so a
// fan-out can be "summon x N, then wait --collect" instead of a wait
// followed by one capture call per instance.
//
// One bad name does not discard the other results: a failure is reported
// against the instance it belongs to and the rest still return normally. A
// collect failure on an otherwise-successful wait is reported the same way,
// against that instance alone.
func (s Service) WaitMany(ctx context.Context, names []string, stableMS, timeoutMS int, mode WaitMode, collect bool) ([]WaitOutcome, bool, error) {
	if len(names) == 0 {
		return nil, false, apperr.New("invalid_arguments", "wait requires at least one instance name")
	}
	if mode != WaitAny {
		mode = WaitAll
	}

	type result struct {
		idx  int
		inst instance.Instance
		snap capture.Snapshot
		err  error
	}
	// Cancelling stops the still-running waits once WaitAny is satisfied; they
	// are reported as unfinished, which is exactly what they are.
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan result, len(names))
	for i, name := range names {
		go func(i int, name string) {
			inst, snap, err := s.Wait(childCtx, name, stableMS, timeoutMS)
			ch <- result{idx: i, inst: inst, snap: snap, err: err}
		}(i, name)
	}

	outcomes := make([]WaitOutcome, len(names))
	for i, name := range names {
		outcomes[i] = WaitOutcome{Name: name}
	}
	for range names {
		r := <-ch
		out := &outcomes[r.idx]
		switch {
		case r.err == nil:
			out.Instance = r.inst
			out.Snapshot = r.snap
			out.Done = !r.snap.TimedOut
		case childCtx.Err() != nil:
			// Cancelled by a satisfied WaitAny, or by the caller: this instance
			// simply was not waited to completion, which is not an error.
		default:
			out.ErrorCode = apperr.Code(r.err)
			out.Error = r.err.Error()
		}
		if out.Done && mode == WaitAny {
			cancel()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	// Instances whose wait was cut short still owe the caller a status, so read
	// it back cheaply rather than reporting an empty record.
	for i := range outcomes {
		out := &outcomes[i]
		if out.Done || out.ErrorCode != "" || out.Instance.Name != "" {
			continue
		}
		inst, err := s.Inspect(ctx, out.Name)
		if err != nil {
			out.ErrorCode = apperr.Code(err)
			out.Error = err.Error()
			continue
		}
		out.Instance = inst
	}

	if collect {
		for i := range outcomes {
			out := &outcomes[i]
			if !out.Done || out.ErrorCode != "" {
				continue
			}
			inst, snap, err := s.Capture(ctx, out.Name, capture.Options{History: -1, Scope: capture.ScopeCurrent})
			if err != nil {
				out.ErrorCode = apperr.Code(err)
				out.Error = "collect: " + err.Error()
				continue
			}
			out.Instance = inst
			out.Snapshot = snap
		}
	}
	return outcomes, satisfied(outcomes, mode), nil
}

func satisfied(outcomes []WaitOutcome, mode WaitMode) bool {
	done := 0
	for _, out := range outcomes {
		if out.Done {
			done++
		}
	}
	if mode == WaitAny {
		return done > 0
	}
	return done == len(outcomes)
}
