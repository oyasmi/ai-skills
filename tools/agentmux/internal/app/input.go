package app

import (
	"io"
	"os"
	"strings"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

func readPromptText(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxPromptInputBytes+1))
	if err != nil {
		return "", apperr.Wrap("input_read_error", err, "read prompt text from stdin")
	}
	if len(b) > maxPromptInputBytes {
		return "", apperr.New("input_too_large", "stdin input exceeds 3 MiB limit")
	}
	return string(b), nil
}

// readPromptFile lets a caller keep a long task contract in a file instead of
// squeezing it through argv, which is where TUI harnesses get unreliable.
func readPromptFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", apperr.Wrap("input_read_error", err, "read prompt file %s", path)
	}
	defer f.Close()
	text, err := readPromptText(f)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", apperr.New("invalid_arguments", "prompt file "+path+" is empty")
	}
	return text, nil
}
