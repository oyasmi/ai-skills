package rpcctl

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
	"github.com/oyasmi/agentmux/internal/capture"
	"github.com/oyasmi/agentmux/internal/instance"
)

const (
	HarnessType     = "pi-rpc"
	inputFIFOName   = "input.fifo"
	outputJSONLName = "output.jsonl"
	stderrLogName   = "stderr.log"
	stateFileName   = "state.json"
	processFileName = "process.json"

	fifoWriteTimeout      = 5 * time.Second
	defaultPollInterval   = 250 * time.Millisecond
	interruptSilenceGrace = 5 * time.Second
)

// Controller drives pi's coding agent through `pi --mode rpc`.
//
// The process model matches claude-code-ndjson, not codex-cli-execjson: one
// long-lived pi process reads JSONL commands from a FIFO and streams JSONL
// events to output.jsonl for the instance's whole lifetime. Process death
// therefore means the instance is gone (see Reconcile). The implementation is
// deliberately standalone rather than shared with the other structured
// controllers, because pi's protocol (id-correlated responses, agent_settled
// as the at-rest signal, in-band abort) differs at every step.
type Controller struct {
	StateDir string
	PollMS   int
}

func (c Controller) poll() time.Duration {
	if c.PollMS > 0 {
		return time.Duration(c.PollMS) * time.Millisecond
	}
	return defaultPollInterval
}

func outputPath(inst instance.Instance) string {
	return filepath.Join(inst.TransportDir, outputJSONLName)
}
func stderrPath(inst instance.Instance) string {
	return filepath.Join(inst.TransportDir, stderrLogName)
}
func statePath(inst instance.Instance) string {
	return filepath.Join(inst.TransportDir, stateFileName)
}

// CanResume reports whether the instance carries a pi session that has settled at
// least one run and can therefore be reopened with `pi --session-id`.
func (c Controller) CanResume(inst instance.Instance) bool {
	if inst.PiSessionID == "" {
		return false
	}
	st, err := loadState(statePath(inst))
	if err != nil {
		return false
	}
	return st.ResumeAvailable
}

func (c Controller) Start(ctx context.Context, inst instance.Instance, command, systemPrompt string, resume bool) (instance.Instance, error) {
	piID := inst.PiSessionID
	if piID == "" {
		var err error
		piID, err = newUUID()
		if err != nil {
			return instance.Instance{}, err
		}
	}
	dir := inst.TransportDir
	if dir == "" {
		dir = filepath.Join(c.StateDir, "rpc", inst.SessionID)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return instance.Instance{}, apperr.Wrap("rpc_process_error", err, "create transport dir")
	}
	for _, name := range []string{outputJSONLName, stderrLogName} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return instance.Instance{}, apperr.Wrap("rpc_process_error", err, "create %s", name)
		}
		_ = f.Close()
	}
	fifoPath := filepath.Join(dir, inputFIFOName)
	if err := ensureFIFO(fifoPath); err != nil {
		return instance.Instance{}, err
	}

	finalCommand := buildPiCommand(command, systemPrompt, piID)
	if err := writeRunScript(dir, finalCommand); err != nil {
		return instance.Instance{}, err
	}
	cmd := exec.Command("/bin/sh", filepath.Join(dir, "run.sh"))
	cmd.Dir = inst.CWD
	cmd.Env = envList(inst.Env, nil)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return instance.Instance{}, apperr.Wrap("rpc_process_error", err, "start pi rpc process")
	}
	go func() { _ = cmd.Wait() }()
	pgid, _ := syscall.Getpgid(cmd.Process.Pid)
	if pgid <= 0 {
		pgid = cmd.Process.Pid
	}
	now := nowUTC()
	inst.PiSessionID = piID
	inst.TransportDir = dir
	inst.ProcessID = cmd.Process.Pid
	inst.ProcessGroupID = pgid
	inst.Command = finalCommand
	inst.Status = instance.StatusStarting
	inst.PaneTitle = ""
	if err := saveProcessMeta(filepath.Join(dir, processFileName), processMeta{
		Version:     1,
		PID:         cmd.Process.Pid,
		PGID:        pgid,
		StartedAt:   now,
		CWD:         inst.CWD,
		Command:     finalCommand,
		Argv0:       "/bin/sh",
		Fingerprint: "agentmux:" + inst.SessionID + ":" + piID,
	}); err != nil {
		return instance.Instance{}, err
	}
	if err := saveState(filepath.Join(dir, stateFileName), initialState(piID, now)); err != nil {
		return instance.Instance{}, err
	}
	if err := c.waitStarted(ctx, inst, 250*time.Millisecond); err != nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		return instance.Instance{}, err
	}
	_ = withStateLocked(filepath.Join(dir, stateFileName), func(st *State) error {
		st.Status = "idle"
		return nil
	})
	inst.Status = instance.StatusIdle
	inst.UpdatedAt = nowUTC()
	return inst, nil
}

