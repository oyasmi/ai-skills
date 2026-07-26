package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/execjsonctl"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/ndjsonctl"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/rpcctl"
)

func TestHarnessForRoutesByHarnessType(t *testing.T) {
	svc, _ := newTestService(t, &fakeTmux{sessions: map[string]bool{}})

	if _, ok := svc.harnessFor(instance.Instance{HarnessType: "claude-code"}); ok {
		t.Fatal("tmux harnesses must not resolve to a structured controller")
	}
	h, ok := svc.harnessFor(instance.Instance{HarnessType: ndjsonctl.HarnessType})
	if !ok {
		t.Fatal("claude-code-ndjson must resolve")
	}
	if _, isNDJSON := h.(ndjsonctl.Controller); !isNDJSON {
		t.Fatalf("claude-code-ndjson routed to %T", h)
	}
	h, ok = svc.harnessFor(instance.Instance{HarnessType: execjsonctl.HarnessType})
	if !ok {
		t.Fatal("codex-cli-execjson must resolve")
	}
	if _, isExecJSON := h.(execjsonctl.Controller); !isExecJSON {
		t.Fatalf("codex-cli-execjson routed to %T", h)
	}
	h, ok = svc.harnessFor(instance.Instance{HarnessType: rpcctl.HarnessType})
	if !ok {
		t.Fatal("pi-rpc must resolve")
	}
	if _, isRPC := h.(rpcctl.Controller); !isRPC {
		t.Fatalf("pi-rpc routed to %T", h)
	}
}

// A codex-cli-execjson instance has no process between turns. Reconcile must
// keep it idle rather than pruning it as exited, otherwise every `list` between
// two prompts would delete the instance.
func TestListKeepsIdleExecJSONInstanceWithNoProcess(t *testing.T) {
	svc, registryPath := newTestService(t, &fakeTmux{sessions: map[string]bool{}})

	transport := t.TempDir()
	writeExecJSONState(t, transport, `{
	  "version": 1,
	  "thread_id": "thread-1",
	  "status": "idle",
	  "resume_available": true,
	  "turns": [{"index":0,"state":"completed","start_offset":0}],
	  "total_turns": 1
	}`)

	seedRegistry(t, registryPath, instance.Instance{
		Name:         "codex",
		Template:     "worker",
		SessionID:    "i_codex",
		HarnessType:  execjsonctl.HarnessType,
		TransportDir: transport,
		ThreadID:     "thread-1",
		ProcessID:    0,
		Status:       instance.StatusIdle,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})

	items, err := svc.List(context.Background(), false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("an idle execjson instance must survive list, got %d instances", len(items))
	}
	if items[0].Status != instance.StatusIdle {
		t.Fatalf("expected idle, got %s", items[0].Status)
	}
	if items[0].ThreadID != "thread-1" {
		t.Fatalf("expected the thread id to survive reconcile, got %q", items[0].ThreadID)
	}
}

