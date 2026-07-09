package execjsonctl

import "encoding/json"

// Event is one line of `codex exec --json` output.
//
// The stream is a flat sequence of thread/turn/item events. Unlike Claude Code's
// stream-json protocol there is no user replay, no idle event, and no cost field.
type Event struct {
	Offset    int64
	EndOffset int64
	Raw       json.RawMessage

	Type     string
	ThreadID string
	Message  string // top-level `error` events carry the text here
	Usage    Usage
	Error    EventError
	Item     Item
	HasItem  bool
}

// Usage mirrors `turn.completed.usage`. Codex reports neither cost nor
// cache-creation tokens, and names its cache field `cached_input_tokens`.
type Usage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

type EventError struct {
	Message string `json:"message"`
}

// Item is the union of every `item.type` codex emits. Fields absent for a given
// item type stay zero; Raw always holds the original object.
type Item struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	Text             string          `json:"text"`
	Message          string          `json:"message"`
	Command          string          `json:"command"`
	AggregatedOutput string          `json:"aggregated_output"`
	ExitCode         *int            `json:"exit_code"`
	Status           string          `json:"status"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Name             string          `json:"name"`
	Raw              json.RawMessage `json:"-"`
}

type NormalizedMessage struct {
	Type        string          `json:"type"`
	Role        string          `json:"role,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	Text        string          `json:"text,omitempty"`
	Tool        string          `json:"tool,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
}

func (u Usage) add(o Usage) Usage {
	return Usage{
		InputTokens:           u.InputTokens + o.InputTokens,
		CachedInputTokens:     u.CachedInputTokens + o.CachedInputTokens,
		OutputTokens:          u.OutputTokens + o.OutputTokens,
		ReasoningOutputTokens: u.ReasoningOutputTokens + o.ReasoningOutputTokens,
	}
}

// usageMap keeps the JSON shape aligned with the ndjson harness so downstream
// consumers see a stable schema. Codex supplies no cost and no cache-creation
// tokens; both are reported as zero rather than omitted.
func usageMap(u Usage) map[string]any {
	return map[string]any{
		"input_tokens":                u.InputTokens,
		"output_tokens":               u.OutputTokens,
		"cache_read_input_tokens":     u.CachedInputTokens,
		"cache_creation_input_tokens": int64(0),
		"reasoning_output_tokens":     u.ReasoningOutputTokens,
		"total_cost_usd":              float64(0),
	}
}
