package rpcctl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"

	"github.com/oyasmi/agentmux/internal/apperr"
)

func readEvents(path string, from int64) ([]Event, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, from, nil
		}
		return nil, from, apperr.Wrap("rpc_parse_error", err, "open output")
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, from, apperr.Wrap("rpc_parse_error", err, "seek output")
	}
	events := []Event{}
	next := from
	r := bufio.NewReader(f)
	for {
		lineStart := next
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if line[len(line)-1] != '\n' {
				// Ignore an incomplete trailing line until the writer finishes it.
				break
			}
			next += int64(len(line))
			lineEnd := next
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) == 0 {
				continue
			}
			ev, perr := parseEvent(lineStart, lineEnd, trimmed)
			if perr != nil {
				return events, next, perr
			}
			events = append(events, ev)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return events, next, apperr.Wrap("rpc_parse_error", err, "read output")
		}
	}
	return events, next, nil
}

func parseEvent(offset, endOffset int64, line []byte) (Event, error) {
	var base struct {
		Type    string           `json:"type"`
		ID      string           `json:"id"`
		Command string           `json:"command"`
		Success bool             `json:"success"`
		Error   string           `json:"error"`
		Method  string           `json:"method"`
		Message AssistantMessage `json:"message"`
	}
	if err := json.Unmarshal(line, &base); err != nil {
		return Event{}, apperr.Wrap("rpc_parse_error", err, "parse output line at offset %d", offset)
	}
	return Event{
		Offset:    offset,
		EndOffset: endOffset,
		Raw:       append(json.RawMessage(nil), line...),
		Type:      base.Type,
		ID:        base.ID,
		Command:   base.Command,
		Success:   base.Success,
		Error:     base.Error,
		Method:    base.Method,
		Message:   base.Message,
	}, nil
}

// dialogMethods are the extension UI requests that block the agent until the
// client answers. Fire-and-forget methods (notify, setStatus, ...) never block
// and are ignored here.
var dialogMethods = map[string]bool{
	"select":  true,
	"confirm": true,
	"input":   true,
	"editor":  true,
}

// applyEvents folds a batch of freshly read events into state. It is the whole
// pi-rpc protocol state machine: prompt correlation, run lifecycle, usage
// accounting, and detection of extension dialogs that need auto-dismissal.
func applyEvents(st *State, events []Event) {
	if len(events) > 0 {
		st.LastEventAt = nowUTC()
	}
	for _, ev := range events {
		st.LastReadOffset = max(st.LastReadOffset, ev.EndOffset)
		switch ev.Type {
		case "response":
			if ev.Command != "prompt" {
				continue
			}
			for i := range st.PendingPrompts {
				if st.PendingPrompts[i].ID != ev.ID {
					continue
				}
				if st.PendingPrompts[i].State != PromptSent {
					break
				}
				if ev.Success {
					st.PendingPrompts[i].State = PromptAccepted
				} else {
					st.PendingPrompts[i].State = PromptFailed
					st.LastError = ev.Error
				}
				break
			}

		case "agent_start":
			st.AgentRunActive = true
			st.InterruptedAt = zeroTime

		case "agent_settled":
			// A run settles only after retries, compaction retries, and all queued
			// follow-ups have drained, so every accepted prompt is now complete.
			// Count one conversation turn per completed user prompt here so turns
			// aligns with claude-code-ndjson (one per result) and codex-cli-execjson
			// (one per turn process). pi's own turn_end fires per agent-loop step
			// (thinking, tool call, tool result), which would wildly inflate turns.
			st.AgentRunActive = false
			st.ResumeAvailable = true
			st.InterruptedAt = zeroTime
			for i := range st.PendingPrompts {
				if st.PendingPrompts[i].State == PromptAccepted || st.PendingPrompts[i].State == PromptSent {
					st.PendingPrompts[i].State = PromptDone
					st.TotalTurns++
				}
			}

		case "turn_end":
			// A pi "turn" is one agent-loop step, not a conversation turn; only its
			// usage is meaningful here. Turn counting happens on agent_settled.
			st.ResumeAvailable = true
			// syncState can intentionally replay events from the beginning of an
			// unfinished prompt. Offsets make billing fields exactly-once even
			// though the rest of the state machine is replay-tolerant. Synthetic
			// zero-offset events used by unit tests are treated as fresh events.
			if ev.Message.Role == "assistant" && (ev.EndOffset <= 0 || ev.EndOffset > st.LastUsageOffset) {
				accumulateUsage(st, ev.Message.Usage)
				if ev.EndOffset > st.LastUsageOffset {
					st.LastUsageOffset = ev.EndOffset
				}
			}

		case "extension_ui_request":
			if dialogMethods[ev.Method] && ev.ID != "" && !hasUIRequest(st.UIRequests, ev.ID) {
				st.UIRequests = append(st.UIRequests, UIRequest{ID: ev.ID, Method: ev.Method})
			}
		}
	}

	switch {
	case st.Status == "exited":
		// terminal; leave as-is
	case hasUnfinishedPrompt(st.PendingPrompts) || st.AgentRunActive:
		st.Status = "busy"
	default:
		st.Status = "idle"
	}
}

