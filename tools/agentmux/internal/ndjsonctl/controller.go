package ndjsonctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
)

const (
	HarnessType               = "claude-code-ndjson"
	inputFIFOName             = "input.fifo"
	outputJSONLName           = "output.jsonl"
	stderrLogName             = "stderr.log"
	stateFileName             = "state.json"
	processFileName           = "process.json"
	fifoWriteTimeout          = 5 * time.Second
	defaultPollInterval       = 250 * time.Millisecond
	interruptSilenceGrace     = 5 * time.Second
	sessionStateEventsEnvName = "CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS"
)

type Controller struct {
	StateDir string
	PollMS   int
}

type processMeta struct {
	Version     int       `json:"version"`
	PID         int       `json:"pid"`
	PGID        int       `json:"pgid"`
	StartedAt   time.Time `json:"started_at"`
	CWD         string    `json:"cwd"`
	Command     string    `json:"command"`
	Argv0       string    `json:"argv0"`
	Fingerprint string    `json:"fingerprint"`
}

// CanResume reports whether the instance carries a Claude session that has
// produced a persisted transcript, which is what `claude --resume` needs.
func (c Controller) CanResume(inst instance.Instance) bool {
	if inst.ClaudeSessionID == "" {
		return false
	}
	st, err := loadState(statePath(inst))
	if err != nil {
		return false
	}
	return st.ResumeAvailable
}

