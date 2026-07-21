package ndjsonctl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oyasmi/agentmux/internal/capture"
	"github.com/oyasmi/agentmux/internal/instance"
)

func TestBuildClaudeCommandAddsProtocolFlags(t *testing.T) {
	cmd := buildClaudeCommand("claude --model sonnet", "be terse", "550e8400-e29b-41d4-a716-446655440000", false)
	for _, want := range []string{
		"-p",
		"--input-format stream-json",
		"--output-format stream-json",
		"--verbose",
		"--include-partial-messages",
		"--replay-user-messages",
		"--session-id '550e8400-e29b-41d4-a716-446655440000'",
		"--append-system-prompt 'be terse'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected command to contain %q, got %q", want, cmd)
		}
	}
}

func TestControllerPromptWaitCaptureAndHalt(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeClaude(t, dir)
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:        "ndjson",
		Template:    "claude-code-ndjson",
		SessionID:   "i_test",
		HarnessType: HarnessType,
		CWD:         dir,
		Env:         map[string]string{},
		Status:      instance.StatusStarting,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	inst.ClaudeSessionID = "550e8400-e29b-41d4-a716-446655440000"
	inst, err := ctrl.Start(context.Background(), inst, fake, "", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if inst.ProcessID <= 0 || inst.ProcessGroupID <= 0 {
		t.Fatalf("expected process metadata, got pid=%d pgid=%d", inst.ProcessID, inst.ProcessGroupID)
	}
	inst, err = ctrl.SendPrompt(context.Background(), inst, "hello")
	if err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	if inst.Status != instance.StatusBusy {
		t.Fatalf("expected busy after prompt, got %s", inst.Status)
	}
	if _, err := ctrl.Wait(context.Background(), inst, 2*time.Second); err != nil {
		t.Fatalf("wait: %v", err)
	}
	snap, err := ctrl.Capture(context.Background(), inst, 0, capture.ScopeCurrent)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if snap.Content != "done" {
		t.Fatalf("expected result content, got %q", snap.Content)
	}
	if snap.Extra["claude_session_id"] != inst.ClaudeSessionID {
		t.Fatalf("expected claude session id in extra")
	}
	if err := ctrl.Halt(context.Background(), inst, true, 0); err != nil {
		t.Fatalf("halt: %v", err)
	}
}

func TestStartKillsProcessWhenStatePersistenceFails(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeClaude(t, dir)
	if err := os.Mkdir(filepath.Join(dir, stateFileName), 0o700); err != nil {
		t.Fatalf("block state path: %v", err)
	}
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:         "claude",
		SessionID:    "i_start_rollback",
		TransportDir: dir,
		CWD:          dir,
		Env:          map[string]string{},
	}

	if _, err := ctrl.Start(context.Background(), inst, fake, "", false); err == nil {
		t.Fatal("expected state persistence failure")
	}
	b, err := os.ReadFile(filepath.Join(dir, processFileName))
	if err != nil {
		t.Fatalf("read process metadata: %v", err)
	}
	var meta processMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatalf("parse process metadata: %v", err)
	}
	waitUntilProcessStops(t, meta.PID)
}

func TestHaltReturnsCleanupError(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocking path: %v", err)
	}
	ctrl := Controller{StateDir: dir}
	inst := instance.Instance{TransportDir: blocked}

	if err := ctrl.Halt(context.Background(), inst, true, 0); err == nil {
		t.Fatal("expected halt cleanup error")
	}
}

func TestWriteFIFOTimeoutDoesNotWaitForReader(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "input.fifo")
	if err := ensureFIFO(fifo); err != nil {
		t.Fatalf("ensure fifo: %v", err)
	}

	start := time.Now()
	err := writeFIFO(context.Background(), fifo, []byte("late\n"), 50*time.Millisecond)
	if err == nil {
		t.Fatalf("expected timeout without fifo reader")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("writeFIFO blocked too long: %s", elapsed)
	}

	// Opening a reader after the timeout must not receive a delayed write from
	// a leftover goroutine.
	rfd, err := syscall.Open(fifo, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open fifo reader: %v", err)
	}
	defer syscall.Close(rfd)
	buf := make([]byte, 16)
	n, err := syscall.Read(rfd, buf)
	if n > 0 || (err != nil && err != syscall.EAGAIN) {
		t.Fatalf("unexpected stale fifo data n=%d err=%v data=%q", n, err, string(buf[:n]))
	}
}