// A missing transport dir is the only thing that makes an execjson instance lost.
func TestReconcileMarksExecJSONLostWhenStateIsGone(t *testing.T) {
	svc, _ := newTestService(t, &fakeTmux{sessions: map[string]bool{}})

	next, err := svc.reconcile(context.Background(), instance.Instance{
		Name:         "codex",
		HarnessType:  execjsonctl.HarnessType,
		TransportDir: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if next.Status != instance.StatusLost {
		t.Fatalf("expected lost when state.json is gone, got %s", next.Status)
	}
}

func writeExecJSONState(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

func seedRegistry(t *testing.T, path string, inst instance.Instance) {
	t.Helper()
	reg := instance.Registry{Instances: map[string]instance.Instance{inst.Name: inst}}
	b, err := json.Marshal(reg)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}

// An unset --history must stay bounded for structured harnesses: their message
// stream is one entry per protocol event, so "everything" is what floods an
// orchestrator's context.
func TestCaptureBoundsStructuredMessagesByDefault(t *testing.T) {
	svc, registryPath := newTestService(t, &fakeTmux{sessions: map[string]bool{}})

	transport := t.TempDir()
	writeExecJSONState(t, transport, `{
	  "version": 1,
	  "thread_id": "thread-1",
	  "status": "idle",
	  "resume_available": true,
	  "turns": [{"index":0,"state":"completed","start_offset":0}],
	  "total_turns": 1
	}`)
	var events strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&events, `{"type":"item.completed","item":{"id":"item_%d","type":"agent_message","text":"chunk %d"}}`+"\n", i, i)
	}
	if err := os.WriteFile(filepath.Join(transport, "output.jsonl"), []byte(events.String()), 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}

	seedRegistry(t, registryPath, instance.Instance{
		Name:         "codex",
		Template:     "worker",
		SessionID:    "i_codex",
		HarnessType:  execjsonctl.HarnessType,
		TransportDir: transport,
		ThreadID:     "thread-1",
		Status:       instance.StatusIdle,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})

	_, snap, err := svc.Capture(context.Background(), "codex", capture.Options{Scope: capture.ScopeCurrent, History: -1})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	msgs, ok := snap.Extra["messages"].([]execjsonctl.NormalizedMessage)
	if !ok {
		t.Fatalf("unexpected messages type %T", snap.Extra["messages"])
	}
	if len(msgs) != defaultStructuredMessages {
		t.Fatalf("expected the default to cap messages at %d, got %d", defaultStructuredMessages, len(msgs))
	}
	if msgs[len(msgs)-1].Text != "chunk 49" {
		t.Fatalf("expected the most recent messages to be kept, got %q", msgs[len(msgs)-1].Text)
	}
	if snap.Content != "chunk 49" {
		t.Fatalf("expected content to hold the latest agent message, got %q", snap.Content)
	}

	// An explicit 0 still means "no limit" for callers that want the full trace.
	_, full, err := svc.Capture(context.Background(), "codex", capture.Options{Scope: capture.ScopeCurrent, History: 0})
	if err != nil {
		t.Fatalf("capture unlimited: %v", err)
	}
	if got := len(full.Extra["messages"].([]execjsonctl.NormalizedMessage)); got != 50 {
		t.Fatalf("expected --history 0 to keep every message, got %d", got)
	}
}

// Polling a long run must cost what is new, not the whole transcript each time.
func TestCaptureSinceReadsOnlyNewOutput(t *testing.T) {
	svc, registryPath := newTestService(t, &fakeTmux{sessions: map[string]bool{}})

	transport := t.TempDir()
	writeExecJSONState(t, transport, `{
	  "version": 1,
	  "thread_id": "thread-1",
	  "status": "idle",
	  "resume_available": true,
	  "turns": [{"index":0,"state":"completed","start_offset":0}],
	  "total_turns": 1
	}`)
	output := filepath.Join(transport, "output.jsonl")
	writeEvents := func(from, to int) {
		var b strings.Builder
		for i := from; i < to; i++ {
			fmt.Fprintf(&b, `{"type":"item.completed","item":{"id":"item_%d","type":"agent_message","text":"chunk %d"}}`+"\n", i, i)
		}
		f, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatalf("open output: %v", err)
		}
		defer f.Close()
		if _, err := f.WriteString(b.String()); err != nil {
			t.Fatalf("write output: %v", err)
		}
	}
	writeEvents(0, 5)

	seedRegistry(t, registryPath, instance.Instance{
		Name:         "codex",
		Template:     "worker",
		SessionID:    "i_codex",
		HarnessType:  execjsonctl.HarnessType,
		TransportDir: transport,
		ThreadID:     "thread-1",
		Status:       instance.StatusIdle,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})

	_, first, err := svc.Capture(context.Background(), "codex", capture.Options{Scope: capture.ScopeCurrent, History: -1})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	cursor, _ := first.Extra["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("a structured capture must hand back a cursor to resume from")
	}

	// Nothing new yet: the same cursor must yield an empty read.
	_, idle, err := svc.Capture(context.Background(), "codex", capture.Options{Since: cursor})
	if err != nil {
		t.Fatalf("capture --since: %v", err)
	}
	if got := len(idle.Extra["messages"].([]execjsonctl.NormalizedMessage)); got != 0 {
		t.Fatalf("expected no new messages, got %d", got)
	}
	if idle.Extra["next_cursor"] != cursor {
		t.Fatalf("an empty read must not move the cursor: %v vs %v", idle.Extra["next_cursor"], cursor)
	}

	writeEvents(5, 8)
	_, next, err := svc.Capture(context.Background(), "codex", capture.Options{Since: cursor})
	if err != nil {
		t.Fatalf("capture --since: %v", err)
	}
	msgs := next.Extra["messages"].([]execjsonctl.NormalizedMessage)
	if len(msgs) != 3 {
		t.Fatalf("expected only the 3 new messages, got %d", len(msgs))
	}
	if msgs[0].Text != "chunk 5" {
		t.Fatalf("expected the read to resume at the cursor, got %q", msgs[0].Text)
	}
	if next.Extra["next_cursor"] == cursor {
		t.Fatal("expected the cursor to advance past the new events")
	}
}