func (c Controller) Start(ctx context.Context, inst instance.Instance, command, systemPrompt string, resume bool) (instance.Instance, error) {
	claudeID := inst.ClaudeSessionID
	if claudeID == "" {
		var err error
		claudeID, err = newUUID()
		if err != nil {
			return instance.Instance{}, err
		}
	}
	dir := inst.TransportDir
	if dir == "" {
		dir = filepath.Join(c.StateDir, "ndjson", inst.SessionID)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return instance.Instance{}, apperr.Wrap("ndjson_process_error", err, "create transport dir")
	}
	for _, name := range []string{outputJSONLName, stderrLogName} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return instance.Instance{}, apperr.Wrap("ndjson_process_error", err, "create %s", name)
		}
		_ = f.Close()
	}
	fifoPath := filepath.Join(dir, inputFIFOName)
	if err := ensureFIFO(fifoPath); err != nil {
		return instance.Instance{}, err
	}

	finalCommand := buildClaudeCommand(command, systemPrompt, claudeID, resume)
	if err := writeRunScript(dir, finalCommand); err != nil {
		return instance.Instance{}, err
	}
	cmd := exec.Command("/bin/sh", filepath.Join(dir, "run.sh"))
	cmd.Dir = inst.CWD
	cmd.Env = envList(inst.Env, map[string]string{sessionStateEventsEnvName: "1"})
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return instance.Instance{}, apperr.Wrap("ndjson_process_error", err, "start claude ndjson process")
	}
	go func() { _ = cmd.Wait() }()
	pgid, _ := syscall.Getpgid(cmd.Process.Pid)
	if pgid <= 0 {
		pgid = cmd.Process.Pid
	}
	rollback := func() { _ = signalGroup(pgid, syscall.SIGKILL) }
	now := nowUTC()
	inst.ClaudeSessionID = claudeID
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
		Fingerprint: "agentmux:" + inst.SessionID + ":" + claudeID,
	}); err != nil {
		rollback()
		return instance.Instance{}, err
	}
	st := initialState(claudeID, now)
	if err := saveState(filepath.Join(dir, stateFileName), st); err != nil {
		rollback()
		return instance.Instance{}, err
	}
	if err := c.waitStarted(ctx, inst, 250*time.Millisecond); err != nil {
		rollback()
		return instance.Instance{}, err
	}
	if err := withStateLocked(filepath.Join(dir, stateFileName), func(st *State) error {
		st.Status = "idle"
		st.SessionIdle = false
		return nil
	}); err != nil {
		rollback()
		return instance.Instance{}, err
	}
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
		return apperr.New("process_not_running", "claude ndjson process exited during startup: "+tailFile(stderrPath(inst), 2048))
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
	var st State
	err := withStateLocked(statePath(inst), func(locked *State) error {
		events, next, err := readEvents(outputPath(inst), syncReadOffset(*locked), 0)
		if err != nil {
			return err
		}
		applyEvents(locked, events)
		locked.LastReadOffset = max(locked.LastReadOffset, next)
		degradeSilentInterrupt(locked, nowUTC())
		st = *locked
		return nil
	})
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
	uuid, err := newUUID()
	if err != nil {
		return inst, err
	}
	msg := UserMessage{Type: "user", UUID: uuid}
	msg.Message.Role = "user"
	msg.Message.Content = []TextContent{{Type: "text", Text: payload}}
	b, err := json.Marshal(msg)
	if err != nil {
		return inst, apperr.Wrap("ndjson_fifo_broken", err, "marshal prompt")
	}
	startOffset := fileSize(outputPath(inst))
	if err := writeFIFO(ctx, filepath.Join(inst.TransportDir, inputFIFOName), append(b, '\n'), fifoWriteTimeout); err != nil {
		return inst, err
	}
	now := nowUTC()
	if err := withStateLocked(statePath(inst), func(st *State) error {
		st.PendingPrompts = append(st.PendingPrompts, PendingPrompt{
			UUID:        uuid,
			SentAt:      now,
			StartOffset: startOffset,
			State:       PromptSent,
		})
		if st.ActivePromptUUID == "" {
			st.ActivePromptUUID = uuid
		}
		st.LastPromptAt = now
		st.InterruptedAt = time.Time{}
		if st.LastReadOffset > startOffset {
			st.LastReadOffset = startOffset
		}
		st.SessionIdle = false
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
	st, err := c.syncState(inst)
	if err != nil {
		return capture.Snapshot{}, err
	}
	var from int64
	if scope != capture.ScopeSession {
		from = promptStartOffset(st)
	}
	events, _, err := readEvents(outputPath(inst), from, 0)
	if err != nil {
		return capture.Snapshot{}, err
	}
	msgs, content, usage, cost := normalizeEvents(events)
	msgs = trimMessages(msgs, history)
	return capture.Snapshot{
		History:    history,
		Content:    content,
		CapturedAt: nowUTC(),
		Extra: map[string]any{
			"messages":          msgs,
			"usage":             usageMap(usage, cost),
			"claude_session_id": inst.ClaudeSessionID,
			"turns":             st.TotalTurns,
			"raw_event_count":   len(events),
		},
	}, nil
}

func (c Controller) Wait(ctx context.Context, inst instance.Instance, timeout time.Duration) (capture.Snapshot, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		st, err := c.syncState(inst)
		if err != nil {
			return capture.Snapshot{}, err
		}
		// Done means "no turn in flight and the instance is at rest". SessionIdle
		// only ever flips true after a prompt has run, so a freshly summoned
		// instance that was never prompted is also at rest and must not hang.
		if !hasUnfinishedPrompt(st.PendingPrompts) && (st.SessionIdle || len(st.PendingPrompts) == 0) {
			return capture.Snapshot{CapturedAt: nowUTC()}, nil
		}
		if !processAlive(inst.ProcessID) {
			return capture.Snapshot{}, apperr.New("process_not_running", "claude ndjson process is not running")
		}
		if time.Now().After(deadline) {
			return capture.Snapshot{}, apperr.New("capture_timeout", "capture timed out before claude became idle")
		}
		if err := sleepPoll(ctx, c.poll()); err != nil {
			return capture.Snapshot{}, err
		}
	}
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
		if err := signalGroup(inst.ProcessGroupID, sig); err != nil {
			return apperr.Wrap("ndjson_process_error", err, "signal claude ndjson process group")
		}
		if !immediately {
			deadline := time.Now().Add(timeout)
			for processAlive(inst.ProcessID) && time.Now().Before(deadline) {
				if err := sleepPoll(ctx, 100*time.Millisecond); err != nil {
					return err
				}
			}
			if processAlive(inst.ProcessID) {
				if err := signalGroup(inst.ProcessGroupID, syscall.SIGKILL); err != nil {
					return apperr.Wrap("ndjson_process_error", err, "kill claude ndjson process group")
				}
			}
		}
	}
	if err := os.Remove(filepath.Join(inst.TransportDir, inputFIFOName)); err != nil && !os.IsNotExist(err) {
		return apperr.Wrap("ndjson_process_error", err, "remove claude input fifo")
	}
	if err := withStateLocked(statePath(inst), func(st *State) error {
		st.Status = "exited"
		for i := range st.PendingPrompts {
			if st.PendingPrompts[i].State == PromptSent || st.PendingPrompts[i].State == PromptReplayed || st.PendingPrompts[i].State == PromptResult {
				st.PendingPrompts[i].State = PromptCancelled
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (c Controller) Interrupt(ctx context.Context, inst instance.Instance) (instance.Instance, error) {
	// With no turn in flight there is nothing to interrupt. Forcing the state to
	// busy here would wedge an idle instance: nothing running will ever emit the
	// session_state idle event needed to clear it. Treat it as a no-op instead.
	st, err := c.syncState(inst)
	if err != nil {
		return inst, err
	}
	if !hasUnfinishedPrompt(st.PendingPrompts) {
		return c.Reconcile(ctx, inst)
	}
	if inst.ProcessGroupID > 0 && processAlive(inst.ProcessID) {
		if err := signalGroup(inst.ProcessGroupID, syscall.SIGINT); err != nil {
			return inst, apperr.Wrap("ndjson_process_error", err, "interrupt claude ndjson process group")
		}
	}
	now := nowUTC()
	if err := withStateLocked(statePath(inst), func(st *State) error {
		for i := range st.PendingPrompts {
			if st.PendingPrompts[i].State == PromptSent || st.PendingPrompts[i].State == PromptReplayed || st.PendingPrompts[i].State == PromptResult {
				st.PendingPrompts[i].State = PromptCancelled
			}
		}
		st.ActivePromptUUID = ""
		st.Status = "busy"
		st.SessionIdle = false
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

// degradeSilentInterrupt prevents a cancelled prompt from leaving a live
// streaming process permanently busy when Claude emits no terminal idle event.
// Any observed protocol event restarts the grace period, so an agent that is
// still producing output is not declared idle prematurely.
func degradeSilentInterrupt(st *State, now time.Time) {
	if st.Status != "busy" || st.InterruptedAt.IsZero() || st.SessionIdle || hasUnfinishedPrompt(st.PendingPrompts) {
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
	st.SessionIdle = true
	st.InterruptedAt = time.Time{}
}

func (c Controller) Attach(inst instance.Instance) *exec.Cmd {
	return exec.Command("tail", "-f", outputPath(inst))
}

func (c Controller) syncState(inst instance.Instance) (State, error) {
	var out State
	err := withStateLocked(statePath(inst), func(st *State) error {
		events, next, err := readEvents(outputPath(inst), syncReadOffset(*st), 0)
		if err != nil {
			return err
		}
		applyEvents(st, events)
		st.LastReadOffset = max(st.LastReadOffset, next)
		out = *st
		return nil
	})
	return out, err
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
func statePath(inst instance.Instance) string { return filepath.Join(inst.TransportDir, stateFileName) }

func nowUTC() time.Time { return time.Now().UTC() }

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", apperr.Wrap("ndjson_process_error", err, "generate uuid")
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexed := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexed[0:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32]), nil
}
