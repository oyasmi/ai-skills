package execjsonctl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
)

// writeFakeCodex emits the same event shapes as `codex exec --json`, including
// re-announcing the thread id on resume. Behavior is switched by env:
//
//	FAKE_MODE=ok       normal turn
//	FAKE_MODE=fail     emits turn.failed and exits 1
//	FAKE_MODE=crash    exits 1 with no terminal event
//	FAKE_MODE=hang     sleeps until killed
func writeFakeCodex(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "codex")
	script := `#!/bin/sh
# Args look like: exec [flags...] [resume <tid>] --json -
tid=""
for a in "$@"; do
  if [ "$prev" = "resume" ]; then tid="$a"; fi
  prev="$a"
done
if [ -z "$tid" ]; then tid="thread-fake-1"; fi
prompt=$(cat)
mode="${FAKE_MODE:-ok}"

printf '{"type":"thread.started","thread_id":"%s"}\n' "$tid"
case "$mode" in
  hang)
    sleep 30
    ;;
  crash)
    printf 'fake codex exploded\n' >&2
    exit 1
    ;;
  fail)
    printf '{"type":"turn.started"}\n'
    printf '{"type":"turn.failed","error":{"message":"model refused"}}\n'
    exit 1
    ;;
  *)
    printf '{"type":"turn.started"}\n'
    printf '{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"echo:%s"}}\n' "$prompt"
    printf '{"type":"turn.completed","usage":{"input_tokens":5,"cached_input_tokens":1,"output_tokens":2,"reasoning_output_tokens":0}}\n'
    ;;
esac
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	return path
}

func newTestInstance(t *testing.T, dir, fakeBin string, env map[string]string) (Controller, instance.Instance) {
	t.Helper()
	ctrl := Controller{StateDir: dir, PollMS: 10}
	if env == nil {
		env = map[string]string{}
	}
	// Put the fake `codex` first on PATH so the real prefix validation applies.
	env["PATH"] = filepath.Dir(fakeBin) + ":" + os.Getenv("PATH")
	inst := instance.Instance{
		Name:        "codex",
		Template:    HarnessType,
		SessionID:   "i_test",
		HarnessType: HarnessType,
		CWD:         dir,
		Env:         env,
		Status:      instance.StatusStarting,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	return ctrl, inst
}

func waitIdle(t *testing.T, ctrl Controller, inst instance.Instance) instance.Instance {
	t.Helper()
	if _, err := ctrl.Wait(context.Background(), inst, 10*time.Second); err != nil {
		t.Fatalf("wait: %v", err)
	}
	next, err := ctrl.Reconcile(context.Background(), inst)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return next
}

func TestStartSpawnsNoProcess(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, nil)

	inst, err := ctrl.Start(context.Background(), inst, "codex exec --skip-git-repo-check", "", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if inst.ProcessID != 0 || inst.ProcessGroupID != 0 {
		t.Fatalf("summon must not spawn a process, got pid=%d", inst.ProcessID)
	}
	if inst.Status != instance.StatusIdle {
		t.Fatalf("expected idle after start, got %s", inst.Status)
	}
	if ctrl.CanResume(inst) {
		t.Fatal("an instance that never prompted has no resumable thread")
	}
}

func TestPromptWaitCaptureThenResumeSecondTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, nil)

	inst, err := ctrl.Start(context.Background(), inst, "codex exec --skip-git-repo-check", "", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	inst, err = ctrl.SendPrompt(context.Background(), inst, "hello")
	if err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	if inst.Status != instance.StatusBusy || inst.ProcessID <= 0 {
		t.Fatalf("expected a busy instance with a live turn, got %s pid=%d", inst.Status, inst.ProcessID)
	}

	inst = waitIdle(t, ctrl, inst)
	if inst.Status != instance.StatusIdle {
		t.Fatalf("expected idle after the turn, got %s", inst.Status)
	}
	if inst.ProcessID != 0 {
		t.Fatalf("no process should remain between turns, got pid=%d", inst.ProcessID)
	}
	if inst.ThreadID != "thread-fake-1" {
		t.Fatalf("expected the thread id to be recorded, got %q", inst.ThreadID)
	}
	if !ctrl.CanResume(inst) {
		t.Fatal("a thread that emitted thread.started must be resumable")
	}

	snap, err := ctrl.Capture(context.Background(), inst, 0, capture.ScopeCurrent)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !strings.Contains(snap.Content, "echo:hello") {
		t.Fatalf("expected the agent message as content, got %q", snap.Content)
	}
	if snap.Extra["thread_id"] != "thread-fake-1" {
		t.Fatalf("expected thread_id in capture data, got %v", snap.Extra["thread_id"])
	}
	if snap.Extra["turn_state"] != string(TurnCompleted) {
		t.Fatalf("expected a completed turn, got %v", snap.Extra["turn_state"])
	}

	// Second prompt must resume the recorded thread rather than start a new one.
	inst, err = ctrl.SendPrompt(context.Background(), inst, "again")
	if err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	inst = waitIdle(t, ctrl, inst)

	script, err := os.ReadFile(runScriptPath(inst.TransportDir, 1))
	if err != nil {
		t.Fatalf("read turn 1 script: %v", err)
	}
	if !strings.Contains(string(script), "resume 'thread-fake-1'") {
		t.Fatalf("second turn must resume the thread, got:\n%s", script)
	}
	current, err := ctrl.Capture(context.Background(), inst, 0, capture.ScopeCurrent)
	if err != nil {
		t.Fatalf("capture current: %v", err)
	}
	if strings.Contains(messagesText(current), "echo:hello") || !strings.Contains(messagesText(current), "echo:again") {
		t.Fatalf("current scope must only include the latest turn, got %s", messagesText(current))
	}
	session, err := ctrl.Capture(context.Background(), inst, 0, capture.ScopeSession)
	if err != nil {
		t.Fatalf("capture session: %v", err)
	}
	if !strings.Contains(messagesText(session), "echo:hello") || !strings.Contains(messagesText(session), "echo:again") {
		t.Fatalf("session scope must include both turns, got %s", messagesText(session))
	}

	st, err := loadState(statePath(inst))
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if st.TotalTurns != 2 || st.TotalInputTokens != 10 || st.TotalCachedInputTokens != 2 {
		t.Fatalf("usage must accumulate across turns, got %+v", st)
	}
}

func messagesText(snap capture.Snapshot) string {
	msgs, _ := snap.Extra["messages"].([]NormalizedMessage)
	var parts []string
	for _, msg := range msgs {
		if msg.Text != "" {
			parts = append(parts, msg.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestPromptWhileBusyIsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, map[string]string{"FAKE_MODE": "hang"})

	inst, err := ctrl.Start(context.Background(), inst, "codex exec --skip-git-repo-check", "", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	inst, err = ctrl.SendPrompt(context.Background(), inst, "first")
	if err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	defer func() { _ = ctrl.Halt(context.Background(), inst, true, 0) }()

	_, err = ctrl.SendPrompt(context.Background(), inst, "second")
	if err == nil {
		t.Fatal("expected the second prompt to be rejected while a turn runs")
	}
	if code := apperr.Code(err); code != "execjson_instance_busy" {
		t.Fatalf("expected execjson_instance_busy, got %s", code)
	}
}

func TestSendPromptKillsSpawnedTurnWhenMetadataPersistenceFails(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, map[string]string{"FAKE_MODE": "hang"})

	inst, err := ctrl.Start(context.Background(), inst, "codex exec --skip-git-repo-check", "", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// saveProcessMeta uses os.WriteFile, so a directory at its destination is
	// a deterministic failure after the detached turn has already spawned.
	if err := os.Mkdir(filepath.Join(inst.TransportDir, processFileName), 0o700); err != nil {
		t.Fatalf("create blocking process metadata directory: %v", err)
	}
	if _, err := ctrl.SendPrompt(context.Background(), inst, "hello"); err == nil {
		t.Fatal("expected process metadata persistence to fail")
	}

	logBytes, err := os.ReadFile(filepath.Join(inst.TransportDir, commandLogName))
	if err != nil {
		t.Fatalf("read command log: %v", err)
	}
	var pid int
	for _, field := range strings.Fields(string(logBytes)) {
		if strings.HasPrefix(field, "pid=") {
			if _, err := fmt.Sscanf(field, "pid=%d", &pid); err != nil {
				t.Fatalf("parse spawned pid: %v", err)
			}
			break
		}
	}
	if pid <= 0 {
		t.Fatalf("expected spawned pid in command log, got %q", logBytes)
	}
	if processAlive(pid) {
		t.Fatalf("spawned turn pid %d survived failed state transaction", pid)
	}
	st, err := loadState(statePath(inst))
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if runningTurn(&st) >= 0 || st.Status != statusIdle {
		t.Fatalf("failed prompt must leave no running turn, got %+v", st)
	}
}

func TestHaltReturnsStateError(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocking path: %v", err)
	}
	ctrl := Controller{StateDir: dir}
	inst := instance.Instance{TransportDir: blocked}

	if err := ctrl.Halt(context.Background(), inst, true, 0); err == nil || apperr.Code(err) != "execjson_state_error" {
		t.Fatalf("expected execjson_state_error, got %v", err)
	}
}

// A turn that dies without turn.completed/turn.failed is a failed turn, but the
// instance itself stays usable: it is a thread id, not a process.
func TestCrashedTurnFailsButInstanceStaysIdle(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, map[string]string{"FAKE_MODE": "crash"})

	inst, err := ctrl.Start(context.Background(), inst, "codex exec --skip-git-repo-check", "", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	inst, err = ctrl.SendPrompt(context.Background(), inst, "boom")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	inst = waitIdle(t, ctrl, inst)

	if inst.Status != instance.StatusIdle {
		t.Fatalf("a crashed turn must not kill the instance, got %s", inst.Status)
	}
	st, err := loadState(statePath(inst))
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if st.Turns[0].State != TurnFailed {
		t.Fatalf("expected a failed turn, got %s", st.Turns[0].State)
	}
	if !strings.Contains(st.LastError, "exploded") {
		t.Fatalf("expected the stderr summary in last_error, got %q", st.LastError)
	}
	if st.Turns[0].ExitCode == nil || *st.Turns[0].ExitCode != 1 {
		t.Fatalf("expected the recorded exit code, got %v", st.Turns[0].ExitCode)
	}
	// The instance can still take another prompt.
	if _, err := ctrl.SendPrompt(context.Background(), inst, "retry"); err != nil {
		t.Fatalf("expected the instance to remain promptable, got %v", err)
	}
}

func TestTurnFailedIsRecordedWithoutFailingWait(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, map[string]string{"FAKE_MODE": "fail"})

	inst, err := ctrl.Start(context.Background(), inst, "codex exec --skip-git-repo-check", "", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	inst, err = ctrl.SendPrompt(context.Background(), inst, "bad")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if _, err := ctrl.Wait(context.Background(), inst, 10*time.Second); err != nil {
		t.Fatalf("wait must succeed even when the turn failed: %v", err)
	}
	snap, err := ctrl.Capture(context.Background(), inst, 0, capture.ScopeCurrent)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if snap.Extra["last_error"] != "model refused" {
		t.Fatalf("expected the failure surfaced via capture, got %v", snap.Extra["last_error"])
	}
	if snap.Content != "model refused" {
		t.Fatalf("expected the failure message as content, got %q", snap.Content)
	}
}

func TestInterruptCancelsRunningTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, map[string]string{"FAKE_MODE": "hang"})

	inst, err := ctrl.Start(context.Background(), inst, "codex exec --skip-git-repo-check", "", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	inst, err = ctrl.SendPrompt(context.Background(), inst, "long")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	pid := inst.ProcessID

	inst, err = ctrl.Interrupt(context.Background(), inst)
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if inst.Status != instance.StatusIdle {
		t.Fatalf("expected idle after interrupt, got %s", inst.Status)
	}
	if processAlive(pid) {
		t.Fatal("interrupt must end the turn process")
	}
	st, err := loadState(statePath(inst))
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if st.Turns[0].State != TurnCancelled {
		t.Fatalf("expected a cancelled turn, got %s", st.Turns[0].State)
	}
}

func TestHaltWithoutRunningTurnSucceeds(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, nil)

	inst, err := ctrl.Start(context.Background(), inst, "codex exec --skip-git-repo-check", "", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := ctrl.Halt(context.Background(), inst, false, time.Second); err != nil {
		t.Fatalf("halt: %v", err)
	}
	st, err := loadState(statePath(inst))
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if st.Status != statusExited {
		t.Fatalf("expected exited after halt, got %s", st.Status)
	}
}

func TestHaltKillsRunningTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, map[string]string{"FAKE_MODE": "hang"})

	inst, err := ctrl.Start(context.Background(), inst, "codex exec --skip-git-repo-check", "", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	inst, err = ctrl.SendPrompt(context.Background(), inst, "long")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	pid := inst.ProcessID
	if err := ctrl.Halt(context.Background(), inst, false, 2*time.Second); err != nil {
		t.Fatalf("halt: %v", err)
	}
	if processAlive(pid) {
		t.Fatal("halt must terminate the running turn's process group")
	}
}

func TestStartRejectsInvalidCommand(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, nil)

	_, err := ctrl.Start(context.Background(), inst, "codex exec --ephemeral", "", false)
	if err == nil {
		t.Fatal("expected --ephemeral to be rejected at summon")
	}
	if code := apperr.Code(err); code != "config_invalid" {
		t.Fatalf("expected config_invalid, got %s", code)
	}
}

func TestStartRejectsPositionalPromptInCommand(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, nil)

	_, err := ctrl.Start(context.Background(), inst, "codex exec hello", "", false)
	if err == nil {
		t.Fatal("expected positional prompt to be rejected")
	}
	if code := apperr.Code(err); code != "config_invalid" {
		t.Fatalf("expected config_invalid, got %s", code)
	}
}

func TestSystemPromptIsPrependedOnFirstTurnOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeCodex(t, dir)
	ctrl, inst := newTestInstance(t, dir, fake, nil)
	inst.SystemPrompt = "be terse"

	inst, err := ctrl.Start(context.Background(), inst, "codex exec --skip-git-repo-check", "be terse", false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	inst, err = ctrl.SendPrompt(context.Background(), inst, "one")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	inst = waitIdle(t, ctrl, inst)

	first, err := os.ReadFile(promptPath(inst.TransportDir, 0))
	if err != nil {
		t.Fatalf("read prompt 0: %v", err)
	}
	if !strings.Contains(string(first), "[SYSTEM]\nbe terse") {
		t.Fatalf("expected the system prompt on the first turn, got:\n%s", first)
	}

	inst, err = ctrl.SendPrompt(context.Background(), inst, "two")
	if err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	inst = waitIdle(t, ctrl, inst)

	second, err := os.ReadFile(promptPath(inst.TransportDir, 1))
	if err != nil {
		t.Fatalf("read prompt 1: %v", err)
	}
	if strings.Contains(string(second), "[SYSTEM]") {
		t.Fatalf("the system prompt must not repeat on later turns, got:\n%s", second)
	}
}
