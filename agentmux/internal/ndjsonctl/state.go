package ndjsonctl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
)

type PromptState string

const (
	PromptSent      PromptState = "sent"
	PromptReplayed  PromptState = "replayed"
	PromptResult    PromptState = "result"
	PromptIdle      PromptState = "idle"
	PromptCancelled PromptState = "cancelled"
	PromptFailed    PromptState = "failed"
)

type PendingPrompt struct {
	UUID           string      `json:"uuid"`
	SentAt         time.Time   `json:"sent_at"`
	StartOffset    int64       `json:"start_offset"`
	ReplayedOffset int64       `json:"replayed_offset,omitempty"`
	ResultOffset   int64       `json:"result_offset,omitempty"`
	State          PromptState `json:"state"`
}

type State struct {
	Version                 int             `json:"version"`
	ClaudeSessionID         string          `json:"claude_session_id"`
	Status                  string          `json:"status"`
	StartedAt               time.Time       `json:"started_at"`
	LastPromptAt            time.Time       `json:"last_prompt_at,omitempty"`
	LastResultAt            time.Time       `json:"last_result_at,omitempty"`
	LastEventAt             time.Time       `json:"last_event_at,omitempty"`
	InterruptedAt           time.Time       `json:"interrupted_at,omitempty"`
	LastReadOffset          int64           `json:"last_read_offset"`
	LastResultOffset        int64           `json:"last_result_offset"`
	ActivePromptUUID        string          `json:"active_prompt_uuid,omitempty"`
	LastCompletedPromptUUID string          `json:"last_completed_prompt_uuid,omitempty"`
	PendingPrompts          []PendingPrompt `json:"pending_prompts"`
	SessionIdle             bool            `json:"session_idle"`
	ResumeAvailable         bool            `json:"resume_available"`
	TotalTurns              int             `json:"total_turns"`
	TotalCostUSD            float64         `json:"total_cost_usd"`
	TotalInputTokens        int64           `json:"total_input_tokens"`
	TotalOutputTokens       int64           `json:"total_output_tokens"`
	LastError               string          `json:"last_error,omitempty"`
	PendingPermission       any             `json:"pending_permission,omitempty"`
}

func initialState(claudeSessionID string, now time.Time) State {
	return State{
		Version:         1,
		ClaudeSessionID: claudeSessionID,
		Status:          "starting",
		StartedAt:       now,
		SessionIdle:     false,
		PendingPrompts:  []PendingPrompt{},
	}
}

func withStateLocked(path string, fn func(*State) error) error {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return apperr.Wrap("ndjson_state_error", err, "create state dir")
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return apperr.Wrap("ndjson_state_error", err, "open state lock")
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return apperr.Wrap("ndjson_state_error", err, "lock state")
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
		if os.IsNotExist(err) {
			return State{Version: 1, PendingPrompts: []PendingPrompt{}}, nil
		}
		return State{}, apperr.Wrap("ndjson_state_error", err, "read state")
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, apperr.Wrap("ndjson_state_error", err, "parse state")
	}
	if st.Version == 0 {
		st.Version = 1
	}
	if st.PendingPrompts == nil {
		st.PendingPrompts = []PendingPrompt{}
	}
	return st, nil
}

func saveState(path string, st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return apperr.Wrap("ndjson_state_error", err, "marshal state")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "state.json.*.tmp")
	if err != nil {
		return apperr.Wrap("ndjson_state_error", err, "create state temp")
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return apperr.Wrap("ndjson_state_error", err, "write state temp")
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return apperr.Wrap("ndjson_state_error", err, "close state temp")
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return apperr.Wrap("ndjson_state_error", err, "chmod state temp")
	}
	if err := os.Rename(tmpName, path); err != nil {
		return apperr.Wrap("ndjson_state_error", err, "replace state")
	}
	return nil
}
