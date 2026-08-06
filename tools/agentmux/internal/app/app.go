package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

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
	if helpText, ok, err := helpForArgs(args); ok {
		if err != nil {
			return writeErr(stdout, stderr, false, "help", "", err)
		}
		fmt.Fprintln(stdout, helpText)
		return 0
	}
	jsonMode, rest, err := extractJSON(args)
	if err != nil {
		return writeErr(stdout, stderr, false, "", "", err)
	}
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
	templateListOnly := rest[0] == "template" && len(rest) > 1 && rest[1] == "list"
	if !templateListOnly {
		if err := config.EnsureStateDir(paths); err != nil {
			return writeErr(stdout, stderr, jsonMode, "", "", err)
		}
		if err := config.EnsureDefaultConfig(paths.ConfigFile); err != nil {
			return writeErr(stdout, stderr, jsonMode, "", "", err)
		}
	}
	var cfg config.Config
	if templateListOnly {
		cfg, err = config.LoadOrDefault(paths.ConfigFile)
	} else {
		cfg, err = config.Load(paths.ConfigFile)
	}
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
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "template list", Data: map[string]any{"templates": items}})
			return 0
		}
		rows := make([][]string, 0, len(items))
		for _, item := range items {
			rows = append(rows, []string{item.Name, item.Model, item.Effort, item.HarnessType, shortPath(item.CWD), summarizeDescription(item.Description)})
		}
		_ = output.RenderTable(stdout, []string{"Name", "Model", "Effort", "Harness", "CWD", "Description"}, rows)
		return 0
	case "list":
		includeEnded, err := parseListArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "list", "", err)
		}
		items, err := svc.List(ctx, includeEnded)
		if err != nil && len(items) == 0 {
			return writeErr(stdout, stderr, jsonMode, "list", "", err)
		}
		var warning string
		if err != nil {
			warning = shortError(err)
		}
		if jsonMode {
			summaries := make([]instanceSummary, 0, len(items))
			for _, item := range items {
				summaries = append(summaries, summarizeInstance(item))
			}
			data := map[string]any{"instances": summaries}
			if warning != "" {
				data["warnings"] = []string{warning}
			}
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "list", Data: data})
			if warning != "" {
				fmt.Fprintf(stderr, "warning: %s\n", warning)
			}
			return 0
		}
		headers := []string{"Name", "Template", "Status", "Model", "CWD", "Created", "Last activity"}
		rows := make([][]string, 0, len(items))
		if includeEnded {
			headers = append(headers, "Ended", "Reason")
		}
		for _, item := range items {
			row := []string{item.Name, item.Template, string(item.Status), item.Model, shortPath(item.CWD), textTime(item.CreatedAt), textTime(item.LastActivityAt)}
			if includeEnded {
				reason := item.EndReason
				if strings.TrimSpace(reason) == "" {
					reason = "-"
				}
				row = append(row, textTime(item.EndedAt), reason)
			}
			rows = append(rows, row)
		}
		_ = output.RenderTable(stdout, headers, rows)
		if warning != "" {
			fmt.Fprintf(stderr, "warning: %s\n", warning)
		}
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
		if len(args) > 2 {
			return writeErr(stdout, stderr, jsonMode, "inspect", args[1], apperr.New("invalid_arguments", "inspect accepts exactly one instance name\n\n"+inspectHelp()))
		}
		inst, err := svc.Inspect(ctx, args[1])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "inspect", args[1], err)
		}
		if jsonMode {
			_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "inspect", Instance: inst.Name, Status: string(inst.Status), Data: detailInstance(inst)})
			return 0
		}
		fmt.Fprintf(stdout, "Name: %s\n", inst.Name)
		fmt.Fprintf(stdout, "Template: %s\n", inst.Template)
		fmt.Fprintf(stdout, "Status: %s\n", inst.Status)
		fmt.Fprintf(stdout, "Model: %s\n", inst.Model)
		fmt.Fprintf(stdout, "Effort: %s\n", inst.Effort)
		fmt.Fprintf(stdout, "Harness: %s\n", inst.HarnessType)
		fmt.Fprintf(stdout, "CWD: %s\n", inst.CWD)
		fmt.Fprintf(stdout, "Command: %s\n", inst.Command)
		fmt.Fprintf(stdout, "Shell: %s\n", inst.Shell)
		fmt.Fprintf(stdout, "Session ID: %s\n", inst.SessionID)
		fmt.Fprintf(stdout, "First prompt sent: %t\n", inst.FirstPromptSent)
		fmt.Fprintf(stdout, "Created: %s\n", detailTime(inst.CreatedAt))
		fmt.Fprintf(stdout, "Observed: %s\n", detailTime(inst.UpdatedAt))
		fmt.Fprintf(stdout, "Last activity: %s\n", detailTime(inst.LastActivityAt))
		if !inst.EndedAt.IsZero() {
			fmt.Fprintf(stdout, "Ended: %s\n", detailTime(inst.EndedAt))
		}
		if inst.EndReason != "" {
			fmt.Fprintf(stdout, "Reason: %s\n", inst.EndReason)
		}
		if inst.LastError != "" {
			fmt.Fprintf(stdout, "Last error: %s\n", firstLineForDisplay(inst.LastError))
		}
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
	case "logs":
		name, follow, err := parseLogsArgs(args[1:])
		if err != nil {
			return writeErr(stdout, stderr, jsonMode, "logs", "", err)
		}
		if jsonMode {
			return writeErr(stdout, stderr, true, "logs", name, apperr.New("invalid_arguments", "logs is human-readable; for an active structured instance use capture --scope session --history 0 --raw --json; ended instances are readable with logs"))
		}
		return runLogs(ctx, svc, name, follow, stdout, stderr)
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
		if jsonMode {
			return writeErr(stdout, stderr, true, "attach", "", apperr.New("invalid_arguments", "attach is interactive and does not support --json"))
		}
		if len(args) > 2 {
			return writeErr(stdout, stderr, false, "attach", args[1], apperr.New("invalid_arguments", "attach accepts at most one instance name\n\n"+attachHelp()))
		}
		if len(args) >= 2 {
			if strings.HasPrefix(args[1], "-") {
				return writeErr(stdout, stderr, false, "attach", "", apperr.New("invalid_arguments", "attach expects an instance name, not a flag\n\n"+attachHelp()))
			}
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
				"build_time":    output.LocalizeTimestamp(BuildTime),
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
		return writeErr(stdout, stderr, jsonMode, args[0], "", apperr.New("invalid_arguments", "unknown command "+args[0]+"\n\n"+rootHelp()))
	}
}

