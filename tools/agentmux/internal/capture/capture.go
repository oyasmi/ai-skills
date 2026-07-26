package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/tmuxctl"
)

type tmuxClient interface {
	CaptureSnapshot(ctx context.Context, target string, history int) (tmuxctl.CaptureSnapshot, error)
	PaneInfo(ctx context.Context, target string) (tmuxctl.PaneInfo, error)
}

type Snapshot struct {
	CursorX     int
	CursorY     int
	Width       int
	Height      int
	History     int
	Content     string
	Digest      string
	PaneTitle   string
	CapturedAt  time.Time
	StableForMS int
	ElapsedMS   int
	Dead        bool
	// SawBusy records that the harness was observed working during this wait.
	// An idle signal that follows an observed busy signal is a real completion;
	// one that never left idle may just be a stale pre-prompt reading.
	SawBusy bool
	// TimedOut marks "still working when the deadline hit". It is a normal
	// outcome of waiting, not a failure, so it travels in the snapshot rather
	// than as an error.
	TimedOut bool
	Extra    map[string]any
}

type Scope string

const (
	ScopeCurrent Scope = "current"
	ScopeSession Scope = "session"
)

// Options carries what a caller wants read back from an instance.
type Options struct {
	History int
	Scope   Scope
	// Raw includes each protocol event's original JSON in normalized messages.
	// Off by default: it multiplies the payload an orchestrator must read.
	Raw bool
}

type TitleState string

const (
	TitleIdle    TitleState = "idle"
	TitleBusy    TitleState = "busy"
	TitleUnknown TitleState = "unknown"
)

// TitleStateFunc maps a harness pane title to an agent state.
type TitleStateFunc func(paneTitle string) TitleState

// WaitOptions configures a blocking wait on a tmux-backed instance.
type WaitOptions struct {
	History   int
	StableMS  int
	TimeoutMS int
	PollMS    int
	// TitleState, when set, is the harness's direct completion signal.
	TitleState TitleStateFunc
	// SettleUntil guards against believing a stale idle title. A harness needs
	// time to react to a freshly sent prompt, so before this instant an idle
	// title only counts once the harness has been observed busy at least once.
	SettleUntil time.Time
}

func (o WaitOptions) normalized() WaitOptions {
	if o.PollMS <= 0 {
		o.PollMS = 250
	}
	if o.TimeoutMS <= 0 {
		o.TimeoutMS = 30000
	}
	return o
}

// idleTrusted decides whether an idle reading may be believed yet.
func idleTrusted(sawBusy bool, settleUntil time.Time) bool {
	return sawBusy || !time.Now().Before(settleUntil)
}

// WaitUntilTitleIdle is the lightweight wait path used when a harness exposes
// a reliable title-level completion signal and screen capture is unnecessary.
//
// A timeout is reported through Snapshot.TimedOut, never as an error: "still
// working" is the expected outcome for long tasks and callers must not have to
// treat it as a failure.
func WaitUntilTitleIdle(ctx context.Context, tmux tmuxClient, target string, opts WaitOptions) (Snapshot, error) {
	opts = opts.normalized()
	if opts.TitleState == nil {
		// Without a title signal there is nothing to poll; fall back to the
		// generic screen-stability wait rather than spinning to the deadline.
		return WaitStable(ctx, tmux, target, opts)
	}
	started := time.Now()
	deadline := started.Add(time.Duration(opts.TimeoutMS) * time.Millisecond)
	sawBusy := false
	for {
		info, err := tmux.PaneInfo(ctx, target)
		if err != nil {
			return Snapshot{}, err
		}
		snap := Snapshot{
			CursorX:    info.CursorX,
			CursorY:    info.CursorY,
			Width:      info.Width,
			Height:     info.Height,
			PaneTitle:  info.PaneTitle,
			CapturedAt: time.Now(),
			Dead:       info.Dead,
		}
		state := opts.TitleState(snap.PaneTitle)
		if state == TitleBusy {
			sawBusy = true
		}
		snap.SawBusy = sawBusy
		if snap.Dead || (state == TitleIdle && idleTrusted(sawBusy, opts.SettleUntil)) {
			snap.ElapsedMS = int(time.Since(started).Milliseconds())
			return snap, nil
		}
		if !time.Now().Before(deadline) {
			snap.TimedOut = true
			snap.ElapsedMS = int(time.Since(started).Milliseconds())
			return snap, nil
		}
		if err := waitPollInterval(ctx, opts.PollMS); err != nil {
			return Snapshot{}, err
		}
	}
}

// WaitStable is the generic completion heuristic: the screen has stopped
// changing for StableMS. A title signal, when available, still short-circuits
// it, subject to the same settle guard as WaitUntilTitleIdle.
func WaitStable(ctx context.Context, tmux tmuxClient, target string, opts WaitOptions) (Snapshot, error) {
	opts = opts.normalized()
	if opts.StableMS <= 0 {
		return Once(ctx, tmux, target, opts.History)
	}
	started := time.Now()
	deadline := started.Add(time.Duration(opts.TimeoutMS) * time.Millisecond)
	var last Snapshot
	var stableStart time.Time
	sawBusy := false
	for {
		snap, err := Once(ctx, tmux, target, opts.History)
		if err != nil {
			return Snapshot{}, err
		}
		if opts.TitleState != nil {
			state := opts.TitleState(snap.PaneTitle)
			if state == TitleBusy {
				sawBusy = true
			}
			snap.SawBusy = sawBusy
			if state == TitleIdle && idleTrusted(sawBusy, opts.SettleUntil) {
				snap.ElapsedMS = int(time.Since(started).Milliseconds())
				return snap, nil
			}
		}
		if snap.Digest == last.Digest && snap.Digest != "" {
			if stableStart.IsZero() {
				stableStart = time.Now()
			}
			snap.StableForMS = int(time.Since(stableStart).Milliseconds())
			// A screen that never moved can also be a harness that has not
			// started yet, so stability is only conclusive past the settle point.
			if snap.StableForMS >= opts.StableMS && idleTrusted(sawBusy, opts.SettleUntil) {
				snap.ElapsedMS = int(time.Since(started).Milliseconds())
				return snap, nil
			}
		} else {
			stableStart = time.Now()
		}
		if !time.Now().Before(deadline) {
			snap.TimedOut = true
			snap.ElapsedMS = int(time.Since(started).Milliseconds())
			return snap, nil
		}
		last = snap
		if err := waitPollInterval(ctx, opts.PollMS); err != nil {
			return Snapshot{}, err
		}
	}
}

func waitPollInterval(ctx context.Context, pollMS int) error {
	timer := time.NewTimer(time.Duration(pollMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func Once(ctx context.Context, tmux tmuxClient, target string, history int) (Snapshot, error) {
	sampled, err := tmux.CaptureSnapshot(ctx, target, history)
	if err != nil {
		return Snapshot{}, err
	}
	sum := sha256.Sum256([]byte(sampled.Content))
	return Snapshot{
		CursorX:    sampled.Info.CursorX,
		CursorY:    sampled.Info.CursorY,
		Width:      sampled.Info.Width,
		Height:     sampled.Info.Height,
		History:    history,
		Content:    sampled.Content,
		Digest:     hex.EncodeToString(sum[:]),
		PaneTitle:  sampled.Info.PaneTitle,
		CapturedAt: time.Now(),
		Dead:       sampled.Info.Dead,
	}, nil
}
