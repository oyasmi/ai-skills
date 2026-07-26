package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/tmuxctl"
)

type fakeTmux struct {
	snapshots []tmuxctl.CaptureSnapshot
	paneInfos []tmuxctl.PaneInfo
	calls     atomic.Int32
}

func (f *fakeTmux) CaptureSnapshot(ctx context.Context, target string, history int) (tmuxctl.CaptureSnapshot, error) {
	i := int(f.calls.Add(1)) - 1
	if i >= len(f.snapshots) {
		i = len(f.snapshots) - 1
	}
	return f.snapshots[i], nil
}

func (f *fakeTmux) PaneInfo(ctx context.Context, target string) (tmuxctl.PaneInfo, error) {
	i := int(f.calls.Add(1)) - 1
	if i >= len(f.paneInfos) {
		i = len(f.paneInfos) - 1
	}
	return f.paneInfos[i], nil
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestOnceReturnsSnapshot(t *testing.T) {
	content := "hello world"
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: content, Info: tmuxctl.PaneInfo{CursorX: 1, CursorY: 2, Width: 80, Height: 24, PaneTitle: "test"}},
		},
	}
	snap, err := Once(context.Background(), ft, "target", 100)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if snap.Content != content {
		t.Fatalf("expected content %q, got %q", content, snap.Content)
	}
	if snap.Digest != digest(content) {
		t.Fatalf("digest mismatch")
	}
	if snap.CursorX != 1 || snap.CursorY != 2 || snap.Width != 80 || snap.Height != 24 {
		t.Fatalf("unexpected pane info: %+v", snap)
	}
}

func titleState(idleTitle string) TitleStateFunc {
	return func(title string) TitleState {
		switch title {
		case idleTitle:
			return TitleIdle
		case "":
			return TitleUnknown
		default:
			return TitleBusy
		}
	}
}

func TestWaitStableReturnsWhenContentStable(t *testing.T) {
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: "changing1", Info: tmuxctl.PaneInfo{Width: 80, Height: 24}},
			{Content: "stable", Info: tmuxctl.PaneInfo{Width: 80, Height: 24}},
			{Content: "stable", Info: tmuxctl.PaneInfo{Width: 80, Height: 24}},
			{Content: "stable", Info: tmuxctl.PaneInfo{Width: 80, Height: 24}},
			{Content: "stable", Info: tmuxctl.PaneInfo{Width: 80, Height: 24}},
		},
	}
	// stable_ms=1 means any two consecutive identical captures qualify
	snap, err := WaitStable(context.Background(), ft, "target", WaitOptions{History: 100, StableMS: 1, TimeoutMS: 5000, PollMS: 1})
	if err != nil {
		t.Fatalf("WaitStable: %v", err)
	}
	if snap.Content != "stable" {
		t.Fatalf("expected stable content, got %q", snap.Content)
	}
	if snap.TimedOut {
		t.Fatal("a settled screen must not report a timeout")
	}
}

// A timeout means "still working", which is a normal outcome of waiting on a
// long task. It must reach the caller as a flagged snapshot, not as an error.
func TestWaitStableTimeoutIsNotAnError(t *testing.T) {
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: "always-different-1", Info: tmuxctl.PaneInfo{}},
			{Content: "always-different-2", Info: tmuxctl.PaneInfo{}},
			{Content: "always-different-3", Info: tmuxctl.PaneInfo{}},
		},
	}
	snap, err := WaitStable(context.Background(), ft, "target", WaitOptions{History: 100, StableMS: 60000, TimeoutMS: 50, PollMS: 1})
	if err != nil {
		t.Fatalf("WaitStable must not fail on timeout: %v", err)
	}
	if !snap.TimedOut {
		t.Fatal("expected TimedOut to be set")
	}
}

func TestWaitStableTitleIdleEarlyReturn(t *testing.T) {
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: "busy-content", Info: tmuxctl.PaneInfo{PaneTitle: "busy"}},
			{Content: "busy-content", Info: tmuxctl.PaneInfo{PaneTitle: "idle"}},
		},
	}
	snap, err := WaitStable(context.Background(), ft, "target", WaitOptions{History: 100, StableMS: 60000, TimeoutMS: 5000, PollMS: 1, TitleState: titleState("idle")})
	if err != nil {
		t.Fatalf("WaitStable: %v", err)
	}
	if snap.PaneTitle != "idle" {
		t.Fatalf("expected pane_title idle, got %q", snap.PaneTitle)
	}
	if !snap.SawBusy {
		t.Fatal("expected the busy observation to be reported")
	}
}

// A screen that has not moved yet is not evidence of completion when the
// harness was just handed a prompt.
func TestWaitStableIgnoresStabilityInsideSettleWindow(t *testing.T) {
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: "unchanged", Info: tmuxctl.PaneInfo{}},
		},
	}
	snap, err := WaitStable(context.Background(), ft, "target", WaitOptions{
		History:     100,
		StableMS:    1,
		TimeoutMS:   30,
		PollMS:      1,
		SettleUntil: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("WaitStable: %v", err)
	}
	if !snap.TimedOut {
		t.Fatal("a stable screen inside the settle window must not count as done")
	}
}

func TestWaitStableZeroStableMSReturnsImmediately(t *testing.T) {
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: "once", Info: tmuxctl.PaneInfo{Width: 80, Height: 24}},
		},
	}
	snap, err := WaitStable(context.Background(), ft, "target", WaitOptions{History: 100, StableMS: 0, TimeoutMS: 5000, PollMS: 1})
	if err != nil {
		t.Fatalf("WaitStable with stableMS=0: %v", err)
	}
	if snap.Content != "once" {
		t.Fatalf("expected content %q, got %q", "once", snap.Content)
	}
}