// A cursor has no meaning for a terminal harness, and silently ignoring it
// would make a polling loop repeat the same screen forever.
func TestCaptureSinceRejectedForTerminalHarness(t *testing.T) {
	svc, registryPath := newTestService(t, &fakeTmux{sessions: map[string]bool{"live-session": true}})
	saveRunningInstance(t, registryPath, "worker", "live-session", instance.StatusIdle, true, time.Now().UTC())

	_, _, err := svc.Capture(context.Background(), "worker", capture.Options{Since: "0"})
	if err == nil || apperr.Code(err) != "invalid_arguments" {
		t.Fatalf("expected invalid_arguments, got %v", err)
	}
}

// A dead worker must stay diagnosable: inspect keeps answering, work commands
// explain what happened, and summon can take the name back.
func TestTombstoneKeepsADeadInstanceDiagnosable(t *testing.T) {
	ctx := context.Background()
	tmux := &fakeTmux{sessions: map[string]bool{}}
	svc, registryPath := newTestService(t, tmux)
	saveRunningInstance(t, registryPath, "worker", "gone-session", instance.StatusBusy, true, time.Now().UTC())

	if _, _, err := svc.Capture(ctx, "worker", capture.Options{}); err == nil ||
		apperr.Code(err) != "process_not_running" || !strings.Contains(err.Error(), "session_lost") {
		t.Fatalf("expected a process_not_running error naming the reason, got %v", err)
	}

	tomb, err := svc.Inspect(ctx, "worker")
	if err != nil {
		t.Fatalf("inspect must still answer for a tombstone: %v", err)
	}
	if tomb.Status != instance.StatusLost || tomb.EndReason != "session_lost" || tomb.EndedAt.IsZero() {
		t.Fatalf("unexpected tombstone: %+v", tomb)
	}

	res, err := svc.Summon(ctx, SummonInput{TemplateName: "worker", Name: "worker"})
	if err != nil {
		t.Fatalf("summon must be able to reclaim the name: %v", err)
	}
	if res.Reused {
		t.Fatal("a tombstone must not be reported as a reused instance")
	}
	if res.Instance.EndReason != "" || !res.Instance.EndedAt.IsZero() {
		t.Fatalf("the fresh instance must not inherit tombstone fields: %+v", res.Instance)
	}
}

// Tombstones are for diagnosis, not for holding capacity hostage.
func TestTombstonesExpireAndDoNotCountAgainstMaxInstances(t *testing.T) {
	ctx := context.Background()
	svc, registryPath := newTestService(t, &fakeTmux{sessions: map[string]bool{}})
	svc.Config.Defaults.MaxInstances = 1
	svc.Config.Defaults.Status.TombstoneTTLMS = intPointer(50)

	seedRegistry(t, registryPath, instance.Instance{
		Name:      "dead",
		Template:  "worker",
		SessionID: "dead-session",
		Status:    instance.StatusExited,
		EndReason: "halted",
		EndedAt:   time.Now().Add(-time.Second),
		UpdatedAt: time.Now().Add(-time.Second),
	})

	if _, err := svc.Summon(ctx, SummonInput{TemplateName: "worker", Name: "fresh"}); err != nil {
		t.Fatalf("a tombstone must not consume the instance budget: %v", err)
	}
	saved, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if _, ok := saved.Get("dead"); ok {
		t.Fatal("expected the expired tombstone to be swept")
	}
}
