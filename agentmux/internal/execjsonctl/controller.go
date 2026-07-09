package execjsonctl

import (
	"context"
	"fmt"
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
	HarnessType     = "codex-cli-execjson"
	outputJSONLName = "output.jsonl"
	stderrLogName   = "stderr.log"
	stateFileName   = "state.json"
	processFileName = "process.json"
	commandLogName  = "command.log"

	defaultPollInterval = 250 * time.Millisecond
	interruptGrace      = 5 * time.Second
)

// Controller drives Codex CLI through `codex exec --json`.
//
// Unlike the claude-code-ndjson harness there is no long-lived agent process.
// An instance is a thread id plus a transport directory; each prompt spawns one
// short-lived `codex exec` (or `codex exec resume`) process that exits when the
// turn ends. Process death therefore means "turn finished", never "instance
// gone" -- see Reconcile.
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

func nowUTC() time.Time { return time.Now().UTC() }

// CanResume reports whether codex recorded a thread we can hand to
// `codex exec resume`. A thread only exists once the agent emitted
// thread.started, so a summoned-but-never-prompted instance cannot resume.
func (c Controller) CanResume(inst instance.Instance) bool {
	if inst.TransportDir == "" {
		return false
	}
	st, err := loadState(statePath(inst))
	if err != nil {
		return false
	}
	return st.ResumeAvailable && st.ThreadID != ""
}

// Start prepares the transport directory. It deliberately spawns nothing: a
// codex instance has no process until its first prompt.
func (c Controller) Start(ctx context.Context, inst instance.Instance, command, systemPrompt string, resume bool) (instance.Instance, error) {
	if err := validateCommand(command); err != nil {
		return instance.Instance{}, err
	}
	dir := inst.TransportDir
	if dir == "" {
		dir = filepath.Join(c.StateDir, "execjson", inst.SessionID)
	}
	if err := os.MkdirAll(filepath.Join(dir, "turns"), 0o755); err != nil {
		return instance.Instance{}, apperr.Wrap("execjson_process_error", err, "create transport dir")
	}
	for _, name := range []string{outputJSONLName, stderrLogName} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return instance.Instance{}, apperr.Wrap("execjson_process_error", err, "create %s", name)
		}
		_ = f.Close()
	}

	threadID := ""
	if resume {
		threadID = inst.ThreadID
	}
	if err := saveState(filepath.Join(dir, stateFileName), initialState(threadID, nowUTC())); err != nil {
		return instance.Instance{}, err
	}

	inst.TransportDir = dir
	inst.ThreadID = threadID
	inst.Command = command
	inst.ProcessID = 0
	inst.ProcessGroupID = 0
	inst.PaneTitle = ""
	inst.Status = instance.StatusIdle
	inst.UpdatedAt = nowUTC()
	return inst, nil
}

// SendPrompt writes the prompt to disk and spawns exactly one turn.
//
// Codex cannot accept input into a turn already in flight, and two concurrent
// `resume` processes would race on the same session file, so a busy instance
// rejects the prompt rather than queueing it.
func (c Controller) SendPrompt(ctx context.Context, inst instance.Instance, text string) (instance.Instance, error) {
	payload := strings.TrimSpace(text)
	if payload == "" {
		return inst, nil
	}
	if inst.TransportDir == "" {
		return inst, apperr.New("execjson_state_error", "instance has no transport dir")
	}
	if sp := strings.TrimSpace(inst.SystemPrompt); sp != "" && !inst.FirstPromptSent {
		payload = "[SYSTEM]\n" + sp + "\n\n[USER]\n" + payload
	}

	var pid, pgid int
	err := withStateLocked(statePath(inst), func(st *State) error {
		if i := runningTurn(st); i >= 0 {
			if processAlive(st.Turns[i].PID) {
				return apperr.New("execjson_instance_busy", fmt.Sprintf("instance %q is running a turn; wait for it before prompting again", inst.Name))
			}
			c.finalize(inst, st, i, false)
		}
		if st.Status == statusExited {
			return apperr.New("process_not_running", fmt.Sprintf("instance %q has been halted", inst.Name))
		}

		turn := len(st.Turns)
		if err := os.WriteFile(promptPath(inst.TransportDir, turn), []byte(payload+"\n"), 0o600); err != nil {
			return apperr.Wrap("execjson_process_error", err, "write prompt")
		}
		_ = os.Remove(exitCodePath(inst.TransportDir, turn))

		command := buildTurnCommand(inst.Command, st.ThreadID)
		script := runScriptPath(inst.TransportDir, turn)
		if err := writeRunScript(inst.TransportDir, script, command, turn); err != nil {
			return err
		}

		startOffset := fileSize(outputPath(inst))
		var err error
		pid, pgid, err = spawnTurn(script, inst.CWD, inst.Env)
		if err != nil {
			return err
		}
		now := nowUTC()
		appendCommandLog(filepath.Join(inst.TransportDir, commandLogName), fmt.Sprintf("turn=%d pid=%d %s", turn, pid, command))
		if err := saveProcessMeta(filepath.Join(inst.TransportDir, processFileName), processMeta{
			Version:     1,
			Turn:        turn,
			PID:         pid,
			PGID:        pgid,
			StartedAt:   now,
			CWD:         inst.CWD,
			Command:     command,
			Fingerprint: "agentmux:" + inst.SessionID + ":" + st.ThreadID,
		}); err != nil {
			return err
		}
		st.Turns = append(st.Turns, Turn{
			Index:       turn,
			StartedAt:   now,
			StartOffset: startOffset,
			PID:         pid,
			PGID:        pgid,
			State:       TurnRunning,
		})
		st.Status = statusBusy
		return nil
	})
	if err != nil {
		return inst, err
	}

	now := nowUTC()
	inst.ProcessID = pid
	inst.ProcessGroupID = pgid
	inst.FirstPromptSent = true
	inst.Status = instance.StatusBusy
	inst.LastActivityAt = now
	inst.UpdatedAt = now
	return inst, nil
}

