package ndjsonctl

import "encoding/json"

type Event struct {
	Offset    int64
	EndOffset int64
	Raw       json.RawMessage

	Type    string
	Subtype string
	State   string
	UUID    string

	SessionID string
	Result    string
	IsError   bool
	Usage     Usage
	CostUSD   float64
	Message   Message
	Event     StreamEvent
}

type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Usage   Usage           `json:"usage"`
	Model   string          `json:"model"`
}

type StreamEvent struct {
	Type    string          `json:"type"`
	Delta   json.RawMessage `json:"delta"`
	Usage   Usage           `json:"usage"`
	Message Message         `json:"message"`
}

type UserMessage struct {
	Type    string `json:"type"`
	UUID    string `json:"uuid"`
	Message struct {
		Role    string        `json:"role"`
		Content []TextContent `json:"content"`
	} `json:"message"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
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
