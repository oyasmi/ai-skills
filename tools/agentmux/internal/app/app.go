package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/config"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/output"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/service"
)

var Version = "dev"
var BuildTime = "unknown"
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
	if rest[0] == "doctor" {
		return runDoctor(ctx, jsonMode, rest[1:], stdout, stderr)
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
		fmt.Fprintln(w, "NAME\tMODEL\tEFFORT\tHARNESS\tCWD\tDESCRIPTION")
		for _, item := range items {
			// A role's description explains when to use it and how, so it is
			// routinely several lines. The table shows a one-line summary to stay a
			// table; `template list --json` carries the whole thing.
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", item["name"], item["model"], item["effort"], item["harness_type"], item["cwd"], summarizeDescription(item["description"]))
		}
		_ = w.Flush()
		return 0
	case "list":
		includeEnded, err := parseListArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "list", "", err)
		}
		items, err := svc.List(ctx, includeEnded)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "list", "", err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "list", Data: map[string]any{"instances": items}})
			return 0
		}
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tTEMPLATE\tSTATUS\tMODEL\tCWD\tUPDATED\tENDED")
		for _, item := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.Name, item.Template, item.Status, item.Model, item.CWD, item.UpdatedAt.Local().Format(time.RFC3339), item.EndReason)
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
			data := map[string]any{
				"template":     res.Instance.Template,
				"model":        res.Instance.Model,
				"effort":       res.Instance.Effort,
				"cwd":          res.Instance.CWD,
				"harness_type": res.Instance.HarnessType,
			}
			if len(res.Warnings) > 0 {
				data["warnings"] = res.Warnings
			}
			_ = output.WriteJSON(stdout, output.Success{
				OK:       true,
				Command:  "summon",
				Instance: res.Instance.Name,
				Reused:   boolPtr(res.Reused),
				Status:   string(res.Instance.Status),
				Data:     data,
			})
			return 0
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", res.Instance.Name, res.Instance.Template, res.Instance.Status)
		for _, w := range res.Warnings {
			fmt.Fprintf(stderr, "warning: %s\n", w)
		}
		return 0
	case "run":
		input, useStdin, err := parseRunArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "run", "", err)
		}
		if useStdin {
			input.Prompt, err = readPromptText(os.Stdin)
			if err != nil {
				return writeErr(stdout, stderr, jsonMode, "run", input.Summon.Name, err)
			}
		}
		res, err := svc.Run(ctx, input)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "run", input.Summon.Name, err)
		}
		if jsonMode {
			data := map[string]any{
				"template":     res.Instance.Template,
				"model":        res.Instance.Model,
				"effort":       res.Instance.Effort,
				"harness_type": res.Instance.HarnessType,
				"cwd":          res.Instance.CWD,
				"elapsed_ms":   res.ElapsedMS,
				"queued_ms":    res.QueuedMS,
			}
			if res.Detached {
				data["detached"] = true
			} else {
				data["content"] = res.Snapshot.Content
				data["timed_out"] = res.TimedOut
				for k, v := range res.Snapshot.Extra {
					data[k] = v
				}
			}
			if len(res.Warnings) > 0 {
				data["warnings"] = res.Warnings
			}
			_ = output.WriteJSON(stdout, output.Success{
				OK:       true,
				Command:  "run",
				Instance: res.Instance.Name,
				Reused:   boolPtr(res.Reused),
				Status:   string(res.Instance.Status),
				Data:     data,
			})
			return 0
		}
		if res.Detached {
			fmt.Fprintf(stdout, "%s\t%s\n", res.Instance.Name, res.Instance.Status)
			for _, w := range res.Warnings {
				fmt.Fprintf(stderr, "warning: %s\n", w)
			}
			return 0
		}
		fmt.Fprint(stdout, res.Snapshot.Content)
		if !strings.HasSuffix(res.Snapshot.Content, "\n") {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stderr, "instance: %s\n", res.Instance.Name)
		for _, w := range res.Warnings {
			fmt.Fprintf(stderr, "warning: %s\n", w)
		}
		if res.TimedOut {
			fmt.Fprintf(stderr, "%s is still working after %dms; wait on it again\n", res.Instance.Name, res.ElapsedMS)
		}
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
		fmt.Fprintf(stdout, "effort: %s\n", inst.Effort)
		fmt.Fprintf(stdout, "cwd: %s\n", inst.CWD)
		fmt.Fprintf(stdout, "command: %s\n", inst.Command)
		fmt.Fprintf(stdout, "session_id: %s\n", inst.SessionID)
		fmt.Fprintf(stdout, "first_prompt_sent: %t\n", inst.FirstPromptSent)
		fmt.Fprintf(stdout, "created_at: %s\n", inst.CreatedAt.Local().Format(time.RFC3339))
		fmt.Fprintf(stdout, "updated_at: %s\n", inst.UpdatedAt.Local().Format(time.RFC3339))
		fmt.Fprintf(stdout, "last_activity_at: %s\n", inst.LastActivityAt.Local().Format(time.RFC3339))
		return 0
	case "prompt":
		name, text, key, useStdin, waitIfBusyMS, err := parsePromptArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "prompt", "", err)
		}
		if useStdin {
			text, err = readPromptText(os.Stdin)
			if err != nil {
				return writeErr(stdout, stderr, jsonMode, "prompt", name, err)
			}
		}
		queuedMS, err := svc.WaitIfBusy(ctx, name, waitIfBusyMS)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "prompt", name, err)
		}
		inst, err := svc.Prompt(ctx, name, text, key)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "prompt", name, err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "prompt", Instance: inst.Name, Status: string(inst.Status), Data: map[string]any{
				"sent_text": text != "",
				"sent_key":  key,
				"queued_ms": queuedMS,
			}})
			return 0
		}
		fmt.Fprintf(stdout, "%s\t%s\n", inst.Name, inst.Status)
		return 0
	case "capture":
		name, opts, err := parseCaptureArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "capture", "", err)
		}
		inst, snap, err := svc.Capture(ctx, name, opts)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "capture", name, err)
		}
		if jsonMode {
			data := map[string]any{
				"harness_type": inst.HarnessType,
				"scope":        string(opts.Scope),
				"content":      snap.Content,
			}
			if opts.Since != "" {
				data["since"] = opts.Since
			}
			if opts.New {
				data["new"] = true
			}
			// Screen geometry only means something for a terminal harness;
			// emitting zeroed fields for structured ones is pure noise.
			if !service.IsStructuredHarness(inst.HarnessType) {
				data["cursor_x"] = snap.CursorX
				data["cursor_y"] = snap.CursorY
				data["width"] = snap.Width
				data["height"] = snap.Height
				data["history_lines"] = snap.History
				data["pane_title"] = snap.PaneTitle
			} else if _, hasTrace := snap.Extra["messages"]; hasTrace {
				data["messages_limit"] = snap.History
			}
			for k, v := range snap.Extra {
				data[k] = v
			}
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "capture", Instance: inst.Name, Status: string(inst.Status), Data: data})
			return 0
		}
		fmt.Fprint(stdout, snap.Content)
		return 0
	case "wait":
		names, stableMS, timeoutMS, mode, collect, err := parseWaitArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "wait", "", err)
		}
		if len(names) > 1 {
			return waitMany(ctx, svc, names, stableMS, timeoutMS, mode, collect, jsonMode, stdout, stderr)
		}
		name := names[0]
		inst, snap, err := svc.Wait(ctx, name, stableMS, timeoutMS)
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "wait", name, err)
		}
		var collectSnap capture.Snapshot
		if collect && !snap.TimedOut {
			inst, collectSnap, err = svc.Capture(ctx, name, capture.Options{History: -1, Scope: capture.ScopeCurrent})
			if err != nil {
				return writeErr(stdout, stderr, jsonMode, "wait", name, err)
			}
		}
		if jsonMode {
			data := map[string]any{
				"timed_out":  snap.TimedOut,
				"elapsed_ms": snap.ElapsedMS,
			}
			// saw_busy and screen stability describe terminal observation; a
			// structured harness reports completion through its protocol.
			if !service.IsStructuredHarness(inst.HarnessType) {
				data["saw_busy"] = snap.SawBusy
				data["stable_for_ms"] = snap.StableForMS
				data["cursor_x"] = snap.CursorX
				data["cursor_y"] = snap.CursorY
				data["width"] = snap.Width
				data["height"] = snap.Height
				data["history_lines"] = snap.History
				data["pane_title"] = snap.PaneTitle
			}
			if collect {
				data["content"] = collectSnap.Content
				for k, v := range collectSnap.Extra {
					data[k] = v
				}
			}
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "wait", Instance: inst.Name, Status: string(inst.Status), Data: data})
			return 0
		}
		timedOut := ""
		if snap.TimedOut {
			timedOut = "\ttimed_out"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%dms%s\n", inst.Name, inst.Status, snap.ElapsedMS, timedOut)
		if collect {
			fmt.Fprint(stdout, collectSnap.Content)
		}
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
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "version", Data: map[string]any{
				"version":       Version,
				"build_time":    BuildTime,
				"binary_path":   resolvedExecutablePath(),
				"commands":      commandNames(),
				"harness_types": service.HarnessTypes(),
				"features":      featureNames(),
			}})
			return 0
		}
		fmt.Fprintln(stdout, Version)
		return 0
	default:
		return writeErr(stdout, stderr, jsonMode, "", "", apperr.New("invalid_arguments", "unknown command "+args[0]+"\n\n"+rootHelp()))
	}
}