// Reconcile refreshes instance status from disk.
//
// The load-bearing rule: a dead process does NOT mean a dead instance. Between
// turns there is no process at all, so status is derived from whether a turn is
// in flight, never from ProcessID liveness alone.
func (c Controller) Reconcile(ctx context.Context, inst instance.Instance) (instance.Instance, error) {
	if inst.TransportDir == "" {
		inst.Status = instance.StatusLost
		inst.UpdatedAt = nowUTC()
		return inst, nil
	}
	if _, err := os.Stat(statePath(inst)); err != nil {
		inst.Status = instance.StatusLost
		inst.UpdatedAt = nowUTC()
		return inst, nil
	}
	var st State
	if err := withStateLocked(statePath(inst), func(locked *State) error {
		c.sync(inst, locked)
		st = *locked
		return nil
	}); err != nil {
		return inst, err
	}
	return c.applyState(inst, st), nil
}

func (c Controller) applyState(inst instance.Instance, st State) instance.Instance {
	inst.ThreadID = st.ThreadID
	switch {
	case st.Status == statusExited:
		inst.Status = instance.StatusExited
		inst.ProcessID = 0
		inst.ProcessGroupID = 0
	case st.Status == statusBusy:
		inst.Status = instance.StatusBusy
		if i := runningTurn(&st); i >= 0 {
			inst.ProcessID = st.Turns[i].PID
			inst.ProcessGroupID = st.Turns[i].PGID
		}
	default:
		inst.Status = instance.StatusIdle
		inst.ProcessID = 0
		inst.ProcessGroupID = 0
	}
	inst.UpdatedAt = nowUTC()
	return inst
}

// sync finalizes a running turn whose process has exited, and refreshes the
// thread id. Must be called with the state lock held.
func (c Controller) sync(inst instance.Instance, st *State) {
	i := runningTurn(st)
	if i < 0 {
		if st.Status != statusExited {
			st.Status = statusIdle
		}
		return
	}
	if processAlive(st.Turns[i].PID) {
		st.Status = statusBusy
		return
	}
	c.finalize(inst, st, i, false)
}

// finalize resolves a turn whose process is gone. The three outcomes are
// distinguished by what reached output.jsonl, falling back to stderr when the
// process died before emitting any terminal event.
func (c Controller) finalize(inst instance.Instance, st *State, i int, cancelled bool) {
	t := &st.Turns[i]
	events, next, err := readEvents(outputPath(inst), t.StartOffset)
	if err != nil {
		st.LastError = err.Error()
	}
	outcome := scanTurn(events, t.StartOffset)

	if outcome.ThreadID != "" {
		if st.ThreadID == "" {
			st.ThreadID = outcome.ThreadID
		} else if st.ThreadID != outcome.ThreadID {
			st.LastError = fmt.Sprintf("codex reported thread %s but instance tracks %s", outcome.ThreadID, st.ThreadID)
		}
		st.ResumeAvailable = true
	}

	t.EndedAt = nowUTC()
	t.EndOffset = outcome.EndOffset
	t.ExitCode = readExitCode(exitCodePath(inst.TransportDir, t.Index))
	t.PID = 0
	t.PGID = 0

	switch {
	case outcome.Terminal == "completed":
		t.State = TurnCompleted
		st.TotalInputTokens += outcome.Usage.InputTokens
		st.TotalOutputTokens += outcome.Usage.OutputTokens
		st.TotalCachedInputTokens += outcome.Usage.CachedInputTokens
		st.TotalReasoningOutputTokens += outcome.Usage.ReasoningOutputTokens
	case outcome.Terminal == "failed":
		t.State = TurnFailed
		t.Error = outcome.Error
		st.LastError = outcome.Error
	case cancelled:
		t.State = TurnCancelled
		t.Error = "interrupted"
	default:
		t.State = TurnFailed
		t.Error = summarizeStderr(stderrPath(inst))
		st.LastError = t.Error
	}

	st.TotalTurns++
	st.LastReadOffset = maxInt64(st.LastReadOffset, next)
	if st.Status != statusExited {
		st.Status = statusIdle
	}
}

