package app

import "strings"

func usage() string {
	return strings.TrimSpace(`
usage:
  agentmux template list [--json]
  agentmux list [--all] [--json]
  agentmux summon --template <template-name> [--name <instance-name>] [--cwd <path>] [--model <model>] [--command <shell-command>] [--system-prompt <text>] [--prompt <text>] [--json]
  agentmux inspect <instance-name> [--json]
  agentmux prompt <instance-name> [--text <text> | --stdin] [--key <key>] [--json]
  agentmux run --template <template-name> [--name <instance-name>] [--cwd <path>] (--prompt <text> | --prompt-file <path> | --stdin) [--timeout <duration-or-ms>] [--history <limit>] [--raw] [--json]
  agentmux capture <instance-name> [--scope current|session] [--history <limit>] [--since <cursor>] [--raw] [--json]
  agentmux wait <instance-name>... [--mode all|any] [--stable <duration-or-ms>] [--timeout <duration-or-ms>] [--json]
  agentmux attach [<instance-name>]
  agentmux halt <instance-name> [--json]
  agentmux version [--json]
`)
}

func helpForArgs(args []string) (string, bool) {
	filtered := make([]string, 0, len(args))
	hasHelp := false
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			hasHelp = true
			continue
		}
		filtered = append(filtered, arg)
	}
	if len(filtered) > 0 && filtered[0] == "help" {
		hasHelp = true
		filtered = filtered[1:]
	}
	if !hasHelp {
		return "", false
	}
	switch len(filtered) {
	case 0:
		return rootHelp(), true
	case 1:
		switch filtered[0] {
		case "template":
			return templateHelp(), true
		case "list":
			return listHelp(), true
		case "summon":
			return summonHelp(), true
		case "run":
			return runHelp(), true
		case "inspect":
			return inspectHelp(), true
		case "prompt":
			return promptHelp(), true
		case "capture":
			return captureHelp(), true
		case "wait":
			return waitHelp(), true
		case "attach":
			return attachHelp(), true
		case "halt":
			return haltHelp(), true
		case "version":
			return versionHelp(), true
		default:
			return rootHelp(), true
		}
	default:
		if filtered[0] == "template" && filtered[1] == "list" {
			return templateListHelp(), true
		}
		return rootHelp(), true
	}
}

func rootHelp() string {
	return strings.TrimSpace(`
agentmux manages isolated terminal or headless agent instances for AI orchestrators.

Usage:
  agentmux <command> [arguments]
  agentmux help [command]
  agentmux --help

Core commands:
  template list   List configured role templates
  list            List live instances and their current statuses
  summon          Create or reuse an instance
  run             Delegate one task end to end: summon, prompt, wait, and read the answer
  inspect         Query one instance's current status and metadata
  prompt          Send text or a special key to an instance
  capture         Read the latest observable output from an instance
  wait            Wait until one or several agents appear done
  attach          Attach a human terminal to an instance
  halt            Stop an instance
  version         Print the CLI version

Global flags:
  --json          Return machine-readable JSON for command output
  -h, --help      Show help for the selected command

Examples:
  agentmux template list --json
  agentmux run --template codex-cli-execjson --cwd ~/work/project --prompt "..." --json
  agentmux summon --template claude-code --name 编码助手-A --cwd ~/work/project
  agentmux summon --template claude-code --name 编码助手-A --prompt "先阅读项目并总结结构" --json
  agentmux capture 编码助手-A --history 120 --json
  echo "补充两行说明" | agentmux prompt 编码助手-A --stdin --json
  agentmux prompt 编码助手-A --text "继续" --json

Learn more:
  agentmux help summon
  agentmux help capture
  agentmux help template
`)
}

func templateHelp() string {
	return strings.TrimSpace(`
template exposes help for template-related subcommands.

Usage:
  agentmux template <subcommand> [arguments]
  agentmux template --help

Subcommands:
  list            List configured templates

Examples:
  agentmux template list
  agentmux template list --json

Learn more:
  agentmux help template list
`)
}

