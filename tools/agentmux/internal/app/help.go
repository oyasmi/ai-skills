package app

import "strings"

func usage() string {
	return strings.TrimSpace(`
usage:
  agentmux template list [--json]
  agentmux list [--all] [--json]
  agentmux summon --template <template-name> [--name <instance-name>] [--cwd <path>] [--model <model>] [--effort <level>] [--command <shell-command>] [--system-prompt <text>] [--prompt <text>] [--json]
  agentmux inspect <instance-name> [--json]
  agentmux prompt <instance-name> [--text <text> | --stdin] [--key <key>] [--json]
  agentmux run --template <template-name> [--name <instance-name>] [--cwd <path>] (--prompt <text> | --prompt-file <path> | --stdin) [--timeout <duration-or-ms>] [--history <limit>] [--trace] [--raw] [--detach] [--json]
  agentmux capture <instance-name> [--scope current|session] [--history <limit>] [--since <cursor> | --new] [--trace] [--raw] [--json]
  agentmux wait <instance-name>... [--mode all|any] [--stable <duration-or-ms>] [--timeout <duration-or-ms>] [--collect] [--json]
  agentmux attach [<instance-name>]
  agentmux halt <instance-name> [--json]
  agentmux doctor [--json]
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
		case "doctor":
			return doctorHelp(), true
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
  doctor          Check environment health: binary, PATH, config, templates, tmux
  version         Print the CLI version

Global flags:
  --json          Return machine-readable JSON for command output
  -h, --help      Show help for the selected command

Examples:
  agentmux doctor --json
  agentmux template list --json
  agentmux run --template builder --cwd ~/work/project --prompt "..." --json
  agentmux run --template planner --cwd ~/work/project --prompt "..." --timeout 15m --json
  agentmux summon --template builder --name 编码助手-A --cwd ~/work/project
  agentmux summon --template builder --name 编码助手-A --prompt "先阅读项目并总结结构" --json
  agentmux capture 编码助手-A --history 120 --json
  echo "补充两行说明" | agentmux prompt 编码助手-A --stdin --json
  agentmux prompt 编码助手-A --text "继续" --json

Learn more:
  agentmux help doctor
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
  Text mode prints a table with template name, model, effort, harness type, cwd, and the first line of the description.
  JSON mode returns {"ok", "command", "data.templates"}, with the full multi-line description.

Notes:
  A template is a role, not a harness: its description says which situations it is for and how to drive it correctly, while harness_type and command are the technical detail behind it.
  model and effort together are the role's strength dial. effort is one of low, medium, high, xhigh, max, plus minimal and off where the harness has them; agentmux translates it per harness (claude --effort, pi --thinking, codex -c model_reasoning_effort=) and clamps into a narrower vocabulary rather than refusing the role.
  Read the full description before delegating: text mode truncates it to one line, so use --json when choosing a role.

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
  --effort <level>          Override how hard the model thinks: low, medium, high, xhigh, max, plus minimal and off where the harness has them
  --command <shell-command> Override the template command
  --system-prompt <text>    Override the template system prompt
  --prompt <text>           The task to send
  --prompt-file <path>      Read the task from a file, which is the reliable way to send a long contract
  --stdin                   Read the task from stdin
  --timeout <duration-or-ms> How long to wait for the answer, default 5m
  --history <limit>         Structured only: also implies --trace, with this as the message limit
  --trace                   Include the per-protocol-event message trace; off by default, since data.content already carries the answer
  --raw                     Include raw protocol events in the returned output; also implies --trace
  --detach                  Send the prompt and return immediately, without waiting or capturing
  --json                    Return JSON output
  -h, --help                Show this help

Behavior:
  run is summon + prompt + wait + capture in one call, with one exit code and one payload.
  Exactly one of --prompt, --prompt-file, or --stdin is required.
  An existing instance with the same name is reused, so repeated runs continue the same session.
  If the reused instance is still busy with earlier work, run waits for it to clear before sending this task's prompt, spending part of --timeout on that wait; data.queued_ms in JSON output reports how much. Exhausting --timeout while still busy fails with instance_busy instead of sending into unrelated work.
  Reaching --timeout after the prompt was sent is not a failure: the agent keeps working, data.timed_out is set, and whatever it produced so far is returned. Wait on the instance again to pick it up.
  The instance is left running so its work can be inspected; stop it with halt.
  --detach still waits out a busy reused instance first (--timeout still governs that), but returns as soon as this task's prompt is sent; data.timed_out and data.content are not produced, data.detached is true instead. This is what makes a parallel fan-out cheap: summon with --detach for each shard, then a single wait --collect --mode any picks up whichever finishes first.
  --detach cannot be combined with --history, --trace, or --raw, since nothing is captured until a later capture or wait --collect call.
  If another active instance already points at the same cwd, data.warnings includes "cwd_shared:<that-instance-name>"; see agentmux help summon.

Examples:
  agentmux run --template builder --cwd ~/work/project --prompt "修复登录重试" --json
  agentmux run --template reviewer --name 审查-A --prompt-file ./task.md --timeout 10m --json
  agentmux run --template builder --name 硬骨头-A --effort xhigh --model opus --prompt-file ./task.md --timeout 20m --json
  cat task.md | agentmux run --template scout --stdin --json
  agentmux run --template builder --name 分片-A --cwd /wt/a --prompt "..." --detach --json
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
  --effort <level>          Override how hard the model thinks: low, medium, high, xhigh, max, plus minimal and off where the harness has them
  --command <command>       Override template command
  --system-prompt <text>    Override template system prompt
  --prompt <text>           Send a prompt in this summon call
  --json                    Return JSON output
  -h, --help                Show this help

Behavior:
  If the named instance exists with the same template, summon reuses it.
  If the named instance exists with a different template, summon returns an error and you must use a new name.
  If --prompt is provided, summon sends the prompt for both new and reused instances.
  Reusing an instance does not mutate its stored config, so --model and --effort are ignored on reuse; use a new name to change a role's strength.
  --model and --effort become the harness's own flags, appended to the template command: claude and pi take --model, codex takes --model plus -c model_reasoning_effort=<level>, and effort is claude --effort or pi --thinking. A command that already sets one of those itself, or positions $MODEL or $EFFORT by hand, is left alone. inspect shows the command that was actually launched.
  If another active instance already points at the same cwd, data.warnings includes "cwd_shared:<that-instance-name>". This does not block the summon: agentmux isolates agent processes, not files, so two writers sharing a checkout will race on the same working tree and Git state, but a read-only reviewer sharing a cwd with a writer is legitimate. Give parallel writers their own worktree and --cwd instead.

Examples:
  agentmux summon --template builder
  agentmux summon --template builder --name 编码助手-A --cwd ~/work/project
  agentmux summon --template builder --name 编码助手-A --effort high --prompt "继续修复测试" --json
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
  --wait-if-busy <duration-or-ms> Wait this long for a busy instance to finish its current work before sending, default 0 (send immediately)
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
  Without --wait-if-busy, sending to a busy instance either fails outright (codex-cli-execjson: execjson_instance_busy) or is accepted and queued behind whatever is already running (claude-code-ndjson, pi-rpc). --wait-if-busy makes every harness behave the same way: wait for the current work to clear, then send. data.queued_ms in JSON output reports how much of that budget was actually spent waiting.
  Exhausting --wait-if-busy without the instance going idle fails with instance_busy instead of sending.

Examples:
  agentmux prompt 编码助手-A --text "继续" --json
  echo "补充两行说明" | agentmux prompt 编码助手-A --stdin --json
  agentmux prompt 编码助手-A --key C-c --json
  agentmux prompt 编码助手-A --text "下一步任务" --wait-if-busy 2m --json
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
  --history <limit>         TUI: history lines. Structured: also implies --trace, with this as the message limit (0 means no limit)
  --since <cursor>          Return only what is new since a cursor from an earlier capture; on a structured harness this also implies --trace
  --new                     Like --since, but the cursor is remembered and advanced for you; cannot combine with --since
  --trace                   Include the per-protocol-event message trace, capped at 20 unless --history overrides it
  --raw                     Include raw protocol events and untruncated bodies; also implies --trace
  --json                    Return JSON output
  -h, --help                Show this help

Output:
  Text mode prints the current content only. This is the cheapest way to read what an agent said.
  JSON mode returns scope, harness type, content, and harness-specific metadata.

Notes:
  capture always returns immediately.
  For TUI harnesses, current means current screen plus optional history lines.
  For structured harnesses, current means the active or most recent turn; session spans the whole recorded conversation.
  data.content already carries the answer: a structured harness emits one message per protocol event, so by default data.messages is omitted entirely rather than returned capped. Ask for it with --trace, --raw, an explicit --history, or --since (each of those is already a deliberate request for event-level detail).
  Use --raw (optionally with --history 0) for debugging; the untouched event stream is always kept in the instance output.jsonl.
  Every structured capture returns data.next_cursor; pass it back as --since to read only what arrived after it, which is how a long run is watched cheaply.
  --new does the same thing without a cursor to carry: the instance remembers where the last --new call left off and starts from there, so a plain "capture --new --json" in a loop is enough to watch a long run. The first --new call on an instance behaves like an ordinary capture.
  --since and --new both override --scope, are not capped by the default message limit, and need a recorded event stream, so terminal harnesses reject them.
  capture is for reading output, not for waiting or querying status by itself.
  Use inspect --json when you only need current status or pane title.
  Use wait if you need to block until the agent appears done.

Examples:
  agentmux capture 编码助手-A
  agentmux capture 编码助手-A --history 120
  agentmux capture 编码助手-A --json
  agentmux capture 编码助手-A --trace --json
  agentmux capture 编码助手-A --scope session --history 40 --json
  agentmux capture 编码助手-A --since 4096 --json
  agentmux capture 编码助手-A --new --json
`)
}

