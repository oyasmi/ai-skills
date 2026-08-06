package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/service"
)

// logMessage is intentionally local to the CLI. Each structured controller
// has its own normalized message type; JSON keeps the human renderer stable
// without coupling those protocol packages together.
type logMessage struct {
	Type        string          `json:"type"`
	Role        string          `json:"role,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	Text        string          `json:"text,omitempty"`
	Tool        string          `json:"tool,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
}

func runLogs(ctx context.Context, svc service.Service, name string, follow bool, stdout, stderr io.Writer) int {
	inst, snap, err := svc.Transcript(ctx, name, "")
	if err != nil {
		return writeErr(stdout, stderr, false, "logs", name, err)
	}
	renderLogBatch(stdout, snap)
	if !follow {
		return 0
	}

	cursor := transcriptCursor(snap)
	for {
		if inst.Ended() {
			return 0
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
		nextInst, nextSnap, err := svc.Transcript(ctx, name, cursor)
		if err != nil {
			return writeErr(stdout, stderr, false, "logs", name, err)
		}
		renderLogBatch(stdout, nextSnap)
		inst = nextInst
		cursor = transcriptCursor(nextSnap)
	}
}

func transcriptCursor(snap capture.Snapshot) string {
	if cursor, ok := snap.Extra["next_cursor"].(string); ok {
		return cursor
	}
	return ""
}

func renderLogBatch(w io.Writer, snap capture.Snapshot) {
	messages := decodeLogMessages(snap.Extra["messages"])
	if len(messages) == 0 {
		if strings.TrimSpace(snap.Content) != "" {
			fmt.Fprintln(w, snap.Content)
		}
		return
	}
	for _, message := range messages {
		label := logLabel(message)
		text := strings.TrimRight(message.Text, "\n")
		if text == "" && len(message.Input) > 0 {
			text = string(message.Input)
		}
		if text == "" && len(message.Raw) > 0 {
			text = string(message.Raw)
		}
		if text == "" {
			fmt.Fprintf(w, "[%s]\n", label)
			continue
		}
		fmt.Fprintf(w, "[%s]\n%s\n", label, text)
	}
}

func decodeLogMessages(value any) []logMessage {
	if value == nil {
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var messages []logMessage
	if json.Unmarshal(b, &messages) != nil {
		return nil
	}
	return messages
}

func logLabel(message logMessage) string {
	if message.ContentType == "thinking" {
		return "THINKING"
	}
	switch message.Type {
	case "user":
		return "USER"
	case "assistant":
		return "ASSISTANT"
	case "tool_use":
		if message.Tool != "" {
			return "TOOL " + message.Tool
		}
		return "TOOL"
	case "toolResult":
		return "TOOL RESULT"
	case "result":
		return "RESULT"
	case "system":
		return "STATUS"
	default:
		return strings.ToUpper(firstNonEmpty(message.Type, "EVENT"))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
