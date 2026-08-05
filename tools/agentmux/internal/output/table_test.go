package output

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestDisplayWidthAccountsForCJKAndCombiningRunes(t *testing.T) {
	if got := displayWidth("中文"); got != 4 {
		t.Fatalf("displayWidth(中文) = %d, want 4", got)
	}
	if got := displayWidth("e\u0301"); got != 1 {
		t.Fatalf("displayWidth(combining) = %d, want 1", got)
	}
}

func TestFitCellUsesDisplayWidthAndEllipsis(t *testing.T) {
	if got := fitCell("中文描述", 5); got != "中文…" {
		t.Fatalf("fitCell = %q, want 中文…", got)
	}
	if got := fitCell("中文", 4); got != "中文" {
		t.Fatalf("fitCell unexpectedly truncated a fitting value: %q", got)
	}
}

func TestRenderTableFitsConfiguredWidthWithoutTrailingPadding(t *testing.T) {
	t.Setenv("COLUMNS", "40")
	var buf bytes.Buffer
	err := RenderTable(&buf, []string{"Name", "Description"}, [][]string{{"编码助手-A", "中文描述以及更多内容"}})
	if err != nil {
		t.Fatalf("RenderTable: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Fatalf("table row has trailing padding: %q", line)
		}
		if displayWidth(line) > 40 {
			t.Fatalf("table row exceeds configured width: width=%d line=%q", displayWidth(line), line)
		}
	}
}

func TestTableWidthUsesColumnsBeforeTTY(t *testing.T) {
	t.Setenv("COLUMNS", "73")
	if got := tableWidth(os.Stdout); got != 73 {
		t.Fatalf("tableWidth = %d, want 73", got)
	}
}

func TestRenderTableKeepsListHeadersReadableAtNarrowWidth(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	var buf bytes.Buffer
	if err := RenderTable(&buf, []string{"Name", "Template", "Status", "Model", "CWD", "Created", "Last activity"}, nil); err != nil {
		t.Fatalf("RenderTable: %v", err)
	}
	if !strings.Contains(buf.String(), "Last activity") {
		t.Fatalf("narrow table truncated a meaningful header: %q", buf.String())
	}
}
