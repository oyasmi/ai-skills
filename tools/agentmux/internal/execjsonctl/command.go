package execjsonctl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

// rejectedFlags are flags agentmux either owns or knows codex exec cannot accept.
var rejectedFlags = map[string]string{
	"--json":                "agentmux injects --json itself",
	"-o":                    "agentmux owns the output stream",
	"--output-last-message": "agentmux owns the output stream",
	"--ask-for-approval":    "codex exec does not accept --ask-for-approval; use --sandbox instead",
	"-a":                    "codex exec does not accept -a; use --sandbox instead",
	"--ephemeral":           "--ephemeral disables session persistence, which makes multi-turn resume impossible",
}

var rejectedSubcommands = map[string]string{
	"resume": "agentmux adds `resume` itself when continuing a thread",
	"review": "codex exec review is not supported by this harness",
}

var allowedBoolFlags = map[string]bool{
	"--skip-git-repo-check":                      true,
	"--dangerously-bypass-approvals-and-sandbox": true,
}

var allowedValueFlags = map[string]bool{
	"-s":        true,
	"--sandbox": true,
	"-C":        true,
	"--cd":      true,
	"--add-dir": true,
	"--color":   true,
	"-m":        true,
	"--model":   true,
}

// validateCommand enforces that the template command is a plain `codex exec`
// prefix carrying only parent-level flags. Everything after it -- the `resume`
// subcommand, `--json`, and the `-` stdin marker -- is appended by agentmux.
//
// The scan is deliberately token-based rather than a real shell parse: it is
// better to reject an exotic-but-valid command than to silently mis-handle one.
func validateCommand(command string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return apperr.New("config_invalid", "codex-cli-execjson command must not be empty")
	}
	if i := strings.IndexAny(trimmed, "|><&;`\n"); i >= 0 {
		return apperr.New("config_invalid", fmt.Sprintf("codex-cli-execjson command must not use shell redirection, pipes, or command chaining (found %q)", trimmed[i]))
	}
	if strings.Contains(trimmed, "$(") {
		return apperr.New("config_invalid", "codex-cli-execjson command must not use command substitution")
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 2 || filepath.Base(fields[0]) != "codex" || fields[1] != "exec" {
		return apperr.New("config_invalid", "codex-cli-execjson command must start with `codex exec`")
	}
	for i := 2; i < len(fields); i++ {
		f := fields[i]
		name := f
		if eq := strings.IndexByte(name, '='); eq > 0 {
			name = name[:eq]
		}
		if reason, bad := rejectedFlags[name]; bad {
			return apperr.New("config_invalid", fmt.Sprintf("codex-cli-execjson command must not contain %s: %s", name, reason))
		}
		if reason, bad := rejectedSubcommands[name]; bad {
			return apperr.New("config_invalid", fmt.Sprintf("codex-cli-execjson command must not contain `%s`: %s", name, reason))
		}
		if allowedBoolFlags[name] {
			if strings.Contains(f, "=") {
				return apperr.New("config_invalid", fmt.Sprintf("codex-cli-execjson flag %s must not use a value", name))
			}
			continue
		}
		if allowedValueFlags[name] {
			if strings.Contains(f, "=") {
				continue
			}
			i++
			if i >= len(fields) || strings.HasPrefix(fields[i], "-") {
				return apperr.New("config_invalid", fmt.Sprintf("codex-cli-execjson flag %s requires a value", name))
			}
			continue
		}
		if strings.HasPrefix(f, "-") {
			return apperr.New("config_invalid", fmt.Sprintf("codex-cli-execjson command contains unsupported flag %s", name))
		}
		return apperr.New("config_invalid", fmt.Sprintf("codex-cli-execjson command must not contain positional argument %q; prompts are supplied by agentmux", f))
	}
	return nil
}

// buildTurnCommand assembles the command line for one turn.
//
// Flag position is load-bearing: `resume` is a subcommand that rejects the
// parent's flags (`--sandbox`, `--cd`, ...), so it must sit after the user's
// prefix but before `--json`. The prompt travels through stdin via `-` so that
// neither ARG_MAX nor shell quoting can corrupt it.
func buildTurnCommand(prefix, threadID string) string {
	parts := []string{strings.TrimSpace(prefix)}
	if threadID != "" {
		parts = append(parts, "resume", shellQuote(threadID))
	}
	parts = append(parts, "--json", "-")
	return strings.Join(parts, " ")
}

// writeRunScript emits the per-turn launcher. It deliberately does not `exec`
// the command: the shell stays alive to record the exit code, which is the only
// way to tell "crashed before emitting turn.failed" from "still running".
func writeRunScript(dir, path, command string, turn int) error {
	script := fmt.Sprintf(`#!/bin/sh
set -u
DIR=%s
%s < "$DIR/turns/%03d.prompt" >> "$DIR/%s" 2>> "$DIR/%s"
code=$?
printf '%%s' "$code" > "$DIR/turns/%03d.exit"
exit "$code"
`, shellQuote(dir), command, turn, outputJSONLName, stderrLogName, turn)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return apperr.Wrap("execjson_process_error", err, "write run script")
	}
	return nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func promptPath(dir string, turn int) string {
	return filepath.Join(dir, "turns", fmt.Sprintf("%03d.prompt", turn))
}

func runScriptPath(dir string, turn int) string {
	return filepath.Join(dir, "turns", fmt.Sprintf("%03d.run.sh", turn))
}

func exitCodePath(dir string, turn int) string {
	return filepath.Join(dir, "turns", fmt.Sprintf("%03d.exit", turn))
}
