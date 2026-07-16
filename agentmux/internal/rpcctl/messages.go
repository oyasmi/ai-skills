package rpcctl

import "encoding/json"

// Event is one decoded line from pi's RPC stdout stream (output.jsonl).
//
// pi emits two shapes on that stream: command responses (type "response",
// carrying the request id we correlate against) and agent events (agent_start,
// agent_settled, message_end, turn_end, extension_ui_request, ...). Only the
// fields the controller acts on are decoded; the raw line is retained so
// capture can surface anything else verbatim.
type Event struct {
	Offset    int64
	EndOffset int64
	Raw       json.RawMessage

	Type string

	// Response fields (type == "response").
	ID      string
	Command string
	Success bool
	Error   string

	// Extension UI request fields (type == "extension_ui_request").
	Method string

	// Message lifecycle fields (message_start / message_end / turn_end).
	Message AssistantMessage
}

// AssistantMessage mirrors the subset of pi's AgentMessage the controller needs:
// role discrimination, token usage, and text/tool content for capture.
type AssistantMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model"`
	Usage   Usage           `json:"usage"`
}

// Usage is pi's per-message token accounting. cost.total is the authoritative
// dollar figure pi computes; the token fields feed capture's usage summary.
type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Cost       struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

// promptCommand is the RPC command written to pi's stdin FIFO for a user prompt.
// streamingBehavior "followUp" is a no-op when the agent is idle (pi ignores it
// unless a run is in flight) and otherwise queues the message to run after the
// current run drains, so a single unconditional value covers both cases.
type promptCommand struct {
	ID                string `json:"id"`
	Type              string `json:"type"`
	Message           string `json:"message"`
	StreamingBehavior string `json:"streamingBehavior"`
}

// controlCommand is a parameterless RPC command (abort).
type controlCommand struct {
	Type string `json:"type"`
}

// uiCancel dismisses an extension dialog so a headless run never blocks waiting
// for a user that will never answer.
type uiCancel struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Cancelled bool   `json:"cancelled"`
}

// NormalizedMessage is the capture-facing view of one conversation element.
type NormalizedMessage struct {
	Type        string          `json:"type"`
	Role        string          `json:"role,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	Text        string          `json:"text,omitempty"`
	Tool        string          `json:"tool,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
}

func addUsage(a, b Usage) Usage {
	a.Input += b.Input
	a.Output += b.Output
	a.CacheRead += b.CacheRead
	a.CacheWrite += b.CacheWrite
	a.Cost.Total += b.Cost.Total
	return a
}
