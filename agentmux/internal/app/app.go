package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
	"github.com/oyasmi/agentmux/internal/config"
	"github.com/oyasmi/agentmux/internal/output"
	"github.com/oyasmi/agentmux/internal/service"
)

var Version = "dev"
var newService = service.New

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, rootHelp())
		return 0
	}
	if helpText, ok := helpForArgs(args); ok {
		fmt.Fprintln(stdout, helpText)
		return 0
	}
	jsonMode, rest := extractJSON(args)
	if len(rest) == 0 {
		fmt.Fprintln(stdout, rootHelp())
		return 0
	}
	if rest[0] == "version" {
		return dispatch(ctx, service.Service{}, jsonMode, rest, stdout, stderr)
	}
	paths, err := config.DiscoverPaths()
	if err != nil {
		return writeErr(stdout, stderr, jsonMode, "", "", err)
	}
	if err := config.EnsureStateDir(paths); err != nil {
		return writeErr(stdout, stderr, jsonMode, "", "", err)
	}
	if err := config.EnsureDefaultConfig(paths.ConfigFile); err != nil {
		return writeErr(stdout, stderr, jsonMode, "", "", err)
	}
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		return writeErr(stdout, stderr, jsonMode, "", "", err)
	}
	svc := newService(paths, cfg)
	return dispatch(ctx, svc, jsonMode, rest, stdout, stderr)
}

