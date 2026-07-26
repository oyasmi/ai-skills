package execjsonctl

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
)

const (
	// maxBriefText caps a single message body in the default projection. The
	// answer an orchestrator needs is in `content`; `messages` is a trace, and
	// an unclamped trace can dwarf the answer by orders of magnitude.
	maxBriefText = 2000
	// maxBriefInput caps a tool call's serialized arguments, which routinely
	// carry whole file bodies.
	maxBriefInput = 1000
)

// briefMessages drops the original protocol payloads and clamps long bodies.
// Callers that need full fidelity ask for it with --raw; the untouched stream
// is always still on disk in output.jsonl.
func briefMessages(msgs []NormalizedMessage) []NormalizedMessage {
	out := make([]NormalizedMessage, 0, len(msgs))
	for _, m := range msgs {
		m.Raw = nil
		m.Text = truncate(m.Text, maxBriefText)
		if len(m.Input) > maxBriefInput {
			m.Input = json.RawMessage(`"(` + strconv.Itoa(len(m.Input)) + ` bytes elided; use --raw)"`)
		}
		out = append(out, m)
	}
	return out
}

// truncate clamps s to a byte budget, cutting on a rune boundary so the result
// stays valid UTF-8.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := 0
	for i := range s {
		if i > max {
			break
		}
		cut = i
	}
	return s[:cut] + "…(truncated)"
}

// captureFrom resolves where a capture should start reading: an explicit
// cursor when the caller passed one, otherwise the scope's natural origin.
func captureFrom(opts capture.Options, origin func() int64) (int64, error) {
	if strings.TrimSpace(opts.Since) != "" {
		return capture.ParseCursor(opts.Since)
	}
	return origin(), nil
}