func waitHelp() string {
	return strings.TrimSpace(`
wait blocks until the agent appears to have finished its current work, and with --collect reads back what it produced.

Usage:
  agentmux wait <instance-name>... [flags]

Arguments:
  <instance-name>...        One or more instance names, listed before any flag

Flags:
  --mode all|any            With several instances: return when all finish, or as soon as one does. Default all
  --stable <duration-or-ms> Settle window before an idle signal is trusted, and stability window for generic detection, default 1500
  --timeout <duration-or-ms> Maximum wait time, default 30s
  --collect                 For every instance that finished, also read back a lean capture (same shape as a plain capture --json) instead of just its status
  --json                    Return JSON output
  -h, --help                Show this help

Output:
  Text mode prints instance name, status, elapsed time, and a timed_out marker; with --collect, the content of each finished instance follows on its own block.
  JSON mode returns timed_out, saw_busy, elapsed_ms, stability, and, for TUI harnesses, screen fields.

Notes:
  wait means "wait until the agent seems done", not "wait until the terminal is visually static".
  Reaching --timeout is not a failure: it returns ok true with status busy and data.timed_out true, so a long task is simply waited on again.
  Only a broken, lost, or exited instance makes wait fail; a --collect read that fails on an otherwise-finished instance is reported the same way, against that instance alone.
  A prompt confirms that a title-signaling harness started working; until that is observed, an idle title inside the --stable window is treated as stale and ignored.
  data.saw_busy reports whether this wait actually observed the harness working.
  With several instances, data.instances reports each one, plus data.done, data.pending, and data.failed. mode=any is what makes a fan-out usable: handle whichever shard lands first instead of blocking on the slowest.
  With several instances a failure on one is reported against that instance instead of discarding the other results.
  --collect only reads instances that are done; a still-pending instance in the same call gets no content, so a fan-out is summon --detach x N, then wait --mode any --collect to pick up whichever shard lands first without a separate capture call per instance.
  Use inspect or list when you want to query status without blocking.
  For title-signaling harnesses such as claude-code, codex-cli, and gemini-cli, completion is inferred from pane_title idle markers.
  For claude-code-ndjson, completion is inferred from protocol events.
  For codex-cli-execjson, completion means the turn process exited; a failed turn still satisfies wait, and the reason is reported by capture --json as last_error.
  For pi-rpc, completion is inferred from the agent_settled protocol event, which fires only after retries, compaction, and any queued follow-up prompts have drained.
  For generic harnesses, completion falls back to screen stability heuristics.
  Without --collect, the title-signaling path polls pane metadata only and does not capture screen content.

Examples:
  agentmux wait 编码助手-A --stable 1500 --timeout 30s --json
  agentmux wait 编码助手-A --timeout 3m --json
  agentmux wait 分片-A 分片-B 分片-C --mode any --timeout 5m --json
  agentmux wait 分片-A 分片-B --mode any --collect --json
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
  JSON mode also returns data.build_time, data.binary_path, data.commands, data.harness_types, and data.features, so a caller can check for a capability instead of probing commands and interpreting failures.

Notes:
  A version of "dev" means this build was not stamped by scripts/install.sh or scripts/release.sh; run agentmux doctor to check whether PATH actually resolves to this binary.

Examples:
  agentmux version
  agentmux version --json
`)
}

func doctorHelp() string {
	return strings.TrimSpace(`
doctor checks whether the environment agentmux depends on is actually usable, in one pass.

Usage:
  agentmux doctor [--json]

Checks:
  binary       This build's version, build time, and resolved path.
  path         Whether a plain "agentmux" on PATH resolves to the binary that is actually running; flags stale copies that shadow a fresh install.
  paths        Resolved config file and state directory locations.
  state_dir    Whether the state directory is writable.
  registry     Whether the instance registry can be locked.
  config       Whether the config file parses and validates.
  template:*   For each configured template, whether its resolved command's binary is on PATH.
  tmux         Whether tmux is on PATH, only checked when a configured template needs it.

Output:
  Each check reports status ok, warn, fail, or skip. Only fail affects the overall result and the exit code.
  Text mode prints a table. JSON mode returns {"ok", "command": "doctor", "data.checks"}.

Notes:
  Run this first whenever agentmux misbehaves, before assuming a bug: most "it does not do what the docs say" reports trace back to a stale binary on PATH or a missing external CLI, both of which doctor catches directly.

Examples:
  agentmux doctor
  agentmux doctor --json
`)
}