func TestWaitUntilTitleIdleReturnsWhenIdle(t *testing.T) {
	ft := &fakeTmux{
		paneInfos: []tmuxctl.PaneInfo{
			{PaneTitle: "busy", Width: 80, Height: 24},
			{PaneTitle: "idle", Width: 80, Height: 24},
		},
	}
	snap, err := WaitUntilTitleIdle(context.Background(), ft, "target", WaitOptions{TimeoutMS: 5000, PollMS: 1, TitleState: titleState("idle")})
	if err != nil {
		t.Fatalf("WaitUntilTitleIdle: %v", err)
	}
	if snap.PaneTitle != "idle" {
		t.Fatalf("expected pane_title idle, got %q", snap.PaneTitle)
	}
	if !snap.SawBusy {
		t.Fatal("expected the busy observation to be reported")
	}
}

// The regression that made wait unusable: a harness has not reacted to the new
// prompt yet, so its title still reads idle from the previous turn. Believing
// it reports a task as finished before the agent has started.
func TestWaitUntilTitleIdleDistrustsStaleIdleInsideSettleWindow(t *testing.T) {
	ft := &fakeTmux{
		paneInfos: []tmuxctl.PaneInfo{
			{PaneTitle: "idle"},
			{PaneTitle: "idle"},
			{PaneTitle: "busy"},
			{PaneTitle: "busy"},
			{PaneTitle: "idle"},
		},
	}
	snap, err := WaitUntilTitleIdle(context.Background(), ft, "target", WaitOptions{
		TimeoutMS:   5000,
		PollMS:      1,
		TitleState:  titleState("idle"),
		SettleUntil: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("WaitUntilTitleIdle: %v", err)
	}
	if !snap.SawBusy {
		t.Fatal("expected the wait to run past the stale idle titles")
	}
	if snap.TimedOut {
		t.Fatal("the idle title after an observed busy must be accepted")
	}
	if got := ft.calls.Load(); got < 5 {
		t.Fatalf("expected the stale idle readings to be skipped, polled %d times", got)
	}
}

// Outside the settle window an idle title is authoritative on its own: waiting
// for a busy transition that already happened would hang until the timeout.
func TestWaitUntilTitleIdleTrustsIdleAfterSettleWindow(t *testing.T) {
	ft := &fakeTmux{paneInfos: []tmuxctl.PaneInfo{{PaneTitle: "idle"}}}
	snap, err := WaitUntilTitleIdle(context.Background(), ft, "target", WaitOptions{
		TimeoutMS:   5000,
		PollMS:      1,
		TitleState:  titleState("idle"),
		SettleUntil: time.Now().Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("WaitUntilTitleIdle: %v", err)
	}
	if snap.TimedOut || snap.SawBusy {
		t.Fatalf("expected an immediate trusted idle, got %+v", snap)
	}
}

func TestWaitUntilTitleIdleTimeoutIsNotAnError(t *testing.T) {
	ft := &fakeTmux{
		paneInfos: []tmuxctl.PaneInfo{
			{PaneTitle: "busy"},
			{PaneTitle: "busy"},
			{PaneTitle: "busy"},
		},
	}
	snap, err := WaitUntilTitleIdle(context.Background(), ft, "target", WaitOptions{TimeoutMS: 50, PollMS: 1, TitleState: titleState("idle")})
	if err != nil {
		t.Fatalf("WaitUntilTitleIdle must not fail on timeout: %v", err)
	}
	if !snap.TimedOut {
		t.Fatal("expected TimedOut to be set")
	}
	if snap.ElapsedMS <= 0 {
		t.Fatalf("expected elapsed time to be reported, got %d", snap.ElapsedMS)
	}
}

// Without a title signal the call must still behave like a wait, falling back
// to screen stability rather than polling a signal that will never arrive.
func TestWaitUntilTitleIdleWithoutTitleSignalFallsBackToStability(t *testing.T) {
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: "settled", Info: tmuxctl.PaneInfo{}},
		},
	}
	snap, err := WaitUntilTitleIdle(context.Background(), ft, "target", WaitOptions{History: 10, StableMS: 1, TimeoutMS: 5000, PollMS: 1})
	if err != nil {
		t.Fatalf("WaitUntilTitleIdle: %v", err)
	}
	if snap.Content != "settled" {
		t.Fatalf("expected the stability path to run, got %+v", snap)
	}
}

func TestWaitUntilTitleIdleReturnsOnDead(t *testing.T) {
	ft := &fakeTmux{
		paneInfos: []tmuxctl.PaneInfo{
			{PaneTitle: "busy"},
			{Dead: true, PaneTitle: "exited"},
		},
	}
	snap, err := WaitUntilTitleIdle(context.Background(), ft, "target", WaitOptions{TimeoutMS: 5000, PollMS: 1, TitleState: titleState("idle")})
	if err != nil {
		t.Fatalf("WaitUntilTitleIdle: %v", err)
	}
	if !snap.Dead {
		t.Fatal("expected snap.Dead to be true")
	}
}

func TestWaitStableContextCancel(t *testing.T) {
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: "content", Info: tmuxctl.PaneInfo{}},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := WaitStable(ctx, ft, "target", WaitOptions{History: 100, StableMS: 60000, TimeoutMS: 5000, PollMS: 1})
	if err == nil {
		t.Fatal("expected context canceled error")
	}
}
