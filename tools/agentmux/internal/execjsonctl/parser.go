package execjsonctl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

// maxAggregatedOutput caps how much of a command_execution's captured output is
// surfaced in normalized messages. The full text stays in Raw and output.jsonl.
const maxAggregatedOutput = 8 << 10

// readEvents parses output.jsonl from a byte offset. A trailing line without a
// newline is a partially written event; it is left for the next read rather
// than parsed, so nextOffset never advances past it.
func readEvents(path string, from int64) ([]Event, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, from, nil
		}
		return nil, from, apperr.Wrap("execjson_parse_error", err, "open output")
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, from, apperr.Wrap("execjson_parse_error", err, "seek output")
	}
	events := []Event{}
	next := from
	r := bufio.NewReader(f)
	for {
		lineStart := next
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if line[len(line)-1] != '\n' {
				break
			}
			next += int64(len(line))
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) > 0 {
				ev, perr := parseEvent(lineStart, next, trimmed)
				if perr != nil {
					return events, lineStart, perr
				}
				events = append(events, ev)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return events, next, apperr.Wrap("execjson_parse_error", err, "read output")
		}
	}
	return events, next, nil
}

func parseEvent(offset, endOffset int64, line []byte) (Event, error) {
	var base struct {
		Type     string          `json:"type"`
		ThreadID string          `json:"thread_id"`
		Message  string          `json:"message"`
		Usage    Usage           `json:"usage"`
		Error    EventError      `json:"error"`
		Item     json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(line, &base); err != nil {
		return Event{}, apperr.Wrap("execjson_parse_error", err, "parse output line at offset %d", offset)
	}
	ev := Event{
		Offset:    offset,
		EndOffset: endOffset,
		Raw:       append(json.RawMessage(nil), line...),
		Type:      base.Type,
		ThreadID:  base.ThreadID,
		Message:   base.Message,
		Usage:     base.Usage,
		Error:     base.Error,
	}
	if len(base.Item) > 0 {
		var item Item
		if err := json.Unmarshal(base.Item, &item); err != nil {
			return Event{}, apperr.Wrap("execjson_parse_error", err, "parse item at offset %d", offset)
		}
		item.Raw = append(json.RawMessage(nil), base.Item...)
		ev.Item = item
		ev.HasItem = true
	}
	return ev, nil
}

// turnOutcome summarizes the terminal state of a single turn's event range.
type turnOutcome struct {
	Terminal  string // "completed", "failed", or "" when no terminal event was seen
	Usage     Usage
	ThreadID  string
	Error     string
	EndOffset int64
}

func scanTurn(events []Event, from int64) turnOutcome {
	out := turnOutcome{EndOffset: from}
	for _, ev := range events {
		out.EndOffset = ev.EndOffset
		switch ev.Type {
		case "thread.started":
			if ev.ThreadID != "" {
				out.ThreadID = ev.ThreadID
			}
		case "turn.completed":
			out.Terminal = "completed"
			out.Usage = out.Usage.add(ev.Usage)
		case "turn.failed":
			out.Terminal = "failed"
			out.Error = ev.Error.Message
		}
	}
	return out
}

// normalizeEvents flattens the event stream into display messages.
//
// item.started -> item.updated* -> item.completed all describe one item, so
// later versions replace earlier ones in place. Crucially, codex restarts item
// ids at item_0 for every turn, so the dedup index is reset on turn.started --
// otherwise turn 2's item_0 would overwrite turn 1's.
func normalizeEvents(events []Event) ([]NormalizedMessage, string, Usage) {
	out := []NormalizedMessage{}
	index := map[string]int{}
	var content string
	var usage Usage

	put := func(id string, msg NormalizedMessage) {
		if id == "" {
			out = append(out, msg)
			return
		}
		if at, ok := index[id]; ok {
			out[at] = msg
			return
		}
		index[id] = len(out)
		out = append(out, msg)
	}

	for _, ev := range events {
		switch ev.Type {
		case "thread.started":
			out = append(out, NormalizedMessage{Type: "system", ContentType: "thread_started", Text: ev.ThreadID, Raw: ev.Raw})
		case "turn.started":
			index = map[string]int{}
			out = append(out, NormalizedMessage{Type: "system", ContentType: "turn_started", Raw: ev.Raw})
		case "turn.completed":
			usage = usage.add(ev.Usage)
			out = append(out, NormalizedMessage{Type: "result", Raw: ev.Raw})
		case "turn.failed":
			if ev.Error.Message != "" && content == "" {
				content = ev.Error.Message
			}
			out = append(out, NormalizedMessage{Type: "result", Text: ev.Error.Message, Raw: ev.Raw})
		case "error":
			out = append(out, NormalizedMessage{Type: "system", ContentType: "error", Text: ev.Message, Raw: ev.Raw})
		case "item.started", "item.updated", "item.completed":
			if !ev.HasItem {
				continue
			}
			msg := normalizeItem(ev.Item)
			if ev.Item.Type == "agent_message" && ev.Type == "item.completed" && ev.Item.Text != "" {
				content = ev.Item.Text
			}
			put(ev.Item.ID, msg)
		default:
			out = append(out, NormalizedMessage{Type: "unknown", Raw: ev.Raw})
		}
	}
	return out, content, usage
}

func normalizeItem(item Item) NormalizedMessage {
	switch item.Type {
	case "agent_message":
		return NormalizedMessage{Type: "assistant", Role: "assistant", ContentType: "text", Text: item.Text}
	case "reasoning":
		return NormalizedMessage{Type: "assistant", Role: "assistant", ContentType: "thinking", Text: item.Text}
	case "command_execution":
		input, _ := json.Marshal(map[string]any{
			"command":   item.Command,
			"exit_code": item.ExitCode,
			"status":    item.Status,
		})
		return NormalizedMessage{
			Type:  "tool_use",
			Tool:  "shell",
			Text:  truncate(item.AggregatedOutput, maxAggregatedOutput),
			Input: input,
			Raw:   item.Raw,
		}
	case "file_change":
		return NormalizedMessage{Type: "tool_use", Tool: "file_change", Raw: item.Raw}
	case "mcp_tool_call":
		return NormalizedMessage{Type: "tool_use", Tool: mcpToolName(item), Raw: item.Raw}
	case "web_search":
		return NormalizedMessage{Type: "tool_use", Tool: "web_search", Raw: item.Raw}
	case "todo_list":
		return NormalizedMessage{Type: "system", ContentType: "todo_list", Raw: item.Raw}
	case "error":
		return NormalizedMessage{Type: "system", ContentType: "error", Text: item.Message, Raw: item.Raw}
	default:
		return NormalizedMessage{Type: "unknown", ContentType: item.Type, Raw: item.Raw}
	}
}

func mcpToolName(item Item) string {
	name := item.Tool
	if name == "" {
		name = item.Name
	}
	switch {
	case item.Server != "" && name != "":
		return item.Server + "/" + name
	case name != "":
		return name
	default:
		return "mcp_tool_call"
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…(truncated)"
}

func trimMessages(msgs []NormalizedMessage, history int) []NormalizedMessage {
	if history <= 0 || len(msgs) <= history {
		return msgs
	}
	return msgs[len(msgs)-history:]
}

func summarizeStderr(path string) string {
	s := tailFile(path, 2048)
	if s == "" {
		return "codex exec exited without emitting turn.completed or turn.failed"
	}
	return strings.TrimSpace(s)
}
