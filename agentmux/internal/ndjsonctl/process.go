package ndjsonctl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

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

func ensureFIFO(path string) error {
	if st, err := os.Stat(path); err == nil {
		if st.Mode()&os.ModeNamedPipe != 0 {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return apperr.Wrap("ndjson_process_error", err, "replace non-fifo")
		}
	} else if !os.IsNotExist(err) {
		return apperr.Wrap("ndjson_process_error", err, "stat fifo")
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		return apperr.Wrap("ndjson_process_error", err, "create fifo")
	}
	return nil
}

func writeFIFO(ctx context.Context, path string, data []byte, timeout time.Duration) error {
	type result struct {
		err error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer f.Close()
		_, err = f.Write(data)
		ch <- result{err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return apperr.New("ndjson_fifo_broken", "timed out opening claude input fifo")
	case res := <-ch:
		if res.err != nil {
			return apperr.Wrap("ndjson_fifo_broken", res.err, "write claude input fifo")
		}
		return nil
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func saveProcessMeta(path string, meta processMeta) error {
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return apperr.Wrap("ndjson_process_error", err, "marshal process meta")
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return apperr.Wrap("ndjson_process_error", err, "write process meta")
	}
	return nil
}

func envList(base map[string]string, extra map[string]string) []string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			env[k] = v
		}
	}
	for k, v := range base {
		env[k] = v
	}
	for k, v := range extra {
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func tailFile(path string, maxBytes int64) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if int64(len(b)) > maxBytes {
		b = b[int64(len(b))-maxBytes:]
	}
	return strings.TrimSpace(string(b))
}

func sleepPoll(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func usageMap(u Usage, cost float64) map[string]any {
	return map[string]any{
		"input_tokens":                u.InputTokens,
		"output_tokens":               u.OutputTokens,
		"cache_creation_input_tokens": u.CacheCreationInputTokens,
		"cache_read_input_tokens":     u.CacheReadInputTokens,
		"total_cost_usd":              cost,
	}
}
