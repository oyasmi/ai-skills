package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
	"github.com/oyasmi/agentmux/internal/tmuxctl"
)

type tmuxClient interface {
	CapturePane(ctx context.Context, target string, history int) (string, error)
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
	Dead        bool
}

type TitleIdleFunc func(paneTitle string) bool

// WaitUntilTitleIdle is the lightweight wait path used when a harness exposes
// a reliable title-level completion signal and screen capture is unnecessary.
func WaitUntilTitleIdle(ctx context.Context, tmux tmuxClient, target string, timeoutMS, pollMS int, titleIdle TitleIdleFunc) (Snapshot, error) {
	if titleIdle == nil {
		return Snapshot{}, apperr.New("internal_error", "title idle detector is required")
	}
	if pollMS <= 0 {
		pollMS = 250
	}
	if timeoutMS <= 0 {
		timeoutMS = 30000
	}
	deadline := time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)
	for {
		if time.Now().After(deadline) {
			return Snapshot{}, apperr.New("capture_timeout", "capture timed out before pane title became idle")
		}
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
		if snap.Dead || titleIdle(snap.PaneTitle) {
			return snap, nil
		}
		select {
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		case <-time.After(time.Duration(pollMS) * time.Millisecond):
		}
	}
}

func WaitStable(ctx context.Context, tmux tmuxClient, target string, history, stableMS, timeoutMS, pollMS int, titleIdle TitleIdleFunc) (Snapshot, error) {
	if pollMS <= 0 {
		pollMS = 250
	}
	if timeoutMS <= 0 {
		timeoutMS = 30000
	}
	if stableMS <= 0 {
		return Once(ctx, tmux, target, history)
	}
	deadline := time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)
	var last Snapshot
	var stableStart time.Time
	for {
		if time.Now().After(deadline) {
			return Snapshot{}, apperr.New("capture_timeout", "capture timed out before screen became stable")
		}
		snap, err := Once(ctx, tmux, target, history)
		if err != nil {
			return Snapshot{}, err
		}
		if titleIdle != nil && titleIdle(snap.PaneTitle) {
			return snap, nil
		}
		if snap.Digest == last.Digest && snap.Digest != "" {
			if stableStart.IsZero() {
				stableStart = time.Now()
			}
			snap.StableForMS = int(time.Since(stableStart).Milliseconds())
			if snap.StableForMS >= stableMS {
				return snap, nil
			}
		} else {
			stableStart = time.Now()
		}
		last = snap
		select {
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		case <-time.After(time.Duration(pollMS) * time.Millisecond):
		}
	}
}

func Once(ctx context.Context, tmux tmuxClient, target string, history int) (Snapshot, error) {
	content, err := tmux.CapturePane(ctx, target, history)
	if err != nil {
		return Snapshot{}, err
	}
	info, err := tmux.PaneInfo(ctx, target)
	if err != nil {
		return Snapshot{}, err
	}
	sum := sha256.Sum256([]byte(content))
	return Snapshot{
		CursorX:    info.CursorX,
		CursorY:    info.CursorY,
		Width:      info.Width,
		Height:     info.Height,
		History:    history,
		Content:    content,
		Digest:     hex.EncodeToString(sum[:]),
		PaneTitle:  info.PaneTitle,
		CapturedAt: time.Now(),
		Dead:       info.Dead,
	}, nil
}
