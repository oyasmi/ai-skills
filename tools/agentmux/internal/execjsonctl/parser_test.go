package execjsonctl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

func writeOutput(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, outputJSONLName)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	return path
}

func TestReadEventsLeavesIncompleteTrailingLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, outputJSONLName)
	complete := `{"type":"turn.started"}` + "\n"
	partial := `{"type":"item.compl`
	if err := os.WriteFile(path, []byte(complete+partial), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	events, next, err := readEvents(path, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected only the complete line, got %d", len(events))
	}
	if next != int64(len(complete)) {
		t.Fatalf("nextOffset must stop before the partial line, got %d want %d", next, len(complete))
	}
}

func TestReadEventsReportsParseErrorWithOffset(t *testing.T) {
	path := writeOutput(t, `{"type":"turn.started"}`, `not json`)
	_, _, err := readEvents(path, 0)
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if code := apperr.Code(err); code != "execjson_parse_error" {
		t.Fatalf("expected execjson_parse_error, got %s", code)
	}
	if !strings.Contains(err.Error(), "offset 24") {
		t.Fatalf("expected the failing offset in the message, got %q", err.Error())
	}
}

func TestNormalizeEventsCollapsesItemLifecycle(t *testing.T) {
	path := writeOutput(t,
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"ls","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"ls","aggregated_output":"a\nb\n","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"all done"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":2,"reasoning_output_tokens":1}}`,
	)
	events, _, err := readEvents(path, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	msgs, content, usage := normalizeEvents(events)

	if content != "all done" {
		t.Fatalf("expected the agent message as content, got %q", content)
	}
	if usage.InputTokens != 10 || usage.CachedInputTokens != 4 || usage.OutputTokens != 2 || usage.ReasoningOutputTokens != 1 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	shells := 0
	for _, m := range msgs {
		if m.Tool == "shell" {
			shells++
			if !strings.Contains(string(m.Input), `"exit_code":0`) {
				t.Fatalf("expected the completed version to win, got input %s", m.Input)
			}
		}
	}
	if shells != 1 {
		t.Fatalf("item.started and item.completed must collapse into one message, got %d", shells)
	}
}

// codex restarts item ids at item_0 for every turn, so dedup must be scoped to
// a turn or turn 2's items would silently overwrite turn 1's.
func TestNormalizeEventsKeepsItemZeroOfEveryTurn(t *testing.T) {
	path := writeOutput(t,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"first"}}`,
		`{"type":"turn.completed","usage":{}}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"second"}}`,
		`{"type":"turn.completed","usage":{}}`,
	)
	events, _, err := readEvents(path, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	msgs, content, _ := normalizeEvents(events)
	if content != "second" {
		t.Fatalf("expected the newest agent message, got %q", content)
	}
	var texts []string
	for _, m := range msgs {
		if m.Type == "assistant" {
			texts = append(texts, m.Text)
		}
	}
	if len(texts) != 2 || texts[0] != "first" || texts[1] != "second" {
		t.Fatalf("both turns' item_0 must survive, got %v", texts)
	}
}

func TestNormalizeEventsHandlesFailureAndUnknownTypes(t *testing.T) {
	path := writeOutput(t,
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"error","message":"bad model"}}`,
		`{"type":"turn.started"}`,
		`{"type":"error","message":"http 400"}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"web_search"}}`,
		`{"type":"something.new","hello":"world"}`,
		`{"type":"turn.failed","error":{"message":"invalid request"}}`,
	)
	events, _, err := readEvents(path, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	msgs, content, _ := normalizeEvents(events)
	if content != "invalid request" {
		t.Fatalf("expected the failure message as content, got %q", content)
	}
	var sawUnknown, sawWebSearch bool
	for _, m := range msgs {
		if m.Type == "unknown" {
			sawUnknown = true
		}
		if m.Tool == "web_search" {
			sawWebSearch = true
		}
	}
	if !sawUnknown {
		t.Fatal("unknown event types must be preserved, not dropped or fatal")
	}
	if !sawWebSearch {
		t.Fatal("expected the web_search tool message")
	}
}

func TestScanTurnReportsTerminalOutcome(t *testing.T) {
	path := writeOutput(t,
		`{"type":"thread.started","thread_id":"t9"}`,
		`{"type":"turn.completed","usage":{"input_tokens":3,"output_tokens":4}}`,
	)
	events, _, err := readEvents(path, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	out := scanTurn(events, 0)
	if out.Terminal != "completed" || out.ThreadID != "t9" || out.Usage.InputTokens != 3 {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

func TestTruncateCapsAggregatedOutput(t *testing.T) {
	long := strings.Repeat("x", maxAggregatedOutput+100)
	got := truncate(long, maxAggregatedOutput)
	if len(got) >= len(long) || !strings.HasSuffix(got, "(truncated)") {
		t.Fatalf("expected truncation, got %d bytes", len(got))
	}
}
