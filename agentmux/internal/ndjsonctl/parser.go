package ndjsonctl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/oyasmi/agentmux/internal/apperr"
)

func readEvents(path string, from int64, limit int) ([]Event, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, from, nil
		}
		return nil, from, apperr.Wrap("ndjson_parse_error", err, "open output")
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, from, apperr.Wrap("ndjson_parse_error", err, "seek output")
	}
	events := []Event{}
	next := from
	r := bufio.NewReader(f)
	for limit <= 0 || len(events) < limit {
		lineStart := next
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if line[len(line)-1] != '\n' {
				// Ignore incomplete trailing line until the writer finishes it.
				break
			}
			next += int64(len(line))
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			ev, err := parseEvent(lineStart, line)
			if err != nil {
				return events, next, err
			}
			events = append(events, ev)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return events, next, apperr.Wrap("ndjson_parse_error", err, "read output")
		}
	}
	return events, next, nil
}

func parseEvent(offset int64, line []byte) (Event, error) {
	var base struct {
		Type      string          `json:"type"`
		Subtype   string          `json:"subtype"`
		State     string          `json:"state"`
		UUID      string          `json:"uuid"`
		SessionID string          `json:"session_id"`
		Result    string          `json:"result"`
		IsError   bool            `json:"is_error"`
		Usage     Usage           `json:"usage"`
		CostUSD   float64         `json:"total_cost_usd"`
		Message   Message         `json:"message"`
		Event     json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(line, &base); err != nil {
		return Event{}, apperr.Wrap("ndjson_parse_error", err, "parse output line at offset %d", offset)
	}
	ev := Event{
		Offset:    offset,
		Raw:       append(json.RawMessage(nil), line...),
		Type:      base.Type,
		Subtype:   base.Subtype,
		State:     base.State,
		UUID:      base.UUID,
		SessionID: base.SessionID,
		Result:    base.Result,
		IsError:   base.IsError,
		Usage:     base.Usage,
		CostUSD:   base.CostUSD,
		Message:   base.Message,
	}
	if len(base.Event) > 0 {
		_ = json.Unmarshal(base.Event, &ev.Event)
	}
	return ev, nil
}

func applyEvents(st *State, events []Event) {
	for _, ev := range events {
		st.LastReadOffset = maxInt64(st.LastReadOffset, ev.Offset+int64(len(ev.Raw))+1)
		switch ev.Type {
		case "user":
			if ev.UUID != "" {
				for i := range st.PendingPrompts {
					if st.PendingPrompts[i].UUID == ev.UUID && st.PendingPrompts[i].State == PromptSent {
						st.PendingPrompts[i].State = PromptReplayed
						st.PendingPrompts[i].ReplayedOffset = ev.Offset
						st.ActivePromptUUID = ev.UUID
						st.ResumeAvailable = true
						break
					}
				}
			}
		case "result":
			st.LastResultOffset = ev.Offset
			st.LastResultAt = nowUTC()
			st.TotalTurns++
			st.TotalCostUSD += ev.CostUSD
			st.TotalInputTokens += ev.Usage.InputTokens
			st.TotalOutputTokens += ev.Usage.OutputTokens
			if ev.IsError {
				st.LastError = ev.Result
			}
			for i := range st.PendingPrompts {
				if st.PendingPrompts[i].State == PromptReplayed {
					st.PendingPrompts[i].State = PromptResult
					st.PendingPrompts[i].ResultOffset = ev.Offset
					st.LastCompletedPromptUUID = st.PendingPrompts[i].UUID
					st.ResumeAvailable = true
					break
				}
			}
		case "system":
			if ev.Subtype == "session_state_changed" {
				if ev.State == "idle" {
					st.SessionIdle = true
					for i := range st.PendingPrompts {
						if st.PendingPrompts[i].State == PromptResult {
							st.PendingPrompts[i].State = PromptIdle
							st.LastCompletedPromptUUID = st.PendingPrompts[i].UUID
						}
					}
					st.ActivePromptUUID = firstActivePrompt(st.PendingPrompts)
				} else if ev.State != "" {
					st.SessionIdle = false
				}
			}
		case "stream_event":
			if ev.Event.Type == "message_start" || ev.Event.Type == "message_delta" {
				st.TotalInputTokens = maxInt64(st.TotalInputTokens, ev.Event.Usage.InputTokens)
				st.TotalOutputTokens = maxInt64(st.TotalOutputTokens, ev.Event.Usage.OutputTokens)
			}
		}
	}
	if hasUnfinishedPrompt(st.PendingPrompts) {
		st.Status = "busy"
	} else if st.SessionIdle {
		st.Status = "idle"
	}
}

func normalizeEvents(events []Event) ([]NormalizedMessage, string, Usage, float64) {
	out := []NormalizedMessage{}
	var resultText string
	var usage Usage
	var cost float64
	for _, ev := range events {
		switch ev.Type {
		case "assistant":
			msgs, text := normalizeContent(ev.Message.Content, "assistant")
			out = append(out, msgs...)
			if text != "" {
				resultText = text
			}
			usage = addUsage(usage, ev.Message.Usage)
		case "user":
			msgs, _ := normalizeContent(ev.Message.Content, "user")
			out = append(out, msgs...)
		case "result":
			if ev.Result != "" {
				resultText = ev.Result
			}
			usage = addUsage(usage, ev.Usage)
			cost += ev.CostUSD
			out = append(out, NormalizedMessage{Type: "result", Text: ev.Result, Raw: ev.Raw})
		case "system":
			out = append(out, NormalizedMessage{Type: "system", ContentType: ev.Subtype, Text: ev.State, Raw: ev.Raw})
		case "stream_event":
			if txt := streamTextDelta(ev); txt != "" {
				resultText += txt
				out = append(out, NormalizedMessage{Type: "assistant", ContentType: "text_delta", Text: txt, Raw: ev.Raw})
			}
		default:
			out = append(out, NormalizedMessage{Type: ev.Type, Raw: ev.Raw})
		}
	}
	return out, resultText, usage, cost
}

func normalizeContent(raw json.RawMessage, role string) ([]NormalizedMessage, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var text string
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []NormalizedMessage{{Type: role, Role: role, ContentType: "text", Text: s}}, s
	}
	var blocks []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return []NormalizedMessage{{Type: role, Role: role, Raw: raw}}, ""
	}
	out := make([]NormalizedMessage, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text", "thinking":
			out = append(out, NormalizedMessage{Type: role, Role: role, ContentType: b.Type, Text: b.Text})
			if b.Type == "text" {
				if text != "" {
					text += "\n"
				}
				text += b.Text
			}
		case "tool_use":
			out = append(out, NormalizedMessage{Type: "tool_use", Tool: b.Name, Input: b.Input})
		default:
			rawBlock, _ := json.Marshal(b)
			out = append(out, NormalizedMessage{Type: b.Type, Raw: rawBlock})
		}
	}
	return out, text
}

