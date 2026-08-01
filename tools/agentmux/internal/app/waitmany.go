package app

import (
	"context"
	"fmt"
	"io"
	"strings"

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
	for _, out := range outcomes {
		item := map[string]any{
			"instance":   out.Name,
			"done":       out.Done,
			"status":     string(out.Instance.Status),
			"elapsed_ms": out.Snapshot.ElapsedMS,
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
		if out.Instance.Name != "" && !service.IsStructuredHarness(out.Instance.HarnessType) {
			item["saw_busy"] = out.Snapshot.SawBusy
			item["pane_title"] = out.Snapshot.PaneTitle
		}
		if collect && out.Done && out.ErrorCode == "" {
			item["content"] = out.Snapshot.Content
		}
		items = append(items, item)
	}

	if jsonMode {
		_ = output.WriteJSON(stdout, output.Success{OK: true, Command: "wait", Data: map[string]any{
			"mode":      string(mode),
			"satisfied": ok,
			"timed_out": !ok,
			"done":      done,
			"pending":   pending,
			"failed":    failed,
			"instances": items,
		}})
		return 0
	}
	for _, out := range outcomes {
		state := "pending"
		switch {
		case out.ErrorCode != "":
			state = out.ErrorCode
		case out.Done:
			state = "done"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%dms\n", out.Name, state, out.Instance.Status, out.Snapshot.ElapsedMS)
		if collect && out.Done && out.ErrorCode == "" {
			fmt.Fprintf(stdout, "--- %s ---\n%s\n", out.Name, out.Snapshot.Content)
		}
	}
	if !ok {
		fmt.Fprintf(stderr, "mode=%s not satisfied; pending: %s\n", mode, strings.Join(pending, ", "))
	}
	return 0
}