func dispatch(ctx context.Context, svc service.Service, jsonMode bool, args []string, stdout, stderr io.Writer) int {
	switch args[0] {
	case "template":
		if len(args) == 1 {
			return writeErr(stdout, stderr, jsonMode, "template", "", apperr.New("invalid_arguments", "missing template subcommand\n\n"+templateHelp()))
		}
		if len(args) < 2 || args[1] != "list" {
			return writeErr(stdout, stderr, jsonMode, "template", "", apperr.New("invalid_arguments", "unknown template subcommand\n\n"+templateHelp()))
		}
		if len(args) > 2 {
			return writeErr(stdout, stderr, jsonMode, "template list", "", apperr.New("invalid_arguments", "template list does not accept positional arguments\n\n"+templateListHelp()))
		}
		items := svc.TemplateList()
		sort.Slice(items, func(i, j int) bool { return items[i]["name"] < items[j]["name"] })
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "template list", Data: map[string]any{"templates": items}})
			return 0
		}
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tMODEL\tHARNESS\tCWD\tDESCRIPTION")
		for _, item := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", item["name"], item["model"], item["harness_type"], item["cwd"], item["description"])
		}
		_ = w.Flush()
		return 0
	case "list":
		if len(args) > 1 {
			return writeErr(stdout, stderr, jsonMode, "list", "", apperr.New("invalid_arguments", "list does not accept positional arguments\n\n"+listHelp()))
		}
		items, err := svc.List(ctx)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "list", "", err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "list", Data: map[string]any{"instances": items}})
			return 0
		}
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tTEMPLATE\tSTATUS\tMODEL\tCWD\tUPDATED")
		for _, item := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", item.Name, item.Template, item.Status, item.Model, item.CWD, item.UpdatedAt.Format(time.RFC3339))
		}
		_ = w.Flush()
		return 0
	case "summon":
		input, err := parseSummonArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "summon", "", err)
		}
		res, err := svc.Summon(ctx, input)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "summon", input.Name, err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{
				OK:       true,
				Command:  "summon",
				Instance: res.Instance.Name,
				Reused:   boolPtr(res.Reused),
				Status:   string(res.Instance.Status),
				Data: map[string]any{
					"template":     res.Instance.Template,
					"model":        res.Instance.Model,
					"cwd":          res.Instance.CWD,
					"harness_type": res.Instance.HarnessType,
				},
			})
			return 0
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", res.Instance.Name, res.Instance.Template, res.Instance.Status)
		return 0
	case "inspect":
		if len(args) < 2 {
			return writeErr(stdout, stderr, jsonMode, "inspect", "", apperr.New("invalid_arguments", "missing instance name\n\n"+inspectHelp()))
		}
		inst, err := svc.Inspect(ctx, args[1])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "inspect", args[1], err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "inspect", Instance: inst.Name, Status: string(inst.Status), Data: inst})
			return 0
		}
		fmt.Fprintf(stdout, "name: %s\n", inst.Name)
		fmt.Fprintf(stdout, "template: %s\n", inst.Template)
		fmt.Fprintf(stdout, "status: %s\n", inst.Status)
		fmt.Fprintf(stdout, "model: %s\n", inst.Model)
		fmt.Fprintf(stdout, "cwd: %s\n", inst.CWD)
		fmt.Fprintf(stdout, "command: %s\n", inst.Command)
		fmt.Fprintf(stdout, "session_id: %s\n", inst.SessionID)
		fmt.Fprintf(stdout, "first_prompt_sent: %t\n", inst.FirstPromptSent)
		fmt.Fprintf(stdout, "created_at: %s\n", inst.CreatedAt.Format(time.RFC3339))
		fmt.Fprintf(stdout, "updated_at: %s\n", inst.UpdatedAt.Format(time.RFC3339))
		fmt.Fprintf(stdout, "last_activity_at: %s\n", inst.LastActivityAt.Format(time.RFC3339))
		return 0
	case "prompt":
		name, text, key, enter, useStdin, err := parsePromptArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "prompt", "", err)
		}
		if useStdin {
			text, err = readPromptText(os.Stdin)
			if err != nil {
				return writeErr(stdout, stderr, jsonMode, "prompt", name, err)
			}
		}
		inst, err := svc.Prompt(ctx, name, text, key, enter)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "prompt", name, err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "prompt", Instance: inst.Name, Status: string(inst.Status), Data: map[string]any{
				"sent_text": text != "",
				"sent_key":  key,
				"enter":     enter,
			}})
			return 0
		}
		fmt.Fprintf(stdout, "%s\t%s\n", inst.Name, inst.Status)
		return 0
	case "capture":
		name, history, stableMS, timeoutMS, err := parseCaptureArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "capture", "", err)
		}
		inst, snap, err := svc.Capture(ctx, name, history, stableMS, timeoutMS)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "capture", name, err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "capture", Instance: inst.Name, Status: string(inst.Status), Data: map[string]any{
				"cursor_x":      snap.CursorX,
				"cursor_y":      snap.CursorY,
				"width":         snap.Width,
				"height":        snap.Height,
				"history_lines": snap.History,
				"stable_for_ms": snap.StableForMS,
				"pane_title":    snap.PaneTitle,
				"content":       snap.Content,
			}})
			return 0
		}
		fmt.Fprint(stdout, snap.Content)
		return 0
	case "wait":
		name, stableMS, timeoutMS, err := parseWaitArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "wait", "", err)
		}
		inst, snap, err := svc.Wait(ctx, name, stableMS, timeoutMS)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "wait", name, err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "wait", Instance: inst.Name, Status: string(inst.Status), Data: map[string]any{
				"cursor_x":      snap.CursorX,
				"cursor_y":      snap.CursorY,
				"width":         snap.Width,
				"height":        snap.Height,
				"history_lines": snap.History,
				"stable_for_ms": snap.StableForMS,
				"pane_title":    snap.PaneTitle,
			}})
			return 0
		}
		fmt.Fprintf(stdout, "%s\t%s\t%dms\n", inst.Name, inst.Status, snap.StableForMS)
		return 0
	case "attach":
		if len(args) >= 2 {
			return attach(ctx, svc, args[1], stderr)
		}
		return attachSelect(ctx, svc, stderr)
	case "halt":
		name, immediately, timeoutMS, err := parseHaltArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "halt", "", err)
		}
		inst, err := svc.HaltWithOptions(ctx, name, immediately, time.Duration(timeoutMS)*time.Millisecond)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "halt", name, err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "halt", Instance: inst.Name, Status: string(inst.Status), Data: map[string]any{}})
			return 0
		}
		fmt.Fprintf(stdout, "%s\t%s\n", inst.Name, inst.Status)
		return 0
	case "version":
		if len(args) > 1 {
			return writeErr(stdout, stderr, jsonMode, "version", "", apperr.New("invalid_arguments", "version does not accept positional arguments\n\n"+versionHelp()))
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "version", Data: map[string]any{"version": Version}})
			return 0
		}
		fmt.Fprintln(stdout, Version)
		return 0
	default:
		return writeErr(stdout, stderr, jsonMode, "", "", apperr.New("invalid_arguments", "unknown command "+args[0]+"\n\n"+rootHelp()))
	}
}

