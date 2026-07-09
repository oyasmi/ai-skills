package service

import (
	"context"
	"os/exec"
	"time"

	"github.com/oyasmi/agentmux/internal/capture"
	"github.com/oyasmi/agentmux/internal/execjsonctl"
	"github.com/oyasmi/agentmux/internal/instance"
	"github.com/oyasmi/agentmux/internal/ndjsonctl"
)

// harness is a structured (non-tmux) agent transport.
//
// This is a dispatch seam, not a shared implementation: the ndjson and execjson
// controllers have genuinely different process models and share no code. The
// interface exists only so the service layer does not grow a branch per harness.
type harness interface {
	Start(ctx context.Context, inst instance.Instance, command, systemPrompt string, resume bool) (instance.Instance, error)
	Reconcile(ctx context.Context, inst instance.Instance) (instance.Instance, error)
	SendPrompt(ctx context.Context, inst instance.Instance, text string) (instance.Instance, error)
	Capture(ctx context.Context, inst instance.Instance, history int) (capture.Snapshot, error)
	Wait(ctx context.Context, inst instance.Instance, timeout time.Duration) (capture.Snapshot, error)
	Interrupt(ctx context.Context, inst instance.Instance) (instance.Instance, error)
	Halt(ctx context.Context, inst instance.Instance, immediately bool, timeout time.Duration) error
	Attach(inst instance.Instance) *exec.Cmd
	CanResume(inst instance.Instance) bool
}

var (
	_ harness = ndjsonctl.Controller{}
	_ harness = execjsonctl.Controller{}
)

// harnessFor returns the structured controller for an instance, or false when
// the instance is driven through tmux.
func (s Service) harnessFor(inst instance.Instance) (harness, bool) {
	switch inst.HarnessType {
	case ndjsonctl.HarnessType:
		return s.NDJSON, true
	case execjsonctl.HarnessType:
		return s.Codex, true
	default:
		return nil, false
	}
}