func accumulateUsage(st *State, u Usage) {
	st.TotalInputTokens += u.Input
	st.TotalOutputTokens += u.Output
	st.TotalCacheReadTokens += u.CacheRead
	st.TotalCacheWriteTokens += u.CacheWrite
	st.TotalCostUSD += u.Cost.Total
}

func hasUIRequest(reqs []UIRequest, id string) bool {
	for _, r := range reqs {
		if r.ID == id {
			return true
		}
	}
	return false
}

// unhandledDialogIDs returns dialog request ids still awaiting a cancellation.
func unhandledDialogIDs(st State) []string {
	var out []string
	for _, r := range st.UIRequests {
		if !r.Handled {
			out = append(out, r.ID)
		}
	}
	return out
}

func markUIHandled(st *State, ids []string) {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for i := range st.UIRequests {
		if set[st.UIRequests[i].ID] {
			st.UIRequests[i].Handled = true
		}
	}
}

// normalizeEvents turns the raw stream into a capture-friendly message list plus
// an aggregate usage figure over the assistant turns in the window.
func normalizeEvents(events []Event) ([]NormalizedMessage, string, Usage) {
	out := []NormalizedMessage{}
	var finalText string
	var usage Usage
	for _, ev := range events {
		if ev.Type != "message_end" {
			continue
		}
		msg := ev.Message
		switch msg.Role {
		case "assistant":
			msgs, text := normalizeContent(msg.Content, "assistant")
			out = append(out, msgs...)
			if text != "" {
				finalText = text
			}
			usage = addUsage(usage, msg.Usage)
		case "user":
			msgs, _ := normalizeContent(msg.Content, "user")
			out = append(out, msgs...)
		case "toolResult":
			msgs, _ := normalizeContent(msg.Content, "toolResult")
			out = append(out, msgs...)
		}
	}
	return out, finalText, usage
}

// normalizeContent decodes pi's content blocks. pi uses {type:"text",text},
// {type:"thinking",thinking}, and {type:"toolCall",name,arguments}; a plain
// string is also accepted for user messages.
func normalizeContent(raw json.RawMessage, role string) ([]NormalizedMessage, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []NormalizedMessage{{Type: role, Role: role, ContentType: "text", Text: s}}, s
	}
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return []NormalizedMessage{{Type: role, Role: role, Raw: raw}}, ""
	}
	out := make([]NormalizedMessage, 0, len(blocks))
	var text string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, NormalizedMessage{Type: role, Role: role, ContentType: "text", Text: b.Text})
			if text != "" {
				text += "\n"
			}
			text += b.Text
		case "thinking":
			out = append(out, NormalizedMessage{Type: role, Role: role, ContentType: "thinking", Text: b.Thinking})
		case "toolCall":
			out = append(out, NormalizedMessage{Type: "tool_use", Tool: b.Name, Input: b.Arguments})
		default:
			rawBlock, _ := json.Marshal(b)
			out = append(out, NormalizedMessage{Type: b.Type, Raw: rawBlock})
		}
	}
	return out, text
}

func trimMessages(msgs []NormalizedMessage, history int) []NormalizedMessage {
	if history <= 0 || len(msgs) <= history {
		return msgs
	}
	return msgs[len(msgs)-history:]
}