func TestSyncStateReplaysFromPendingStartWithoutDoubleCounting(t *testing.T) {
	dir := t.TempDir()
	user := `{"type":"user","uuid":"prompt-1","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}` + "\n"
	resultOffset := int64(len(user))
	result := `{"type":"result","subtype":"success","is_error":false,"result":"done","total_cost_usd":0.01,"usage":{"input_tokens":2,"output_tokens":3}}` + "\n"
	idle := `{"type":"system","subtype":"session_state_changed","state":"idle"}` + "\n"
	output := filepath.Join(dir, outputJSONLName)
	if err := os.WriteFile(output, []byte(user+result+idle), 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	st := initialState("session-1", time.Now().UTC())
	st.LastReadOffset = int64(len(user + result + idle))
	st.LastResultOffset = resultOffset
	st.TotalTurns = 1
	st.TotalCostUSD = 0.01
	st.TotalInputTokens = 2
	st.TotalOutputTokens = 3
	st.SessionIdle = false
	st.Status = "busy"
	st.PendingPrompts = []PendingPrompt{{
		UUID:         "prompt-1",
		StartOffset:  0,
		ResultOffset: resultOffset,
		State:        PromptResult,
	}}
	if err := saveState(filepath.Join(dir, stateFileName), st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	ctrl := Controller{PollMS: 10}
	got, err := ctrl.syncState(instance.Instance{TransportDir: dir})
	if err != nil {
		t.Fatalf("sync state: %v", err)
	}
	if !got.SessionIdle || got.Status != "idle" || got.PendingPrompts[0].State != PromptIdle {
		t.Fatalf("expected idle prompt after replay, got status=%s idle=%v prompt=%s", got.Status, got.SessionIdle, got.PendingPrompts[0].State)
	}
	if got.TotalTurns != 1 || got.TotalInputTokens != 2 || got.TotalOutputTokens != 3 || got.TotalCostUSD != 0.01 {
		t.Fatalf("expected no duplicate accounting, got turns=%d input=%d output=%d cost=%v", got.TotalTurns, got.TotalInputTokens, got.TotalOutputTokens, got.TotalCostUSD)
	}
}

func TestNormalizeEventsUsesAuthoritativeResultTextAndUsage(t *testing.T) {
	events := []Event{
		{Type: "stream_event", Event: StreamEvent{Delta: json.RawMessage(`{"type":"text_delta","text":"do"}`)}},
		{Type: "stream_event", Event: StreamEvent{Type: "message_delta", Delta: json.RawMessage(`{"type":"text_delta","text":"ne"}`), Usage: Usage{InputTokens: 5, OutputTokens: 6}}},
		{Type: "assistant", Message: Message{Content: json.RawMessage(`[{"type":"text","text":"done"}]`), Usage: Usage{InputTokens: 7, OutputTokens: 8}}},
		{Type: "result", Result: "done", Usage: Usage{InputTokens: 2, OutputTokens: 3}, CostUSD: 0.01},
	}

	_, content, usage, cost := normalizeEvents(events)
	if content != "done" {
		t.Fatalf("expected final result content without duplicated deltas, got %q", content)
	}
	if usage.InputTokens != 2 || usage.OutputTokens != 3 || cost != 0.01 {
		t.Fatalf("expected result usage/cost, got input=%d output=%d cost=%v", usage.InputTokens, usage.OutputTokens, cost)
	}
}

func writeFakeClaude(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-claude")
	script := `#!/bin/sh
printf '{"type":"system","subtype":"init","session_id":"550e8400-e29b-41d4-a716-446655440000"}\n'
while IFS= read -r line; do
  uuid=$(printf '%s' "$line" | sed -n 's/.*"uuid":"\([^"]*\)".*/\1/p')
  printf '{"type":"user","uuid":"%s","session_id":"550e8400-e29b-41d4-a716-446655440000","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}\n' "$uuid"
  printf '{"type":"assistant","session_id":"550e8400-e29b-41d4-a716-446655440000","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":1,"output_tokens":1}}}\n'
  printf '{"type":"result","subtype":"success","is_error":false,"result":"done","total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"session_id":"550e8400-e29b-41d4-a716-446655440000"}\n'
  printf '{"type":"system","subtype":"session_state_changed","state":"idle","session_id":"550e8400-e29b-41d4-a716-446655440000"}\n'
done
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	return path
}

func waitUntilProcessStops(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("spawned process %d survived failed startup transaction", pid)
	}
}

// TestInterruptIdleInstanceDoesNotWedgeBusy guards the fix for interrupt
// wedging: with no turn in flight, Interrupt must not force the instance into a
// busy state it can never leave.
func TestInterruptIdleInstanceDoesNotWedgeBusy(t *testing.T) {
	dir := t.TempDir()
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:         "ndjson",
		SessionID:    "i_idle",
		HarnessType:  HarnessType,
		TransportDir: dir,
		ProcessID:    os.Getpid(), // a live pid; ProcessGroupID 0 so no signal is sent
	}
	st := initialState("550e8400-e29b-41d4-a716-446655440000", nowUTC())
	st.Status = "idle"
	if err := saveState(statePath(inst), st); err != nil {
		t.Fatalf("save state: %v", err)
	}
	got, err := ctrl.Interrupt(context.Background(), inst)
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if got.Status == instance.StatusBusy {
		t.Fatalf("interrupt on idle instance wedged status to busy")
	}
}

func TestReconcileDegradesSilentInterruptedPromptToIdle(t *testing.T) {
	dir := t.TempDir()
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:         "ndjson",
		SessionID:    "i_interrupted",
		HarnessType:  HarnessType,
		TransportDir: dir,
		ProcessID:    os.Getpid(),
	}
	if err := os.WriteFile(outputPath(inst), nil, 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	st := initialState("550e8400-e29b-41d4-a716-446655440000", nowUTC())
	st.Status = "busy"
	st.SessionIdle = false
	st.InterruptedAt = nowUTC().Add(-interruptSilenceGrace - time.Second)
	st.LastError = "interrupted"
	st.PendingPrompts = []PendingPrompt{{UUID: "prompt-1", State: PromptCancelled}}
	if err := saveState(statePath(inst), st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	got, err := ctrl.Reconcile(context.Background(), inst)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got.Status != instance.StatusIdle {
		t.Fatalf("silent interrupted prompt must degrade to idle, got %s", got.Status)
	}
	saved, err := loadState(statePath(inst))
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if !saved.SessionIdle || !saved.InterruptedAt.IsZero() || saved.LastError != "interrupted" {
		t.Fatalf("expected persisted interrupted idle state, got %+v", saved)
	}
}

func TestReconcileKeepsRecentlyInterruptedPromptBusy(t *testing.T) {
	dir := t.TempDir()
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{TransportDir: dir, ProcessID: os.Getpid()}
	if err := os.WriteFile(outputPath(inst), nil, 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	st := initialState("550e8400-e29b-41d4-a716-446655440000", nowUTC())
	st.Status = "busy"
	st.InterruptedAt = nowUTC()
	st.PendingPrompts = []PendingPrompt{{UUID: "prompt-1", State: PromptCancelled}}
	if err := saveState(statePath(inst), st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	got, err := ctrl.Reconcile(context.Background(), inst)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got.Status != instance.StatusBusy {
		t.Fatalf("recently interrupted prompt must remain busy during grace period, got %s", got.Status)
	}
}

// TestWaitReturnsImmediatelyBeforeFirstPrompt guards the fix for wait hanging on
// a freshly summoned instance that was never prompted (SessionIdle is still
// false but nothing is in flight).
func TestWaitReturnsImmediatelyBeforeFirstPrompt(t *testing.T) {
	dir := t.TempDir()
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:         "ndjson",
		SessionID:    "i_new",
		HarnessType:  HarnessType,
		TransportDir: dir,
		ProcessID:    os.Getpid(),
	}
	st := initialState("550e8400-e29b-41d4-a716-446655440000", nowUTC())
	st.Status = "idle"
	st.SessionIdle = false
	if err := saveState(statePath(inst), st); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if _, err := ctrl.Wait(context.Background(), inst, 500*time.Millisecond); err != nil {
		t.Fatalf("wait before first prompt should return immediately, got %v", err)
	}
}
