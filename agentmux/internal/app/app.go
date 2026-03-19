package app

import (
	"context"
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

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	jsonMode, rest := extractJSON(args)
	paths, err := config.DiscoverPaths()
	if err != nil {
		return writeErr(stdout, stderr, jsonMode, "", "", err)
	}
	if err := config.EnsureStateDir(paths); err != nil {
		return writeErr(stdout, stderr, jsonMode, "", "", err)
	}
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		return writeErr(stdout, stderr, jsonMode, "", "", err)
	}
	svc := service.New(paths, cfg)
	return dispatch(ctx, svc, jsonMode, rest, stdout, stderr)
}

func dispatch(ctx context.Context, svc service.Service, jsonMode bool, args []string, stdout, stderr io.Writer) int {
	switch args[0] {
	case "template":
		if len(args) < 2 || args[1] != "list" {
			return writeErr(stdout, stderr, jsonMode, "template", "", apperr.New("invalid_arguments", "usage: agentmux template list [--json]"))
		}
		items := svc.TemplateList()
		sort.Slice(items, func(i, j int) bool { return items[i]["name"] < items[j]["name"] })
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "template list", Data: map[string]any{"templates": items}})
			return 0
		}
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tMODEL\tCWD\tDESCRIPTION")
		for _, item := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item["name"], item["model"], item["cwd"], item["description"])
		}
		_ = w.Flush()
		return 0
	case "list":
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
					"template": res.Instance.Template,
					"model":    res.Instance.Model,
					"cwd":      res.Instance.CWD,
				},
			})
			return 0
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", res.Instance.Name, res.Instance.Template, res.Instance.Status)
		return 0
	case "inspect":
		if len(args) < 2 {
			return writeErr(stdout, stderr, jsonMode, "inspect", "", apperr.New("invalid_arguments", "usage: agentmux inspect <instance-name> [--json]"))
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
		name, text, key, enter, err := parsePromptArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "prompt", "", err)
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
				"content":       snap.Content,
			}})
			return 0
		}
		fmt.Fprint(stdout, snap.Content)
		return 0
	case "attach":
		if len(args) >= 2 {
			return attach(ctx, svc, args[1], stderr)
		}
		return attachSelect(ctx, svc, stderr)
	case "halt":
		if len(args) < 2 {
			return writeErr(stdout, stderr, jsonMode, "halt", "", apperr.New("invalid_arguments", "usage: agentmux halt <instance-name> [--json]"))
		}
		inst, err := svc.Halt(ctx, args[1])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "halt", args[1], err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "halt", Instance: inst.Name, Status: string(inst.Status), Data: map[string]any{}})
			return 0
		}
		fmt.Fprintf(stdout, "%s\t%s\n", inst.Name, inst.Status)
		return 0
	default:
		return writeErr(stdout, stderr, jsonMode, "", "", apperr.New("invalid_arguments", usage()))
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
		return in, apperr.New("invalid_arguments", "summon requires --template")
	}
	return in, nil
}

func parsePromptArgs(args []string) (name, text, key string, enter bool, err error) {
	if len(args) == 0 {
		return "", "", "", false, apperr.New("invalid_arguments", "usage: agentmux prompt <instance-name> [--text TEXT] [--key KEY] [--enter]")
	}
	name = args[0]
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--text":
			i++
			if i >= len(args) {
				return "", "", "", false, apperr.New("invalid_arguments", "missing value for --text")
			}
			text = args[i]
		case "--key":
			i++
			if i >= len(args) {
				return "", "", "", false, apperr.New("invalid_arguments", "missing value for --key")
			}
			key = args[i]
		case "--enter":
			enter = true
		case "--json":
		default:
			return "", "", "", false, apperr.New("invalid_arguments", "unknown flag "+args[i])
		}
	}
	return name, text, key, enter, nil
}

func parseCaptureArgs(args []string) (name string, history, stableMS, timeoutMS int, err error) {
	if len(args) == 0 {
		return "", 0, 0, 0, apperr.New("invalid_arguments", "usage: agentmux capture <instance-name> [--history N] [--stable MS] [--timeout DURATION]")
	}
	name = args[0]
	history = -1
	timeoutMS = 30000
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--history":
			i++
			if i >= len(args) {
				return "", 0, 0, 0, apperr.New("invalid_arguments", "missing value for --history")
			}
			history, err = strconv.Atoi(args[i])
			if err != nil {
				return "", 0, 0, 0, apperr.New("invalid_arguments", "invalid value for --history")
			}
		case "--stable":
			i++
			if i >= len(args) {
				return "", 0, 0, 0, apperr.New("invalid_arguments", "missing value for --stable")
			}
			stableMS, err = strconv.Atoi(args[i])
			if err != nil {
				return "", 0, 0, 0, apperr.New("invalid_arguments", "invalid value for --stable")
			}
		case "--timeout":
			i++
			if i >= len(args) {
				return "", 0, 0, 0, apperr.New("invalid_arguments", "missing value for --timeout")
			}
			d, derr := time.ParseDuration(args[i])
			if derr != nil {
				return "", 0, 0, 0, apperr.New("invalid_arguments", "invalid value for --timeout")
			}
			timeoutMS = int(d.Milliseconds())
		case "--json":
		default:
			return "", 0, 0, 0, apperr.New("invalid_arguments", "unknown flag "+args[i])
		}
	}
	return name, history, stableMS, timeoutMS, nil
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
  agentmux summon --template <template-name> [--name <instance-name>] [--cwd <path>] [--model <provider/model>] [--command <shell-command>] [--system-prompt <text>] [--prompt <text>] [--json]
  agentmux inspect <instance-name> [--json]
  agentmux prompt <instance-name> [--text <text>] [--key <key>] [--enter] [--json]
  agentmux capture <instance-name> [--history <lines>] [--stable <ms>] [--timeout <duration>] [--json]
  agentmux attach [<instance-name>]
  agentmux halt <instance-name> [--json]
`)
}

func boolPtr(v bool) *bool { return &v }
