package rpcctl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
)

func TestBuildPiCommandAddsRPCFlags(t *testing.T) {
	cmd := buildPiCommand("pi --model sonnet", "be terse", "550e8400-e29b-41d4-a716-446655440000")
	for _, want := range []string{
		"--mode rpc",
		"--session-id '550e8400-e29b-41d4-a716-446655440000'",
		"--append-system-prompt 'be terse'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected command to contain %q, got %q", want, cmd)
		}
	}
}

func TestBuildPiCommandRespectsExistingSystemPromptFlag(t *testing.T) {
	cmd := buildPiCommand("pi --system-prompt custom", "ignored", "s1")
	if strings.Contains(cmd, "--append-system-prompt") {
		t.Fatalf("must not append system prompt when one is already set: %q", cmd)
	}
}

func TestControllerPromptWaitCaptureAndHalt(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakePi(t, dir)
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:        "pi",
		Template:    "pi-rpc",
		SessionID:   "i_test",
		HarnessType: HarnessType,
		CWD:         dir,
		Env:         map[string]string{},
		Status:      instance.StatusStarting,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	inst.PiSessionID = "550e8400-e29b-41d4-a716-446655440000"
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
	if _, err := ctrl.Wait(context.Background(), inst, 3*time.Second); err != nil {
		t.Fatalf("wait: %v", err)
	}
	snap, err := ctrl.Capture(context.Background(), inst, 0, capture.ScopeCurrent)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if snap.Content != "done" {
		t.Fatalf("expected result content, got %q", snap.Content)
	}
	if snap.Extra["pi_session_id"] != inst.PiSessionID {
		t.Fatalf("expected pi session id in extra, got %v", snap.Extra["pi_session_id"])
	}
	if snap.Extra["turns"].(int) != 1 {
		t.Fatalf("expected one turn, got %v", snap.Extra["turns"])
	}
	usage := snap.Extra["usage"].(map[string]any)
	if usage["output_tokens"].(int64) != 2 || usage["total_cost_usd"].(float64) != 0.01 {
		t.Fatalf("expected accumulated usage from turn_end, got %v", usage)
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
	fake := writeFakePi(t, dir)
	if err := os.Mkdir(filepath.Join(dir, stateFileName), 0o700); err != nil {
		t.Fatalf("block state path: %v", err)
	}
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:         "pi",
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

// TestApplyEventsPromptLifecycle drives the state machine through one prompt's
// full arc: sent -> accepted (response) -> busy (agent_start) -> done+idle
// (agent_settled).
func TestApplyEventsPromptLifecycle(t *testing.T) {
	st := initialState("s1", nowUTC())
	st.PendingPrompts = []PendingPrompt{{ID: "p1", State: PromptSent}}
	st.Status = "busy"

	applyEvents(&st, []Event{{Type: "response", Command: "prompt", ID: "p1", Success: true}})
	if st.PendingPrompts[0].State != PromptAccepted || st.Status != "busy" {
		t.Fatalf("after response: state=%s status=%s", st.PendingPrompts[0].State, st.Status)
	}

	applyEvents(&st, []Event{{Type: "agent_start"}})
	if !st.AgentRunActive || st.Status != "busy" {
		t.Fatalf("after agent_start: active=%v status=%s", st.AgentRunActive, st.Status)
	}

	applyEvents(&st, []Event{
		{Type: "turn_end", Message: AssistantMessage{Role: "assistant", Usage: mkUsage(1, 2, 0.01)}},
		{Type: "agent_settled"},
	})
	if st.AgentRunActive {
		t.Fatalf("agent_settled must clear AgentRunActive")
	}
	if st.PendingPrompts[0].State != PromptDone {
		t.Fatalf("agent_settled must mark prompt done, got %s", st.PendingPrompts[0].State)
	}
	if st.Status != "idle" {
		t.Fatalf("expected idle after settle, got %s", st.Status)
	}
	if st.TotalTurns != 1 || st.TotalOutputTokens != 2 || st.TotalCostUSD != 0.01 {
		t.Fatalf("usage accounting wrong: turns=%d out=%d cost=%v", st.TotalTurns, st.TotalOutputTokens, st.TotalCostUSD)
	}
	if !st.ResumeAvailable {
		t.Fatalf("a settled run must be resumable")
	}
}

func TestApplyEventsRejectedPrompt(t *testing.T) {
	st := initialState("s1", nowUTC())
	st.PendingPrompts = []PendingPrompt{{ID: "p1", State: PromptSent}}
	st.Status = "busy"
	applyEvents(&st, []Event{{Type: "response", Command: "prompt", ID: "p1", Success: false, Error: "bad model"}})
	if st.PendingPrompts[0].State != PromptFailed {
		t.Fatalf("expected failed, got %s", st.PendingPrompts[0].State)
	}
	if st.LastError != "bad model" {
		t.Fatalf("expected error recorded, got %q", st.LastError)
	}
	if st.Status != "idle" {
		t.Fatalf("a rejected prompt leaves nothing in flight; want idle, got %s", st.Status)
	}
}

// TestApplyEventsQueuedFollowUpDrainsInOneSettle covers a prompt sent while the
// agent is already streaming: pi queues it as a follow-up and drains it within
// the same run, so a single agent_settled completes both prompts.
func TestApplyEventsQueuedFollowUpDrainsInOneSettle(t *testing.T) {
	st := initialState("s1", nowUTC())
	st.AgentRunActive = true
	st.PendingPrompts = []PendingPrompt{
		{ID: "p1", State: PromptAccepted},
		{ID: "p2", State: PromptAccepted},
	}
	st.Status = "busy"
	applyEvents(&st, []Event{{Type: "agent_settled"}})
	for _, p := range st.PendingPrompts {
		if p.State != PromptDone {
			t.Fatalf("prompt %s not drained by settle: %s", p.ID, p.State)
		}
	}
	if st.Status != "idle" {
		t.Fatalf("expected idle, got %s", st.Status)
	}
	if st.TotalTurns != 2 {
		t.Fatalf("draining two queued prompts must count two turns, got %d", st.TotalTurns)
	}
}

// TestApplyEventsTurnEndDoesNotInflateTurns locks in that pi's per-step turn_end
// events (thinking, tool call, tool result) count only usage, never turns. A
// single user prompt that runs a tool and settles is exactly one turn, however
// many turn_end events pi streams within it.
func TestApplyEventsTurnEndDoesNotInflateTurns(t *testing.T) {
	st := initialState("s1", nowUTC())
	st.PendingPrompts = []PendingPrompt{{ID: "p1", State: PromptAccepted}}
	st.AgentRunActive = true
	st.Status = "busy"

	events := []Event{{Type: "agent_start"}}
	for i := 0; i < 16; i++ {
		events = append(events, Event{Type: "turn_end", Message: AssistantMessage{Role: "assistant", Usage: mkUsage(1, 1, 0.001)}})
	}
	events = append(events, Event{Type: "agent_settled"})
	applyEvents(&st, events)

	if st.TotalTurns != 1 {
		t.Fatalf("16 turn_end steps in one run must count one turn, got %d", st.TotalTurns)
	}
	if st.TotalOutputTokens != 16 {
		t.Fatalf("usage must still accumulate per turn_end, got %d output tokens", st.TotalOutputTokens)
	}
}

func TestApplyEventsDoesNotDoubleCountReplayedUsage(t *testing.T) {
	st := initialState("s1", nowUTC())
	event := Event{
		Type:      "turn_end",
		EndOffset: 128,
		Message: AssistantMessage{
			Role:  "assistant",
			Usage: mkUsage(3, 5, 0.02),
		},
	}

	applyEvents(&st, []Event{event})
	applyEvents(&st, []Event{event})

	if st.TotalInputTokens != 3 || st.TotalOutputTokens != 5 || st.TotalCostUSD != 0.02 {
		t.Fatalf("replayed usage must be counted once, got input=%d output=%d cost=%v", st.TotalInputTokens, st.TotalOutputTokens, st.TotalCostUSD)
	}
	if st.LastUsageOffset != event.EndOffset {
		t.Fatalf("expected usage offset %d, got %d", event.EndOffset, st.LastUsageOffset)
	}
}

func TestLoadStateMigratesUsageOffsetFromReadOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), stateFileName)
	body := []byte(`{"version":1,"status":"busy","last_read_offset":256,"total_input_tokens":7,"pending_prompts":[]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	st, err := loadState(path)
	if err != nil {
		t.Fatalf("load legacy state: %v", err)
	}
	if st.LastUsageOffset != 256 {
		t.Fatalf("expected usage cursor to migrate to 256, got %d", st.LastUsageOffset)
	}
}

func TestNormalizeEventsFromMessageEnd(t *testing.T) {
	events := []Event{
		{Type: "message_start", Message: AssistantMessage{Role: "assistant"}},
		{Type: "message_end", Message: AssistantMessage{
			Role:    "assistant",
			Content: json.RawMessage(`[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"done"},{"type":"toolCall","name":"bash","arguments":{"command":"ls"}}]`),
			Usage:   mkUsage(3, 4, 0.02),
		}},
	}
	msgs, content, usage := normalizeEvents(events)
	if content != "done" {
		t.Fatalf("expected assistant text, got %q", content)
	}
	if usage.Output != 4 {
		t.Fatalf("expected usage from message_end, got %+v", usage)
	}
	var sawThinking, sawTool bool
	for _, m := range msgs {
		if m.ContentType == "thinking" && m.Text == "hmm" {
			sawThinking = true
		}
		if m.Type == "tool_use" && m.Tool == "bash" {
			sawTool = true
		}
	}
	if !sawThinking || !sawTool {
		t.Fatalf("expected thinking and tool_use normalized, got %+v", msgs)
	}
}

func TestInterruptIdleInstanceDoesNotWedgeBusy(t *testing.T) {
	dir := t.TempDir()
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:         "pi",
		SessionID:    "i_idle",
		HarnessType:  HarnessType,
		TransportDir: dir,
		ProcessID:    os.Getpid(), // live pid; ProcessGroupID 0 so no signal is sent
	}
	if err := os.WriteFile(outputPath(inst), nil, 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	st := initialState("s1", nowUTC())
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

func TestWaitReturnsImmediatelyBeforeFirstPrompt(t *testing.T) {
	dir := t.TempDir()
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:         "pi",
		SessionID:    "i_new",
		HarnessType:  HarnessType,
		TransportDir: dir,
		ProcessID:    os.Getpid(),
	}
	if err := os.WriteFile(outputPath(inst), nil, 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	st := initialState("s1", nowUTC())
	st.Status = "idle"
	if err := saveState(statePath(inst), st); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if _, err := ctrl.Wait(context.Background(), inst, 500*time.Millisecond); err != nil {
		t.Fatalf("wait before first prompt should return immediately, got %v", err)
	}
}

func TestReconcileDegradesSilentInterruptToIdle(t *testing.T) {
	dir := t.TempDir()
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:         "pi",
		SessionID:    "i_interrupted",
		HarnessType:  HarnessType,
		TransportDir: dir,
		ProcessID:    os.Getpid(),
	}
	if err := os.WriteFile(outputPath(inst), nil, 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	st := initialState("s1", nowUTC())
	st.Status = "busy"
	st.AgentRunActive = true
	st.InterruptedAt = nowUTC().Add(-interruptSilenceGrace - time.Second)
	st.LastError = "interrupted"
	st.PendingPrompts = []PendingPrompt{{ID: "p1", State: PromptCancelled}}
	if err := saveState(statePath(inst), st); err != nil {
		t.Fatalf("save state: %v", err)
	}
	got, err := ctrl.Reconcile(context.Background(), inst)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got.Status != instance.StatusIdle {
		t.Fatalf("silent interrupt must degrade to idle, got %s", got.Status)
	}
}

func TestReconcileKeepsRecentlyInterruptedBusy(t *testing.T) {
	dir := t.TempDir()
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{TransportDir: dir, ProcessID: os.Getpid()}
	if err := os.WriteFile(outputPath(inst), nil, 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	st := initialState("s1", nowUTC())
	st.Status = "busy"
	st.AgentRunActive = true
	st.InterruptedAt = nowUTC()
	st.PendingPrompts = []PendingPrompt{{ID: "p1", State: PromptCancelled}}
	if err := saveState(statePath(inst), st); err != nil {
		t.Fatalf("save state: %v", err)
	}
	got, err := ctrl.Reconcile(context.Background(), inst)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got.Status != instance.StatusBusy {
		t.Fatalf("recently interrupted must remain busy during grace, got %s", got.Status)
	}
}

// TestSyncStateAutoCancelsExtensionDialog verifies that a blocking extension
// dialog is dismissed so a headless run cannot hang on it.
func TestSyncStateAutoCancelsExtensionDialog(t *testing.T) {
	dir := t.TempDir()
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:         "pi",
		SessionID:    "i_dialog",
		HarnessType:  HarnessType,
		TransportDir: dir,
		ProcessID:    os.Getpid(),
	}
	if err := ensureFIFO(filepath.Join(dir, inputFIFOName)); err != nil {
		t.Fatalf("ensure fifo: %v", err)
	}
	// Keep the FIFO readable so the cancellation write succeeds without a real pi.
	reader := openFIFOReader(t, filepath.Join(dir, inputFIFOName))
	defer reader.Close()

	event := `{"type":"extension_ui_request","id":"dlg-1","method":"confirm","title":"ok?"}` + "\n"
	if err := os.WriteFile(outputPath(inst), []byte(event), 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	st := initialState("s1", nowUTC())
	st.Status = "idle"
	if err := saveState(statePath(inst), st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if _, err := ctrl.syncState(context.Background(), inst); err != nil {
		t.Fatalf("sync: %v", err)
	}
	saved, err := loadState(statePath(inst))
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(saved.UIRequests) != 1 || !saved.UIRequests[0].Handled {
		t.Fatalf("expected dialog recorded and handled, got %+v", saved.UIRequests)
	}
	line := reader.ReadLine(t, time.Second)
	var got uiCancel
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("parse cancel %q: %v", line, err)
	}
	if got.Type != "extension_ui_response" || got.ID != "dlg-1" || !got.Cancelled {
		t.Fatalf("unexpected cancel payload: %+v", got)
	}
}

// fifoReader holds a non-blocking read descriptor on the input FIFO so a test
// can both satisfy the controller's write-side open and inspect what was written.
type fifoReader struct {
	fd  int
	buf []byte
}

func openFIFOReader(t *testing.T, path string) *fifoReader {
	t.Helper()
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open fifo reader: %v", err)
	}
	return &fifoReader{fd: fd}
}

func (r *fifoReader) Close() { _ = syscall.Close(r.fd) }

func (r *fifoReader) ReadLine(t *testing.T, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if i := indexByte(r.buf, '\n'); i >= 0 {
			line := string(r.buf[:i])
			r.buf = r.buf[i+1:]
			return line
		}
		chunk := make([]byte, 4096)
		n, err := syscall.Read(r.fd, chunk)
		if n > 0 {
			r.buf = append(r.buf, chunk[:n]...)
			continue
		}
		if err != nil && err != syscall.EAGAIN && err != syscall.EWOULDBLOCK {
			t.Fatalf("read fifo: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out reading fifo line")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

func mkUsage(in, out int64, cost float64) Usage {
	var u Usage
	u.Input = in
	u.Output = out
	u.Cost.Total = cost
	return u
}

func writeFakePi(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-pi")
	script := `#!/bin/sh
while IFS= read -r line; do
  type=$(printf '%s' "$line" | sed -n 's/.*"type":"\([^"]*\)".*/\1/p')
  id=$(printf '%s' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  if [ "$type" = "prompt" ]; then
    printf '{"type":"response","id":"%s","command":"prompt","success":true}\n' "$id"
    printf '{"type":"agent_start"}\n'
    printf '{"type":"message_start","message":{"role":"assistant"}}\n'
    printf '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input":1,"output":2,"cacheRead":0,"cacheWrite":0,"cost":{"total":0.01}}}}\n'
    printf '{"type":"turn_end","message":{"role":"assistant","usage":{"input":1,"output":2,"cacheRead":0,"cacheWrite":0,"cost":{"total":0.01}}}}\n'
    printf '{"type":"agent_settled"}\n'
  fi
done
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake pi: %v", err)
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