func (c Controller) waitStarted(ctx context.Context, inst instance.Instance, delay time.Duration) error {
	if delay > 0 {
		if err := sleepPoll(ctx, delay); err != nil {
			return err
		}
	}
	if !processAlive(inst.ProcessID) {
		return apperr.New("process_not_running", "pi rpc process exited during startup: "+tailFile(stderrPath(inst), 2048))
	}
	return nil
}

func (c Controller) Reconcile(ctx context.Context, inst instance.Instance) (instance.Instance, error) {
	if inst.ProcessID <= 0 {
		inst.Status = instance.StatusLost
		inst.UpdatedAt = nowUTC()
		return inst, nil
	}
	if !processAlive(inst.ProcessID) {
		inst.Status = instance.StatusExited
		inst.UpdatedAt = nowUTC()
		return inst, nil
	}
	st, err := c.syncState(ctx, inst)
	if err != nil {
		return inst, err
	}
	inst.Status = instance.Status(st.Status)
	if inst.Status == "" || inst.Status == instance.StatusStarting {
		inst.Status = instance.StatusIdle
	}
	inst.UpdatedAt = nowUTC()
	return inst, nil
}

func (c Controller) SendPrompt(ctx context.Context, inst instance.Instance, text string) (instance.Instance, error) {
	payload := strings.TrimSpace(text)
	if payload == "" {
		return inst, nil
	}
	id, err := newUUID()
	if err != nil {
		return inst, err
	}
	cmd := promptCommand{
		ID:                id,
		Type:              "prompt",
		Message:           payload,
		StreamingBehavior: "followUp",
	}
	b, err := json.Marshal(cmd)
	if err != nil {
		return inst, apperr.Wrap("rpc_fifo_broken", err, "marshal prompt")
	}
	startOffset := fileSize(outputPath(inst))
	if err := writeFIFO(ctx, filepath.Join(inst.TransportDir, inputFIFOName), append(b, '\n'), fifoWriteTimeout); err != nil {
		return inst, err
	}
	now := nowUTC()
	if err := withStateLocked(statePath(inst), func(st *State) error {
		st.PendingPrompts = append(st.PendingPrompts, PendingPrompt{
			ID:          id,
			SentAt:      now,
			StartOffset: startOffset,
			State:       PromptSent,
		})
		st.LastPromptAt = now
		st.InterruptedAt = zeroTime
		if st.LastReadOffset > startOffset {
			st.LastReadOffset = startOffset
		}
		st.Status = "busy"
		return nil
	}); err != nil {
		return inst, err
	}
	inst.FirstPromptSent = true
	inst.Status = instance.StatusBusy
	inst.LastActivityAt = now
	inst.UpdatedAt = now
	return inst, nil
}

func (c Controller) Capture(ctx context.Context, inst instance.Instance, history int, scope capture.Scope) (capture.Snapshot, error) {
	st, err := c.syncState(ctx, inst)
	if err != nil {
		return capture.Snapshot{}, err
	}
	var from int64
	if scope != capture.ScopeSession {
		from = promptStartOffset(st)
	}
	events, _, err := readEvents(outputPath(inst), from)
	if err != nil {
		return capture.Snapshot{}, err
	}
	msgs, content, _ := normalizeEvents(events)
	msgs = trimMessages(msgs, history)
	return capture.Snapshot{
		History:    history,
		Content:    content,
		CapturedAt: nowUTC(),
		Extra: map[string]any{
			"messages":        msgs,
			"usage":           usageMap(st),
			"pi_session_id":   inst.PiSessionID,
			"turns":           st.TotalTurns,
			"last_error":      st.LastError,
			"raw_event_count": len(events),
		},
	}, nil
}

func (c Controller) Wait(ctx context.Context, inst instance.Instance, timeout time.Duration) (capture.Snapshot, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		st, err := c.syncState(ctx, inst)
		if err != nil {
			return capture.Snapshot{}, err
		}
		// At rest means no prompt is awaiting settlement and no run is active.
		// A freshly summoned instance that was never prompted is also at rest.
		if !hasUnfinishedPrompt(st.PendingPrompts) && !st.AgentRunActive {
			return capture.Snapshot{CapturedAt: nowUTC()}, nil
		}
		if !processAlive(inst.ProcessID) {
			return capture.Snapshot{}, apperr.New("process_not_running", "pi rpc process is not running")
		}
		if time.Now().After(deadline) {
			return capture.Snapshot{}, apperr.New("capture_timeout", "capture timed out before pi became idle")
		}
		if err := sleepPoll(ctx, c.poll()); err != nil {
			return capture.Snapshot{}, err
		}
	}
}