func parseSummonArgs(args []string) (service.SummonInput, error) {
	in := service.SummonInput{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--template":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --template")
			}
			in.TemplateName = args[i]
		case "--name":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --name")
			}
			in.Name = args[i]
		case "--cwd":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --cwd")
			}
			in.CWD = &args[i]
		case "--model":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --model")
			}
			in.Model = &args[i]
		case "--command":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --command")
			}
			in.Command = &args[i]
		case "--system-prompt":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --system-prompt")
			}
			in.SystemPrompt = &args[i]
		case "--prompt":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --prompt")
			}
			in.Prompt = &args[i]
		default:
			if args[i] == "--json" {
				continue
			}
			return in, apperr.New("invalid_arguments", "unknown flag "+args[i])
		}
	}
	if strings.TrimSpace(in.TemplateName) == "" {
		return in, apperr.New("invalid_arguments", "summon requires --template\n\n"+summonHelp())
	}
	return in, nil
}

func parsePromptArgs(args []string) (name, text, key string, enter, useStdin bool, err error) {
	if len(args) == 0 {
		return "", "", "", false, false, apperr.New("invalid_arguments", "missing instance name\n\n"+promptHelp())
	}
	name = args[0]
	fs := newFlagSet("prompt")
	fs.StringVar(&text, "text", "", "")
	fs.StringVar(&key, "key", "", "")
	fs.BoolVar(&enter, "enter", false, "")
	fs.BoolVar(&useStdin, "stdin", false, "")
	if err := fs.Parse(args[1:]); err != nil {
		return "", "", "", false, false, err
	}
	if fs.NArg() > 0 {
		return "", "", "", false, false, apperr.New("invalid_arguments", "prompt does not accept positional arguments after instance name")
	}
	if useStdin && text != "" {
		return "", "", "", false, false, apperr.New("invalid_arguments", "--stdin cannot be used with --text")
	}
	return name, text, key, enter, useStdin, nil
}

func parseCaptureArgs(args []string) (name string, history, stableMS, timeoutMS int, err error) {
	if len(args) == 0 {
		return "", 0, 0, 0, apperr.New("invalid_arguments", "missing instance name\n\n"+captureHelp())
	}
	name = args[0]
	history = -1
	fs := newFlagSet("capture")
	var stableRaw string
	var timeoutRaw string
	fs.IntVar(&history, "history", -1, "")
	fs.StringVar(&stableRaw, "stable", "", "")
	fs.StringVar(&timeoutRaw, "timeout", "30s", "")
	if err := fs.Parse(args[1:]); err != nil {
		return "", 0, 0, 0, err
	}
	if fs.NArg() > 0 {
		return "", 0, 0, 0, apperr.New("invalid_arguments", "capture does not accept positional arguments after instance name")
	}
	if history < -1 {
		return "", 0, 0, 0, apperr.New("invalid_arguments", "invalid value for --history: must be -1 or a non-negative integer")
	}
	if stableRaw != "" {
		stableMS, err = parseMillisOrDuration(stableRaw, "--stable")
		if err != nil {
			return "", 0, 0, 0, err
		}
	}
	timeoutMS, err = parseMillisOrDuration(timeoutRaw, "--timeout")
	if err != nil {
		return "", 0, 0, 0, err
	}
	return name, history, stableMS, timeoutMS, nil
}

func parseWaitArgs(args []string) (name string, stableMS, timeoutMS int, err error) {
	if len(args) == 0 {
		return "", 0, 0, apperr.New("invalid_arguments", "missing instance name\n\n"+waitHelp())
	}
	name = args[0]
	fs := newFlagSet("wait")
	var stableRaw string
	var timeoutRaw string
	fs.StringVar(&stableRaw, "stable", "1500", "")
	fs.StringVar(&timeoutRaw, "timeout", "30s", "")
	if err := fs.Parse(args[1:]); err != nil {
		return "", 0, 0, err
	}
	if fs.NArg() > 0 {
		return "", 0, 0, apperr.New("invalid_arguments", "wait does not accept positional arguments after instance name")
	}
	stableMS, err = parseMillisOrDuration(stableRaw, "--stable")
	if err != nil {
		return "", 0, 0, err
	}
	timeoutMS, err = parseMillisOrDuration(timeoutRaw, "--timeout")
	if err != nil {
		return "", 0, 0, err
	}
	return name, stableMS, timeoutMS, nil
}

