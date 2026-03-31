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