func templateListHelp() string {
	return strings.TrimSpace(`
template list prints the configured role templates from ~/.config/agentmux/config.yaml.

Usage:
  agentmux template list [--json]

Output:
  Text mode prints a table with template name, model, harness type, cwd, and description.
  JSON mode returns {"ok", "command", "data.templates"}.

Examples:
  agentmux template list
  agentmux template list --json
`)
}

func listHelp() string {
	return strings.TrimSpace(`
list prints the known agent instances from the local registry and reconciles their tmux state.

Usage:
  agentmux list [--all] [--json]

Flags:
  --all                     Also show tombstones: instances that have stopped
  --json                    Return JSON output

Output:
  Text mode prints a table with name, template, status, model, cwd, update time, and end reason.
  JSON mode returns {"ok", "command", "data.instances"}.

Notes:
  Use list for multi-instance status overview.
  A stopped instance is kept as a tombstone so it can still be diagnosed: status exited or lost, plus end_reason, ended_at, and any last_error the harness reported.
  Tombstones are hidden by default, never count against max_instances, and are swept after defaults.status.tombstone_ttl_ms.
  inspect keeps answering for a tombstone; prompt, capture, and wait fail with process_not_running and name the reason.

Examples:
  agentmux list
  agentmux list --all --json
`)
}

func runHelp() string {
	return strings.TrimSpace(`
run delegates one task from start to finish and returns what the agent produced.

Usage:
  agentmux run --template <template-name> [flags]

Flags:
  --template <name>         Template to summon or reuse (required)
  --name <instance-name>    Instance name; generated when omitted
  --cwd <path>              Working directory for the agent
  --model <model>           Override the template model
  --command <shell-command> Override the template command
  --system-prompt <text>    Override the template system prompt
  --prompt <text>           The task to send
  --prompt-file <path>      Read the task from a file, which is the reliable way to send a long contract
  --stdin                   Read the task from stdin
  --timeout <duration-or-ms> How long to wait for the answer, default 5m
  --history <limit>         Message limit for the returned output
  --raw                     Include raw protocol events in the returned output
  --json                    Return JSON output
  -h, --help                Show this help

Behavior:
  run is summon + prompt + wait + capture in one call, with one exit code and one payload.
  Exactly one of --prompt, --prompt-file, or --stdin is required.
  An existing instance with the same name is reused, so repeated runs continue the same session.
  Reaching --timeout is not a failure: the agent keeps working, data.timed_out is set, and whatever it produced so far is returned. Wait on the instance again to pick it up.
  The instance is left running so its work can be inspected; stop it with halt.

Examples:
  agentmux run --template codex-cli-execjson --cwd ~/work/project --prompt "修复登录重试" --json
  agentmux run --template claude-code-ndjson --name 审查-A --prompt-file ./task.md --timeout 10m --json
  cat task.md | agentmux run --template pi-rpc --stdin --json
`)
}

func summonHelp() string {
	return strings.TrimSpace(`
summon creates a new instance or reuses an existing one with the same name and template.

Usage:
  agentmux summon --template <template-name> [flags]

Required flags:
  --template <name>         Template name to resolve from config

Optional flags:
  --name <instance-name>    Reuse or create a specific instance name
  --cwd <path>              Override working directory
  --model <model>           Override template model
  --command <command>       Override template command
  --system-prompt <text>    Override template system prompt
  --prompt <text>           Send a prompt in this summon call
  --json                    Return JSON output
  -h, --help                Show this help

Behavior:
  If the named instance exists with the same template, summon reuses it.
  If the named instance exists with a different template, summon returns an error and you must use a new name.
  If --prompt is provided, summon sends the prompt for both new and reused instances.
  Reusing an instance does not mutate its stored config.

Examples:
  agentmux summon --template claude-code
  agentmux summon --template claude-code --name 编码助手-A --cwd ~/work/project
  agentmux summon --template claude-code --name 编码助手-A --prompt "继续修复测试" --json
`)
}