func parseHaltArgs(args []string) (name string, immediately bool, timeoutMS int, err error) {
	if len(args) == 0 {
		return "", false, 0, apperr.New("invalid_arguments", "missing instance name\n\n"+haltHelp())
	}
	name = args[0]
	fs := newFlagSet("halt")
	var timeoutRaw string
	fs.BoolVar(&immediately, "immediately", false, "")
	fs.StringVar(&timeoutRaw, "timeout", "5s", "")
	if err := fs.Parse(args[1:]); err != nil {
		return "", false, 0, err
	}
	if fs.NArg() > 0 {
		return "", false, 0, apperr.New("invalid_arguments", "halt does not accept positional arguments after instance name")
	}
	if immediately && timeoutRaw != "5s" {
		return "", false, 0, apperr.New("invalid_arguments", "--timeout cannot be used with --immediately")
	}
	timeoutMS, err = parseMillisOrDuration(timeoutRaw, "--timeout")
	if err != nil {
		return "", false, 0, err
	}
	return name, immediately, timeoutMS, nil
}

func attach(ctx context.Context, svc service.Service, name string, stderr io.Writer) int {
	inst, err := svc.Inspect(ctx, name)
	if err != nil {
		return writeErr(io.Discard, stderr, false, "attach", name, err)
	}
	cmd := svc.Tmux.Attach(inst.SessionID)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func attachSelect(ctx context.Context, svc service.Service, stderr io.Writer) int {
	if fi, _ := os.Stdin.Stat(); (fi.Mode() & os.ModeCharDevice) == 0 {
		return writeErr(io.Discard, stderr, false, "attach", "", apperr.New("invalid_arguments", "attach without instance requires a tty"))
	}
	items, err := svc.List(ctx)
	if err != nil {
		return writeErr(io.Discard, stderr, false, "attach", "", err)
	}
	if len(items) == 0 {
		fmt.Fprintln(stderr, "no instances")
		return 1
	}
	for i, item := range items {
		fmt.Fprintf(stderr, "%d. %s (%s)\n", i+1, item.Name, item.Status)
	}
	fmt.Fprint(stderr, "select instance: ")
	var choice int
	if _, err := fmt.Fscan(os.Stdin, &choice); err != nil || choice < 1 || choice > len(items) {
		fmt.Fprintln(stderr, "invalid selection")
		return 1
	}
	return attach(ctx, svc, items[choice-1].Name, stderr)
}

func extractJSON(args []string) (bool, []string) {
	out := make([]string, 0, len(args))
	jsonMode := false
	for _, arg := range args {
		if arg == "--json" {
			jsonMode = true
			continue
		}
		out = append(out, arg)
	}
	return jsonMode, out
}

func writeErr(stdout, stderr io.Writer, jsonMode bool, command, instance string, err error) int {
	if jsonMode {
		_ = output.WriteJSON(stdout, output.Failure{
			OK:        false,
			Command:   command,
			Instance:  instance,
			ErrorCode: apperr.Code(err),
			Error:     err.Error(),
		})
		return 1
	}
	fmt.Fprintln(stderr, err.Error())
	return 1
}

func usage() string {
	return strings.TrimSpace(`
usage:
  agentmux template list [--json]
  agentmux list [--json]
  agentmux summon --template <template-name> [--name <instance-name>] [--cwd <path>] [--model <model>] [--command <shell-command>] [--system-prompt <text>] [--prompt <text>] [--json]
  agentmux inspect <instance-name> [--json]
  agentmux prompt <instance-name> [--text <text> | --stdin] [--key <key>] [--enter] [--json]
  agentmux capture <instance-name> [--history <lines>] [--stable <duration-or-ms>] [--timeout <duration-or-ms>] [--json]
  agentmux wait <instance-name> [--stable <duration-or-ms>] [--timeout <duration-or-ms>] [--json]
  agentmux attach [<instance-name>]
  agentmux halt <instance-name> [--json]
  agentmux version [--json]
`)
}

func boolPtr(v bool) *bool { return &v }

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
  list            List known instances
  summon          Create or reuse an instance
  inspect         Show detailed instance metadata
  prompt          Send text or a special key to an instance
  capture         Capture current screen text from an instance
  wait            Wait until the screen is stable without returning content
  attach          Attach a human terminal to an instance
  halt            Stop an instance
  version         Print the CLI version

Global flags:
  --json          Return machine-readable JSON for command output
  -h, --help      Show help for the selected command

Examples:
  agentmux template list --json
  agentmux summon --template 深度编码专家 --name 编码助手-A --cwd ~/work/project
  agentmux summon --template 深度编码专家 --name 编码助手-A --prompt "先阅读项目并总结结构" --json
  agentmux capture 编码助手-A --history 120 --stable 1500 --json
  echo "继续修复剩余失败测试" | agentmux prompt 编码助手-A --stdin --enter --json
  agentmux prompt 编码助手-A --text "继续" --enter --json

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

Examples:
  agentmux list
  agentmux list --json
`)
}

func summonHelp() string {
	return strings.TrimSpace(`
summon creates a new instance or reuses an existing one with the same name.

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
  If the named instance exists, summon reuses it.
  If --prompt is provided, summon sends the prompt for both new and reused instances.
  Reusing an instance does not mutate its stored config.

Examples:
  agentmux summon --template 深度编码专家
  agentmux summon --template 深度编码专家 --name 编码助手-A --cwd ~/work/project
  agentmux summon --template 深度编码专家 --name 编码助手-A --prompt "继续修复测试" --json
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
  --enter                   Send Enter after --text
  --json                    Return JSON output
  -h, --help                Show this help

Supported keys:
  Enter, C-c, Escape, Up, Down, Tab

Notes:
  Provide at least one of --text, --stdin, or --key.
  --stdin reads all of stdin as one text payload.
  --stdin cannot be combined with --text.
  --enter affects text input from --text or --stdin.

Examples:
  agentmux prompt 编码助手-A --text "继续" --enter --json
  echo "长文本" | agentmux prompt 编码助手-A --stdin --enter --json
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
  --stable <duration-or-ms> Wait until screen content is stable, for example 1500, 1500ms, or 1.5s
  --timeout <duration-or-ms> Maximum wait time for stability, for example 30s or 500ms
  --json                    Return JSON output
  -h, --help                Show this help

Output:
  Text mode prints captured screen text only.
  JSON mode returns cursor position, screen size, stability, pane title, and content.

Notes:
  For claude-code harnesses, --stable can return early when pane_title indicates idle.

Examples:
  agentmux capture 编码助手-A
  agentmux capture 编码助手-A --history 120 --json
  agentmux capture 编码助手-A --history 120 --stable 1500 --timeout 30s --json
`)
}

func waitHelp() string {
	return strings.TrimSpace(`
wait blocks until the instance screen is stable without returning captured content.

Usage:
  agentmux wait <instance-name> [flags]

Arguments:
  <instance-name>           Target instance name

Flags:
  --stable <duration-or-ms> Required stable interval, default 1500
  --timeout <duration-or-ms> Maximum wait time, default 30s
  --json                    Return JSON output
  -h, --help                Show this help

Output:
  Text mode prints instance name, status, and stable duration only.
  JSON mode returns cursor position, screen size, history lines, stability, and pane title.

Notes:
  For claude-code harnesses, wait can return early when pane_title indicates idle.
  That path polls pane metadata only and does not capture screen content.

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

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseMillisOrDuration(raw, flagName string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(value); err == nil {
		if n < 0 {
			return 0, apperr.New("invalid_arguments", "invalid value for "+flagName+": must be non-negative")
		}
		return n, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, apperr.New("invalid_arguments", "invalid value for "+flagName)
	}
	if d < 0 {
		return 0, apperr.New("invalid_arguments", "invalid value for "+flagName+": must be non-negative")
	}
	return int(d.Milliseconds()), nil
}

func readPromptText(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", apperr.Wrap("internal_error", err, "read prompt text from stdin")
	}
	return string(b), nil
}