func boolPtr(v bool) *bool { return &v }

func textTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("01-02 15:04")
}

func detailTime(value time.Time) string {
	if formatted := output.LocalTime(value); formatted != "" {
		return formatted
	}
	return "-"
}

func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if path == home {
			return "~"
		}
		prefix := home + string(os.PathSeparator)
		if strings.HasPrefix(path, prefix) {
			return "~" + strings.TrimPrefix(path, home)
		}
	}
	return path
}

func firstLineForDisplay(value string) string {
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return strings.TrimSpace(value)
}

// summarizeDescription collapses the first paragraph of a description into a
// single printable line. The table renderer owns the width budget; keeping
// this function uncapped prevents a short first sentence from hiding useful
// context before the renderer has even seen it.
func summarizeDescription(s string) string {
	if i := strings.Index(s, "\n\n"); i >= 0 {
		s = s[:i]
	}
	return collapseWrappedText(s)
}

func collapseWrappedText(s string) string {
	var b strings.Builder
	spacePending := false
	var previous rune
	for _, r := range s {
		if unicode.IsSpace(r) {
			spacePending = b.Len() > 0
			continue
		}
		if spacePending && needsWrappedSpace(previous, r) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		previous = r
		spacePending = false
	}
	return strings.TrimSpace(b.String())
}

func needsWrappedSpace(previous, current rune) bool {
	if previous == 0 {
		return false
	}
	if isCJKRune(previous) && isCJKRune(current) {
		return false
	}
	if unicode.IsPunct(previous) || unicode.IsPunct(current) {
		return false
	}
	return true
}

func isCJKRune(r rune) bool {
	return (r >= 0x2e80 && r <= 0x9fff) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0x20000 && r <= 0x3fffd)
}