func inspectHelp() string {
	return strings.TrimSpace(`
inspect shows detailed metadata for one instance.

Usage:
  agentmux inspect <instance-name> [--json]

Arguments:
  <instance-name>           Target instance name

Output:
  Text mode prints key-value fields.
  JSON mode returns {"ok", "command", "instance", "status", "data"}.

Notes:
  inspect is the primary command for querying one instance's current status.
  Use inspect --json for lightweight status checks.
  JSON inspect includes persisted fields such as harness_type and the latest observed pane_title.

Examples:
  agentmux inspect 编码助手-A
  agentmux inspect 编码助手-A --json
`)
}

func promptHelp() string {
	return strings.TrimSpace(`
prompt sends text or one special key to an existing instance.

Usage:
  agentmux prompt <instance-name> [flags]

Arguments:
  <instance-name>           Target instance name

Flags:
  --text <text>             Send text to the instance
  --stdin                   Read text from stdin
  --key <key>               Send one special key
  --json                    Return JSON output
  -h, --help                Show this help

Supported keys:
  Enter, C-c, Escape, Up, Down, Tab

Notes:
  Provide at least one of --text, --stdin, or --key.
  --stdin reads all of stdin as one text payload.
  --stdin cannot be combined with --text.
  --text and --stdin submit automatically after the text is pasted.
  On title-signaling TUI harnesses, prompt then waits for the harness to visibly start working, bounded by defaults.status.prompt_ack_ms, so later status reads describe this turn instead of the previous one.
  If text appears in the input box but execution does not start, follow up with --key Enter.
  For some TUI harnesses, especially Claude Code, very long stdin payloads may be less reliable than writing instructions to a file and sending a short follow-up prompt.
  On structured harnesses only C-c is meaningful; other keys are accepted as no-ops.
  On codex-cli-execjson each prompt starts one turn process. Prompting while a turn runs fails with execjson_instance_busy, since codex cannot take input into a running turn; wait first.
  On pi-rpc, prompting while a run is in flight is accepted and queued as a follow-up, delivered after the current run finishes; C-c aborts the running turn in-band.

Examples:
  agentmux prompt 编码助手-A --text "继续" --json
  echo "补充两行说明" | agentmux prompt 编码助手-A --stdin --json
  agentmux prompt 编码助手-A --key C-c --json
`)
}

func captureHelp() string {
	return strings.TrimSpace(`
capture reads the latest observable output from an instance without waiting.

Usage:
  agentmux capture <instance-name> [flags]

Arguments:
  <instance-name>           Target instance name

Flags:
  --scope <scope>           Output scope: current or session, default current
  --history <limit>         TUI: history lines; structured: message limit (default 20, 0 means no limit)
  --since <cursor>          Return only what is new since a cursor from an earlier capture
  --raw                     Include raw protocol events and untruncated bodies
  --json                    Return JSON output
  -h, --help                Show this help

Output:
  Text mode prints the current content only. This is the cheapest way to read what an agent said.
  JSON mode returns scope, harness type, content, and harness-specific metadata.

Notes:
  capture always returns immediately.
  For TUI harnesses, current means current screen plus optional history lines.
  For structured harnesses, current means the active or most recent turn; session spans the whole recorded conversation.
  Structured harnesses emit one message per protocol event, so JSON output is capped at 20 messages and strips raw payloads by default.
  Use --raw (optionally with --history 0) for debugging; the untouched event stream is always kept in the instance output.jsonl.
  Every structured capture returns data.next_cursor; pass it back as --since to read only what arrived after it, which is how a long run is watched cheaply.
  --since overrides --scope, is not capped by the default message limit, and needs a recorded event stream, so terminal harnesses reject it.
  capture is for reading output, not for waiting or querying status by itself.
  Use inspect --json when you only need current status or pane title.
  Use wait if you need to block until the agent appears done.

Examples:
  agentmux capture 编码助手-A
  agentmux capture 编码助手-A --history 120
  agentmux capture 编码助手-A --json
  agentmux capture 编码助手-A --scope session --history 40 --json
  agentmux capture 编码助手-A --since 4096 --json
`)
}