// Interrupt aborts the running turn with pi's in-band abort command, which
// settles the run cleanly. If the FIFO write fails it falls back to SIGINT on
// the process group. With nothing in flight it is a no-op, so an idle instance
// is never wedged into a busy state that no event will clear.
func (c Controller) Interrupt(ctx context.Context, inst instance.Instance) (instance.Instance, error) {
	st, err := c.syncState(ctx, inst)
	if err != nil {
		return inst, err
	}
	if !hasUnfinishedPrompt(st.PendingPrompts) && !st.AgentRunActive {
		return c.Reconcile(ctx, inst)
	}
	b, _ := json.Marshal(controlCommand{Type: "abort"})
	if werr := writeFIFO(ctx, filepath.Join(inst.TransportDir, inputFIFOName), append(b, '\n'), fifoWriteTimeout); werr != nil {
		if inst.ProcessGroupID > 0 && processAlive(inst.ProcessID) {
			_ = syscall.Kill(-inst.ProcessGroupID, syscall.SIGINT)
		}
	}
	now := nowUTC()
	if err := withStateLocked(statePath(inst), func(st *State) error {
		for i := range st.PendingPrompts {
			if st.PendingPrompts[i].State == PromptSent || st.PendingPrompts[i].State == PromptAccepted {
				st.PendingPrompts[i].State = PromptCancelled
			}
		}
		st.Status = "busy"
		st.InterruptedAt = now
		st.LastError = "interrupted"
		return nil
	}); err != nil {
		return inst, err
	}
	inst.Status = instance.StatusBusy
	inst.UpdatedAt = now
	return inst, nil
}

func (c Controller) Halt(ctx context.Context, inst instance.Instance, immediately bool, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if inst.ProcessGroupID > 0 && processAlive(inst.ProcessID) {
		sig := syscall.SIGTERM
		if immediately {
			sig = syscall.SIGKILL
		}
		_ = syscall.Kill(-inst.ProcessGroupID, sig)
		if !immediately {
			deadline := time.Now().Add(timeout)
			for processAlive(inst.ProcessID) && time.Now().Before(deadline) {
				if err := sleepPoll(ctx, 100*time.Millisecond); err != nil {
					return err
				}
			}
			if processAlive(inst.ProcessID) {
				_ = syscall.Kill(-inst.ProcessGroupID, syscall.SIGKILL)
			}
		}
	}
	_ = os.Remove(filepath.Join(inst.TransportDir, inputFIFOName))
	_ = withStateLocked(statePath(inst), func(st *State) error {
		st.Status = "exited"
		st.AgentRunActive = false
		for i := range st.PendingPrompts {
			if st.PendingPrompts[i].State == PromptSent || st.PendingPrompts[i].State == PromptAccepted {
				st.PendingPrompts[i].State = PromptCancelled
			}
		}
		return nil
	})
	return nil
}

func (c Controller) Attach(inst instance.Instance) *exec.Cmd {
	return exec.Command("tail", "-f", outputPath(inst))
}

// syncState reads new events under the state lock, then dismisses any extension
// dialog pi is blocking on. The FIFO writes for those dismissals happen outside
// the lock (a FIFO write can stall up to its timeout) and only get marked handled
// once they succeed, so a transient failure is simply retried on the next sync.
func (c Controller) syncState(ctx context.Context, inst instance.Instance) (State, error) {
	var out State
	err := withStateLocked(statePath(inst), func(st *State) error {
		events, next, err := readEvents(outputPath(inst), syncReadOffset(*st))
		if err != nil {
			return err
		}
		applyEvents(st, events)
		st.LastReadOffset = max(st.LastReadOffset, next)
		degradeSilentInterrupt(st, nowUTC())
		out = *st
		return nil
	})
	if err != nil {
		return State{}, err
	}
	if cancelled := c.cancelDialogs(ctx, inst, out); len(cancelled) > 0 {
		_ = withStateLocked(statePath(inst), func(st *State) error {
			markUIHandled(st, cancelled)
			return nil
		})
	}
	return out, nil
}

// cancelDialogs writes a cancellation for every unhandled extension dialog and
// returns the ids that were successfully dismissed.
func (c Controller) cancelDialogs(ctx context.Context, inst instance.Instance, st State) []string {
	ids := unhandledDialogIDs(st)
	if len(ids) == 0 {
		return nil
	}
	fifo := filepath.Join(inst.TransportDir, inputFIFOName)
	var done []string
	for _, id := range ids {
		b, _ := json.Marshal(uiCancel{Type: "extension_ui_response", ID: id, Cancelled: true})
		if err := writeFIFO(ctx, fifo, append(b, '\n'), fifoWriteTimeout); err != nil {
			break
		}
		done = append(done, id)
	}
	return done
}

// degradeSilentInterrupt is a safety net for the rare case where an abort does
// not produce the expected agent_settled: after a quiet grace period following an
// interrupt with nothing left in flight, force the instance back to idle so it
// does not stay busy forever.
func degradeSilentInterrupt(st *State, now time.Time) {
	if st.Status != "busy" || st.InterruptedAt.IsZero() || hasUnfinishedPrompt(st.PendingPrompts) {
		return
	}
	lastProgress := st.InterruptedAt
	if st.LastEventAt.After(lastProgress) {
		lastProgress = st.LastEventAt
	}
	if now.Sub(lastProgress) < interruptSilenceGrace {
		return
	}
	st.Status = "idle"
	st.AgentRunActive = false
	st.InterruptedAt = zeroTime
}
