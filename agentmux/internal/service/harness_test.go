package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oyasmi/agentmux/internal/execjsonctl"
	"github.com/oyasmi/agentmux/internal/instance"
	"github.com/oyasmi/agentmux/internal/ndjsonctl"
	"github.com/oyasmi/agentmux/internal/rpcctl"
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

	items, err := svc.List(context.Background())
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

	next := svc.reconcile(context.Background(), instance.Instance{
		Name:         "codex",
		HarnessType:  execjsonctl.HarnessType,
		TransportDir: filepath.Join(t.TempDir(), "does-not-exist"),
	})
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