func streamTextDelta(ev Event) string {
	var delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if len(ev.Event.Delta) == 0 {
		return ""
	}
	if err := json.Unmarshal(ev.Event.Delta, &delta); err != nil {
		return ""
	}
	if delta.Type == "text_delta" {
		return delta.Text
	}
	return ""
}

func addUsage(a, b Usage) Usage {
	return Usage{
		InputTokens:              a.InputTokens + b.InputTokens,
		OutputTokens:             a.OutputTokens + b.OutputTokens,
		CacheReadInputTokens:     a.CacheReadInputTokens + b.CacheReadInputTokens,
		CacheCreationInputTokens: a.CacheCreationInputTokens + b.CacheCreationInputTokens,
	}
}

func firstActivePrompt(prompts []PendingPrompt) string {
	for _, p := range prompts {
		if p.State == PromptSent || p.State == PromptReplayed || p.State == PromptResult {
			return p.UUID
		}
	}
	return ""
}

func hasUnfinishedPrompt(prompts []PendingPrompt) bool {
	return firstActivePrompt(prompts) != ""
}

func promptStartOffset(st State) int64 {
	var offset int64
	found := false
	for _, p := range st.PendingPrompts {
		if p.State == PromptSent || p.State == PromptReplayed || p.State == PromptResult {
			if !found || p.StartOffset < offset {
				offset = p.StartOffset
				found = true
			}
		}
	}
	if found {
		return offset
	}
	var completed *PendingPrompt
	for i := range st.PendingPrompts {
		if st.PendingPrompts[i].UUID == st.LastCompletedPromptUUID {
			completed = &st.PendingPrompts[i]
			break
		}
	}
	if completed != nil {
		return completed.StartOffset
	}
	return st.LastResultOffset
}

func trimMessages(msgs []NormalizedMessage, history int) []NormalizedMessage {
	if history <= 0 || len(msgs) <= history {
		return msgs
	}
	return msgs[len(msgs)-history:]
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func parseResumeNotFound(stderr string) bool {
	return strings.Contains(stderr, "No conversation found with session ID")
}

func parseErrorAt(offset int64, err error) error {
	return apperr.New("ndjson_parse_error", fmt.Sprintf("parse output line at offset %d: %v", offset, err))
}
