package app

import "strings"

func usage() string {
	return strings.TrimSpace(`
usage:
  agentmux template list [--json]
  agentmux list [--json]
  agentmux summon --template <template-name> [--name <instance-name>] [--cwd <path>] [--model <model>] [--command <shell-command>] [--system-prompt <text>] [--prompt <text>] [--json]
  agentmux inspect <instance-name> [--json]
  agentmux prompt <instance-name> [--text <text> | --stdin] [--key <key>] [--json]
  agentmux capture <instance-name> [--history <lines>] [--json]
  agentmux wait <instance-name> [--stable <duration-or-ms>] [--timeout <duration-or-ms>] [--json]
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
agentmux manages isolated tmux-backed terminal agent instances for AI orchestrators.

Usage:
  agentmux <command> [arguments]
  agentmux help [command]
  agentmux --help

Core commands:
  template list   List configured role templates
  list            List known instances and their current statuses
  summon          Create or reuse an instance
  inspect         Query one instance's current status and metadata
  prompt          Send text or a special key to an instance
  capture         Capture current screen text from an instance
  wait            Wait until an agent appears done with/without a timeout
  attach          Attach a human terminal to an instance
  halt            Stop an instance
  version         Print the CLI version

Global flags:
  --json          Return machine-readable JSON for command output
  -h, --help      Show help for the selected command

Examples:
  agentmux template list --json
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
  agentmux list [--json]

Output:
  Text mode prints a table with name, template, status, model, cwd, and update time.
  JSON mode returns {"ok", "command", "data.instances"}.

Notes:
  Use list for multi-instance status overview.

Examples:
  agentmux list
  agentmux list --json
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
  If text appears in the input box but execution does not start, follow up with --key Enter.
  For some TUI harnesses, especially Claude Code, very long stdin payloads may be less reliable than writing instructions to a file and sending a short follow-up prompt.

Examples:
  agentmux prompt 编码助手-A --text "继续" --json
  echo "补充两行说明" | agentmux prompt 编码助手-A --stdin --json
  agentmux prompt 编码助手-A --key C-c --json
`)
}

func captureHelp() string {
	return strings.TrimSpace(`
capture returns pure text captured from the instance screen through tmux capture-pane.

Usage:
  agentmux capture <instance-name> [flags]

Arguments:
  <instance-name>           Target instance name

Flags:
  --history <lines>         Include N history lines above the visible screen
  --json                    Return JSON output
  -h, --help                Show this help

Output:
  Text mode prints captured screen text only.
  JSON mode returns cursor position, screen size, pane title, and content.

Notes:
  capture always returns the current screen immediately.
  capture is for reading terminal output, not for waiting or querying status by itself.
  Use inspect --json when you only need current status or pane title.
  Use wait if you need to block until the agent appears done.

Examples:
  agentmux capture 编码助手-A
  agentmux capture 编码助手-A --history 120 --json
`)
}

func waitHelp() string {
	return strings.TrimSpace(`
wait blocks until the agent appears to have finished its current work without returning captured content.

Usage:
  agentmux wait <instance-name> [flags]

Arguments:
  <instance-name>           Target instance name

Flags:
  --stable <duration-or-ms> Stability window for generic harness detection, default 1500
  --timeout <duration-or-ms> Maximum wait time, default 30s
  --json                    Return JSON output
  -h, --help                Show this help

Output:
  Text mode prints instance name, status, and stable duration only.
  JSON mode returns cursor position, screen size, history lines, stability, and pane title.

Notes:
  wait means "wait until the agent seems done", not "wait until the terminal is visually static".
  Use inspect or list when you want to query status without blocking.
  For title-signaling harnesses such as claude-code, codex-cli, and gemini-cli, completion is inferred from pane_title idle markers.
  For generic harnesses, completion falls back to screen stability heuristics.
  The title-signaling path polls pane metadata only and does not capture screen content.

Examples:
  agentmux wait 编码助手-A --stable 1500 --timeout 30s --json
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
version prints the current agentmux CLI version.

Usage:
  agentmux version [--json]

Examples:
  agentmux version
  agentmux version --json
`)
}
