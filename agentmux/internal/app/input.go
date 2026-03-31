package app

import (
	"io"

	"github.com/oyasmi/agentmux/internal/apperr"
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
