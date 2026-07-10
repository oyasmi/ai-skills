package execjsonctl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
)

type TurnState string

const (
	TurnRunning   TurnState = "running"
	TurnCompleted TurnState = "completed"
	TurnFailed    TurnState = "failed"
	TurnCancelled TurnState = "cancelled"
)

const (
	statusIdle   = "idle"
	statusBusy   = "busy"
	statusExited = "exited"
)

type Turn struct {
	Index       int       `json:"index"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at,omitempty"`
	StartOffset int64     `json:"start_offset"`
	EndOffset   int64     `json:"end_offset,omitempty"`
	PID         int       `json:"pid,omitempty"`
	PGID        int       `json:"pgid,omitempty"`
	State       TurnState `json:"state"`
	ExitCode    *int      `json:"exit_code,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type State struct {
	Version         int       `json:"version"`
	ThreadID        string    `json:"thread_id,omitempty"`
	Status          string    `json:"status"`
	StartedAt       time.Time `json:"started_at"`
	LastReadOffset  int64     `json:"last_read_offset"`
	ResumeAvailable bool      `json:"resume_available"`
	Turns           []Turn    `json:"turns"`

	TotalTurns                 int   `json:"total_turns"`
	TotalInputTokens           int64 `json:"total_input_tokens"`
	TotalOutputTokens          int64 `json:"total_output_tokens"`
	TotalCachedInputTokens     int64 `json:"total_cached_input_tokens"`
	TotalReasoningOutputTokens int64 `json:"total_reasoning_output_tokens"`

	LastError string `json:"last_error,omitempty"`
}

func initialState(threadID string, now time.Time) State {
	return State{
		Version:         1,
		ThreadID:        threadID,
		Status:          statusIdle,
		StartedAt:       now,
		ResumeAvailable: threadID != "",
		Turns:           []Turn{},
	}
}

// runningTurn returns the index of the turn that was last spawned and has not
// been finalized, or -1. At most one turn can be running: SendPrompt refuses to
// spawn a second one.
func runningTurn(st *State) int {
	for i := range st.Turns {
		if st.Turns[i].State == TurnRunning {
			return i
		}
	}
	return -1
}

func lastTurn(st *State) int {
	if len(st.Turns) == 0 {
		return -1
	}
	return len(st.Turns) - 1
}

func withStateLocked(path string, fn func(*State) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return apperr.Wrap("execjson_state_error", err, "create state dir")
	}
	lockFile, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return apperr.Wrap("execjson_state_error", err, "open state lock")
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return apperr.Wrap("execjson_state_error", err, "lock state")
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()

	st, err := loadState(path)
	if err != nil {
		return err
	}
	if err := fn(&st); err != nil {
		return err
	}
	return saveState(path, st)
}

func loadState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}, apperr.Wrap("execjson_state_error", err, "read state")
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, apperr.Wrap("execjson_state_error", err, "parse state")
	}
	if st.Version == 0 {
		st.Version = 1
	}
	if st.Turns == nil {
		st.Turns = []Turn{}
	}
	return st, nil
}

func saveState(path string, st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return apperr.Wrap("execjson_state_error", err, "marshal state")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "state.json.*.tmp")
	if err != nil {
		return apperr.Wrap("execjson_state_error", err, "create state temp")
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return apperr.Wrap("execjson_state_error", err, "write state temp")
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return apperr.Wrap("execjson_state_error", err, "close state temp")
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return apperr.Wrap("execjson_state_error", err, "chmod state temp")
	}
	if err := os.Rename(tmpName, path); err != nil {
		return apperr.Wrap("execjson_state_error", err, "replace state")
	}
	return nil
}