func boolPtr(v bool) *bool { return &v }

// summarizeDescription collapses a multi-line description into a single
// printable line for a tab-separated table. Descriptions are authored as
// YAML block scalars that soft-wrap a sentence across several lines, so a
// naive first-line split cuts off mid-sentence; this reflows the first
// paragraph and takes as many whole sentences as fit within a length
// budget instead.
func summarizeDescription(s string) string {
	if i := strings.Index(s, "\n\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	const maxRunes = 60
	enders := "。！？.!?"
	runes := []rune(s)

	var sentenceEnds []int
	start := 0
	for i, r := range runes {
		if strings.ContainsRune(enders, r) {
			sentenceEnds = append(sentenceEnds, i+1)
			start = i + 1
		}
	}
	if start < len(runes) {
		sentenceEnds = append(sentenceEnds, len(runes))
	}

	cut := 0
	for _, end := range sentenceEnds {
		if end > maxRunes && cut > 0 {
			break
		}
		cut = end
		if cut >= maxRunes {
			break
		}
	}
	if cut == 0 {
		cut = len(runes)
	}
	if cut > maxRunes {
		cut = maxRunes
	}
	if cut < len(runes) {
		return strings.TrimSpace(string(runes[:cut])) + "..."
	}
	return string(runes[:cut])
}
