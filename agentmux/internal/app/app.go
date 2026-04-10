package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
	"github.com/oyasmi/agentmux/internal/config"
	"github.com/oyasmi/agentmux/internal/output"
	"github.com/oyasmi/agentmux/internal/service"
)

var Version = "dev"
var newService = service.New

const maxPromptInputBytes = 3 << 20

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
		name, text, key, useStdin, err := parsePromptArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "prompt", "", err)
		}
		if useStdin {
			text, err = readPromptText(os.Stdin)
			if err != nil {
				return writeErr(stdout, stderr, jsonMode, "prompt", name, err)
			}
		}
		inst, err := svc.Prompt(ctx, name, text, key)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "prompt", name, err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "prompt", Instance: inst.Name, Status: string(inst.Status), Data: map[string]any{
				"sent_text": text != "",
				"sent_key":  key,
			}})
			return 0
		}
		fmt.Fprintf(stdout, "%s\t%s\n", inst.Name, inst.Status)
		return 0
	case "capture":
		name, history, err := parseCaptureArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "capture", "", err)
		}
		inst, snap, err := svc.Capture(ctx, name, history)
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

func boolPtr(v bool) *bool { return &v }
