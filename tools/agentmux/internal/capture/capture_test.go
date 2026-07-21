package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync/atomic"
	"testing"

	"github.com/oyasmi/agentmux/internal/tmuxctl"
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
	snap, err := WaitStable(context.Background(), ft, "target", 100, 1, 5000, 1, nil)
	if err != nil {
		t.Fatalf("WaitStable: %v", err)
	}
	if snap.Content != "stable" {
		t.Fatalf("expected stable content, got %q", snap.Content)
	}
}

func TestWaitStableTimeout(t *testing.T) {
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: "always-different-1", Info: tmuxctl.PaneInfo{}},
			{Content: "always-different-2", Info: tmuxctl.PaneInfo{}},
			{Content: "always-different-3", Info: tmuxctl.PaneInfo{}},
		},
	}
	_, err := WaitStable(context.Background(), ft, "target", 100, 60000, 50, 1, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitStableTitleIdleEarlyReturn(t *testing.T) {
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: "busy-content", Info: tmuxctl.PaneInfo{PaneTitle: "busy"}},
			{Content: "busy-content", Info: tmuxctl.PaneInfo{PaneTitle: "idle"}},
		},
	}
	titleIdle := func(title string) bool { return title == "idle" }
	snap, err := WaitStable(context.Background(), ft, "target", 100, 60000, 5000, 1, titleIdle)
	if err != nil {
		t.Fatalf("WaitStable: %v", err)
	}
	if snap.PaneTitle != "idle" {
		t.Fatalf("expected pane_title idle, got %q", snap.PaneTitle)
	}
}

func TestWaitStableZeroStableMSReturnsImmediately(t *testing.T) {
	ft := &fakeTmux{
		snapshots: []tmuxctl.CaptureSnapshot{
			{Content: "once", Info: tmuxctl.PaneInfo{Width: 80, Height: 24}},
		},
	}
	snap, err := WaitStable(context.Background(), ft, "target", 100, 0, 5000, 1, nil)
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
	titleIdle := func(title string) bool { return title == "idle" }
	snap, err := WaitUntilTitleIdle(context.Background(), ft, "target", 5000, 1, titleIdle)
	if err != nil {
		t.Fatalf("WaitUntilTitleIdle: %v", err)
	}
	if snap.PaneTitle != "idle" {
		t.Fatalf("expected pane_title idle, got %q", snap.PaneTitle)
	}
}

func TestWaitUntilTitleIdleTimeout(t *testing.T) {
	ft := &fakeTmux{
		paneInfos: []tmuxctl.PaneInfo{
			{PaneTitle: "busy"},
			{PaneTitle: "busy"},
			{PaneTitle: "busy"},
		},
	}
	titleIdle := func(title string) bool { return title == "idle" }
	_, err := WaitUntilTitleIdle(context.Background(), ft, "target", 50, 1, titleIdle)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitUntilTitleIdleNilFunc(t *testing.T) {
	ft := &fakeTmux{}
	_, err := WaitUntilTitleIdle(context.Background(), ft, "target", 5000, 1, nil)
	if err == nil {
		t.Fatal("expected error for nil titleIdle func")
	}
}

func TestWaitUntilTitleIdleReturnsOnDead(t *testing.T) {
	ft := &fakeTmux{
		paneInfos: []tmuxctl.PaneInfo{
			{PaneTitle: "busy"},
			{Dead: true, PaneTitle: "exited"},
		},
	}
	titleIdle := func(title string) bool { return title == "idle" }
	snap, err := WaitUntilTitleIdle(context.Background(), ft, "target", 5000, 1, titleIdle)
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
	_, err := WaitStable(ctx, ft, "target", 100, 60000, 5000, 1, nil)
	if err == nil {
		t.Fatal("expected context canceled error")
	}
}
