package tmuxctl

import "testing"

func TestParsePaneInfoUsesUnitSeparator(t *testing.T) {
	out := "1" + paneInfoSep + "2" + paneInfoSep + "80" + paneInfoSep + "24" + paneInfoSep + "0" + paneInfoSep + "bash|worker" + paneInfoSep + "✳ Ready|Done"
	info, err := parsePaneInfo(out)
	if err != nil {
		t.Fatalf("parsePaneInfo: %v", err)
	}
	if info.CursorX != 1 || info.CursorY != 2 || info.Width != 80 || info.Height != 24 {
		t.Fatalf("unexpected pane geometry: %+v", info)
	}
	if info.Command != "bash|worker" {
		t.Fatalf("unexpected command: %q", info.Command)
	}
	if info.PaneTitle != "✳ Ready|Done" {
		t.Fatalf("unexpected pane title: %q", info.PaneTitle)
	}
}

func TestParsePaneInfoRejectsMalformedOutput(t *testing.T) {
	if _, err := parsePaneInfo("1" + paneInfoSep + "2"); err == nil {
		t.Fatalf("expected malformed pane info to fail")
	}
}

func TestParseCaptureSnapshotSeparatesMetadataAndContent(t *testing.T) {
	out := "1" + paneInfoSep + "2" + paneInfoSep + "80" + paneInfoSep + "24" + paneInfoSep + "0" + paneInfoSep + "codex|agent" + paneInfoSep + "Ready | waiting" + "\n" + "line1\nline2"

	snap, err := parseCaptureSnapshot(out)
	if err != nil {
		t.Fatalf("parseCaptureSnapshot: %v", err)
	}
	if snap.Info.Command != "codex|agent" {
		t.Fatalf("unexpected command: %q", snap.Info.Command)
	}
	if snap.Info.PaneTitle != "Ready | waiting" {
		t.Fatalf("unexpected title: %q", snap.Info.PaneTitle)
	}
	if snap.Content != "line1\nline2" {
		t.Fatalf("unexpected content: %q", snap.Content)
	}
}