func waitHelp() string {
	return strings.TrimSpace(`
wait blocks until the agent appears to have finished its current work without returning captured content.

Usage:
  agentmux wait <instance-name>... [flags]

Arguments:
  <instance-name>...        One or more instance names, listed before any flag

Flags:
  --mode all|any            With several instances: return when all finish, or as soon as one does. Default all
  --stable <duration-or-ms> Settle window before an idle signal is trusted, and stability window for generic detection, default 1500
  --timeout <duration-or-ms> Maximum wait time, default 30s
  --json                    Return JSON output
  -h, --help                Show this help

Output:
  Text mode prints instance name, status, elapsed time, and a timed_out marker.
  JSON mode returns timed_out, saw_busy, elapsed_ms, stability, and, for TUI harnesses, screen fields.

Notes:
  wait means "wait until the agent seems done", not "wait until the terminal is visually static".
  Reaching --timeout is not a failure: it returns ok true with status busy and data.timed_out true, so a long task is simply waited on again.
  Only a broken, lost, or exited instance makes wait fail.
  A prompt confirms that a title-signaling harness started working; until that is observed, an idle title inside the --stable window is treated as stale and ignored.
  data.saw_busy reports whether this wait actually observed the harness working.
  With several instances, data.instances reports each one, plus data.done, data.pending, and data.failed. mode=any is what makes a fan-out usable: handle whichever shard lands first instead of blocking on the slowest.
  With several instances a failure on one is reported against that instance instead of discarding the other results.
  Use inspect or list when you want to query status without blocking.
  For title-signaling harnesses such as claude-code, codex-cli, and gemini-cli, completion is inferred from pane_title idle markers.
  For claude-code-ndjson, completion is inferred from protocol events.
  For codex-cli-execjson, completion means the turn process exited; a failed turn still satisfies wait, and the reason is reported by capture --json as last_error.
  For pi-rpc, completion is inferred from the agent_settled protocol event, which fires only after retries, compaction, and any queued follow-up prompts have drained.
  For generic harnesses, completion falls back to screen stability heuristics.
  The title-signaling path polls pane metadata only and does not capture screen content.

Examples:
  agentmux wait 编码助手-A --stable 1500 --timeout 30s --json
  agentmux wait 编码助手-A --timeout 3m --json
  agentmux wait 分片-A 分片-B 分片-C --mode any --timeout 5m --json
  agentmux wait 编码助手-A --stable 2s
`)
}

func attachHelp() string {
	return strings.TrimSpace(`
attach lets a human attach a terminal to an instance's tmux session.

Usage:
  agentmux attach [<instance-name>]

Arguments:
  <instance-name>           Optional target instance name

Behavior:
  If an instance name is provided, attach connects directly.
  If no instance name is provided and stdin is a TTY, attach prompts for selection.
  If no instance name is provided and stdin is not a TTY, attach returns an error.

Examples:
  agentmux attach 编码助手-A
  agentmux attach
`)
}

func haltHelp() string {
	return strings.TrimSpace(`
halt stops an instance gracefully by default. It sends Ctrl-C, waits up to the timeout,
and falls back to killing the tmux session if the instance is still running.

Usage:
  agentmux halt <instance-name> [--timeout <duration-or-ms>] [--immediately] [--json]

Arguments:
  <instance-name>           Target instance name

Flags:
  --timeout <duration-or-ms> Graceful shutdown timeout, default 5s
  --immediately             Skip graceful shutdown and kill the tmux session directly

Examples:
  agentmux halt 编码助手-A
  agentmux halt 编码助手-A --timeout 8s
  agentmux halt 编码助手-A --immediately
  agentmux halt 编码助手-A --json
`)
}

func versionHelp() string {
	return strings.TrimSpace(`
version prints the current agentmux CLI version, and in JSON mode what this build can do.

Usage:
  agentmux version [--json]

Output:
  Text mode prints the version string only.
  JSON mode also returns data.commands, data.harness_types, and data.features, so a caller can check for a capability instead of probing commands and interpreting failures.

Examples:
  agentmux version
  agentmux version --json
`)
}