func (c Controller) Capture(ctx context.Context, inst instance.Instance, history int, scope capture.Scope) (capture.Snapshot, error) {
	st, err := c.load(inst)
	if err != nil {
		return capture.Snapshot{}, err
	}
	// Current scope reads the current (or most recent) turn. Session scope reads
	// from the beginning and lets history act as a message limit.
	var from int64
	if scope != capture.ScopeSession {
		if i := lastTurn(&st); i >= 0 {
			from = st.Turns[i].StartOffset
		}
	}
	events, _, err := readEvents(outputPath(inst), from)
	if err != nil {
		return capture.Snapshot{}, err
	}
	msgs, content, usage := normalizeEvents(events)
	msgs = trimMessages(msgs, history)

	turnState := ""
	if i := lastTurn(&st); i >= 0 {
		turnState = string(st.Turns[i].State)
	}
	return capture.Snapshot{
		History:    history,
		Content:    content,
		CapturedAt: nowUTC(),
		Extra: map[string]any{
			"messages":        msgs,
			"usage":           usageMap(usage),
			"thread_id":       st.ThreadID,
			"turns":           st.TotalTurns,
			"turn_state":      turnState,
			"last_error":      st.LastError,
			"raw_event_count": len(events),
		},
	}, nil
}

// Wait blocks until no turn is in flight. A turn that ends in turn.failed has
// still "completed" for wait's purposes; the failure surfaces through
// capture's last_error rather than through a wait error.
func (c Controller) Wait(ctx context.Context, inst instance.Instance, timeout time.Duration) (capture.Snapshot, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		next, err := c.Reconcile(ctx, inst)
		if err != nil {
			return capture.Snapshot{}, err
		}
		if next.Status != instance.StatusBusy {
			return capture.Snapshot{CapturedAt: nowUTC()}, nil
		}
		if time.Now().After(deadline) {
			return capture.Snapshot{}, apperr.New("capture_timeout", "timed out before the codex turn finished")
		}
		if err := sleepPoll(ctx, c.poll()); err != nil {
			return capture.Snapshot{}, err
		}
	}
}

// Interrupt cancels the running turn. codex exec has no in-band interrupt, so
// SIGINT ends the process and therefore the turn.
func (c Controller) Interrupt(ctx context.Context, inst instance.Instance) (instance.Instance, error) {
	var pid, pgid int
	if err := withStateLocked(statePath(inst), func(st *State) error {
		if i := runningTurn(st); i >= 0 {
			pid, pgid = st.Turns[i].PID, st.Turns[i].PGID
		}
		return nil
	}); err != nil {
		return inst, err
	}
	if pid == 0 || !processAlive(pid) {
		return c.Reconcile(ctx, inst)
	}
	signalGroup(pgid, syscall.SIGINT)
	waitForExit(ctx, pid, interruptGrace)
	if processAlive(pid) {
		signalGroup(pgid, syscall.SIGKILL)
		waitForExit(ctx, pid, interruptGrace)
	}

	var st State
	if err := withStateLocked(statePath(inst), func(locked *State) error {
		if i := runningTurn(locked); i >= 0 {
			c.finalize(inst, locked, i, true)
		}
		st = *locked
		return nil
	}); err != nil {
		return inst, err
	}
	return c.applyState(inst, st), nil
}

func (c Controller) Halt(ctx context.Context, inst instance.Instance, immediately bool, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	var pid, pgid int
	_ = withStateLocked(statePath(inst), func(st *State) error {
		if i := runningTurn(st); i >= 0 {
			pid, pgid = st.Turns[i].PID, st.Turns[i].PGID
		}
		return nil
	})
	if pid > 0 && processAlive(pid) {
		if immediately {
			signalGroup(pgid, syscall.SIGKILL)
		} else {
			signalGroup(pgid, syscall.SIGTERM)
			waitForExit(ctx, pid, timeout)
			if processAlive(pid) {
				signalGroup(pgid, syscall.SIGKILL)
			}
		}
		waitForExit(ctx, pid, interruptGrace)
	}
	_ = withStateLocked(statePath(inst), func(st *State) error {
		if i := runningTurn(st); i >= 0 {
			c.finalize(inst, st, i, true)
		}
		st.Status = statusExited
		return nil
	})
	return nil
}

func (c Controller) Attach(inst instance.Instance) *exec.Cmd {
	return exec.Command("tail", "-f", outputPath(inst))
}

func (c Controller) load(inst instance.Instance) (State, error) {
	var st State
	err := withStateLocked(statePath(inst), func(locked *State) error {
		c.sync(inst, locked)
		st = *locked
		return nil
	})
	return st, err
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
