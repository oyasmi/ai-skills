package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/output"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/service"
)

// waitMany renders a wait over several instances. The payload is shaped for a
// caller deciding what to do next: which instances finished, which are still
// working, and which could not be waited on at all.
func waitMany(ctx context.Context, svc service.Service, names []string, stableMS, timeoutMS int, mode service.WaitMode, collect bool, jsonMode bool, stdout, stderr io.Writer) int {
	outcomes, ok, err := svc.WaitMany(ctx, names, stableMS, timeoutMS, mode, collect)
	if err != nil {
		return writeErr(stdout, stderr, jsonMode, "wait", "", err)
	}

	done := []string{}
	pending := []string{}
	failed := []string{}
	items := make([]map[string]any, 0, len(outcomes))
	observedTimeout := false
	for _, out := range outcomes {
		status := string(out.Instance.Status)
		if out.ErrorCode != "" {
			status = "error"
		} else if status == "" && !out.Done {
			status = "pending"
		}
		item := map[string]any{
			"instance":   out.Name,
			"done":       out.Done,
			"status":     status,
			"elapsed_ms": out.Snapshot.ElapsedMS,
		}
		if out.TimedOut {
			item["timed_out"] = true
			observedTimeout = true
		}
		switch {
		case out.ErrorCode != "":
			item["error_code"] = out.ErrorCode
			item["error"] = out.Error
			failed = append(failed, out.Name)
		case out.Done:
			done = append(done, out.Name)
		default:
			pending = append(pending, out.Name)
		}
		if out.Instance.Name != "" {
			for key, value := range waitSnapshotData(out.Instance, out.Snapshot) {
				item[key] = value
			}
		}
		if collect && out.Done && out.ErrorCode == "" {
			item["content"] = out.Snapshot.Content
			item["harness_type"] = out.Instance.HarnessType
			item["scope"] = string(capture.ScopeCurrent)
			for key, value := range out.Snapshot.Extra {
				item[key] = value
			}
		}
		items = append(items, item)
	}
	commandOK := len(failed) == 0
	timedOut := !ok && observedTimeout

	if jsonMode {
		_ = output.WriteJSON(stdout, output.Success{OK: commandOK, Command: "wait", Data: map[string]any{
			"mode":      string(mode),
			"satisfied": ok,
			"timed_out": timedOut,
			"done":      done,
			"pending":   pending,
			"failed":    failed,
			"instances": items,
		}})
		if !commandOK {
			return 1
		}
		return 0
	}
	rows := make([][]string, 0, len(outcomes))
	for _, out := range outcomes {
		state := "pending"
		switch {
		case out.ErrorCode != "":
			state = out.ErrorCode
		case out.Done:
			state = "done"
		case out.TimedOut:
			state = "timed_out"
		}
		status := string(out.Instance.Status)
		if status == "" {
			status = "-"
		}
		rows = append(rows, []string{out.Name, state, status, fmt.Sprintf("%dms", out.Snapshot.ElapsedMS)})
	}
	_ = output.RenderTable(stdout, []string{"Instance", "State", "Status", "Elapsed"}, rows)
	if collect {
		for _, out := range outcomes {
			if out.Done && out.ErrorCode == "" {
				fmt.Fprintf(stdout, "--- %s ---\n%s\n", out.Name, out.Snapshot.Content)
			}
		}
	}
	if len(failed) > 0 {
		fmt.Fprintf(stderr, "wait failed for: %s\n", strings.Join(failed, ", "))
	} else if !ok {
		fmt.Fprintf(stderr, "mode=%s timed out; pending: %s\n", mode, strings.Join(pending, ", "))
	}
	if !commandOK {
		return 1
	}
	return 0
}

func waitSnapshotData(inst instance.Instance, snap capture.Snapshot) map[string]any {
	data := map[string]any{}
	if service.IsStructuredHarness(inst.HarnessType) {
		return data
	}
	data["saw_busy"] = snap.SawBusy
	data["stable_for_ms"] = snap.StableForMS
	data["cursor_x"] = snap.CursorX
	data["cursor_y"] = snap.CursorY
	data["width"] = snap.Width
	data["height"] = snap.Height
	data["history_lines"] = snap.History
	data["pane_title"] = snap.PaneTitle
	return data
}
