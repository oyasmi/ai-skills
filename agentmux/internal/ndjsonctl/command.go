package ndjsonctl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oyasmi/agentmux/internal/apperr"
)

func buildClaudeCommand(command, systemPrompt, sessionID string, resume bool) string {
	flags := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--replay-user-messages"}
	if resume {
		flags = append(flags, "--resume", shellQuote(sessionID))
	} else {
		flags = append(flags, "--session-id", shellQuote(sessionID))
	}
	if strings.TrimSpace(systemPrompt) != "" && !hasAnyFlag(command, "--system-prompt", "--system-prompt-file", "--append-system-prompt", "--append-system-prompt-file") {
		flags = append(flags, "--append-system-prompt", shellQuote(systemPrompt))
	}
	return strings.TrimSpace(command) + " " + strings.Join(flags, " ")
}

func hasAnyFlag(command string, flags ...string) bool {
	fields := strings.Fields(command)
	for _, f := range fields {
		for _, want := range flags {
			if f == want || strings.HasPrefix(f, want+"=") {
				return true
			}
		}
	}
	return false
}

func writeRunScript(dir, command string) error {
	script := fmt.Sprintf(`#!/bin/sh
set -eu
DIR=%s
FIFO="$DIR/%s"
OUT="$DIR/%s"
ERR="$DIR/%s"
mkdir -p "$DIR"
if [ ! -p "$FIFO" ]; then
  rm -f "$FIFO"
  mkfifo "$FIFO"
fi
exec 3<>"$FIFO"
exec %s < "$FIFO" >> "$OUT" 2>> "$ERR"
`, shellQuote(dir), inputFIFOName, outputJSONLName, stderrLogName, command)
	path := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return apperr.Wrap("ndjson_process_error", err, "write run script")
	}
	return nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
