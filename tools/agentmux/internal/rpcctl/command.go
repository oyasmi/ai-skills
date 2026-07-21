package rpcctl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oyasmi/agentmux/internal/apperr"
)

// buildPiCommand appends the RPC-mode flags to the template command.
//
// pi's --session-id both resumes an existing project session and creates a new
// one under that id when none exists, so the same flag serves start and resume;
// resume is therefore just running with the same id in the same cwd. The system
// prompt is passed with --append-system-prompt unless the template already sets
// a system-prompt flag of its own.
func buildPiCommand(command, systemPrompt, sessionID string) string {
	flags := []string{"--mode", "rpc", "--session-id", shellQuote(sessionID)}
	if strings.TrimSpace(systemPrompt) != "" &&
		!hasAnyFlag(command, "--system-prompt", "--append-system-prompt") {
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

// writeRunScript wires pi's stdin to the FIFO. The `exec 3<>"$FIFO"` line keeps a
// live descriptor on the pipe so pi never sees stdin EOF (which would trigger its
// shutdown) during the gaps between our command writes.
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
		return apperr.Wrap("rpc_process_error", err, "write run script")
	}
	return nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
