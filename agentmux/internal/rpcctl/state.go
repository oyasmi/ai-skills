package rpcctl

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
	// PromptSent: written to the FIFO, awaiting pi's command response.
	PromptSent PromptState = "sent"
	// PromptAccepted: pi acknowledged the prompt (running now or queued).
	PromptAccepted PromptState = "accepted"
	// PromptDone: the agent run that carried this prompt has settled.
	PromptDone PromptState = "done"
	// PromptFailed: pi rejected the prompt before acceptance.
	PromptFailed PromptState = "failed"
	// PromptCancelled: an interrupt aborted the prompt's run.
	PromptCancelled PromptState = "cancelled"
)

// PendingPrompt tracks one prompt from FIFO write to run settlement. ID is the
// uuid we mint and echo into the RPC command so pi's response can be correlated.
type PendingPrompt struct {
	ID          string      `json:"id"`
	SentAt      time.Time   `json:"sent_at"`
	StartOffset int64       `json:"start_offset"`
	State       PromptState `json:"state"`
}

// UIRequest records an extension dialog that pi is blocking on. Handled flips
// true once the controller has written a cancellation response for it.
type UIRequest struct {
	ID      string `json:"id"`
	Method  string `json:"method"`
	Handled bool   `json:"handled"`
}

type State struct {
	Version     int    `json:"version"`
	PiSessionID string `json:"pi_session_id"`
	Status      string `json:"status"`

	StartedAt     time.Time `json:"started_at"`
	LastPromptAt  time.Time `json:"last_prompt_at,omitempty"`
	LastEventAt   time.Time `json:"last_event_at,omitempty"`
	InterruptedAt time.Time `json:"interrupted_at,omitempty"`

	LastReadOffset int64 `json:"last_read_offset"`
	// LastUsageOffset makes usage accounting idempotent when sync rewinds to a
	// pending prompt's start offset to recover an event/state persistence race.
	LastUsageOffset int64 `json:"last_usage_offset"`

	// AgentRunActive is true between agent_start and agent_settled: pi is
	// producing a turn. ResumeAvailable records that at least one run has settled
	// since start, which is what makes `pi --session-id` resumable.
	AgentRunActive  bool `json:"agent_run_active"`
	ResumeAvailable bool `json:"resume_available"`

	PendingPrompts []PendingPrompt `json:"pending_prompts"`
	UIRequests     []UIRequest     `json:"ui_requests,omitempty"`

	TotalTurns            int     `json:"total_turns"`
	TotalInputTokens      int64   `json:"total_input_tokens"`
	TotalOutputTokens     int64   `json:"total_output_tokens"`
	TotalCacheReadTokens  int64   `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens int64   `json:"total_cache_write_tokens"`
	TotalCostUSD          float64 `json:"total_cost_usd"`

	LastError string `json:"last_error,omitempty"`
}

func initialState(piSessionID string, now time.Time) State {
	return State{
		Version:        1,
		PiSessionID:    piSessionID,
		Status:         "starting",
		StartedAt:      now,
		PendingPrompts: []PendingPrompt{},
	}
}

func withStateLocked(path string, fn func(*State) error) error {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return apperr.Wrap("rpc_state_error", err, "create state dir")
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return apperr.Wrap("rpc_state_error", err, "open state lock")
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return apperr.Wrap("rpc_state_error", err, "lock state")
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
		return State{}, apperr.Wrap("rpc_state_error", err, "read state")
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, apperr.Wrap("rpc_state_error", err, "parse state")
	}
	if st.Version == 0 {
		st.Version = 1
	}
	if st.PendingPrompts == nil {
		st.PendingPrompts = []PendingPrompt{}
	}
	// Version-1 state files written before LastUsageOffset existed already
	// include usage for every event through LastReadOffset. Seed the new cursor
	// from that durable boundary to avoid a one-time double count on upgrade.
	if st.LastUsageOffset == 0 && st.LastReadOffset > 0 {
		st.LastUsageOffset = st.LastReadOffset
	}
	return st, nil
}

func saveState(path string, st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return apperr.Wrap("rpc_state_error", err, "marshal state")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "state.json.*.tmp")
	if err != nil {
		return apperr.Wrap("rpc_state_error", err, "create state temp")
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return apperr.Wrap("rpc_state_error", err, "write state temp")
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return apperr.Wrap("rpc_state_error", err, "close state temp")
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return apperr.Wrap("rpc_state_error", err, "chmod state temp")
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return apperr.Wrap("rpc_state_error", err, "replace state")
	}
	return nil
}

// hasUnfinishedPrompt reports whether any prompt is still awaiting its run to
// settle. This — not process liveness — is what "busy" means for pi-rpc.
func hasUnfinishedPrompt(prompts []PendingPrompt) bool {
	for _, p := range prompts {
		if p.State == PromptSent || p.State == PromptAccepted {
			return true
		}
	}
	return false
}

// promptStartOffset is the earliest output offset of any in-flight or most
// recent prompt, used to scope "current" captures to the latest turn.
func promptStartOffset(st State) int64 {
	var offset int64
	found := false
	for _, p := range st.PendingPrompts {
		if p.State == PromptSent || p.State == PromptAccepted {
			if !found || p.StartOffset < offset {
				offset = p.StartOffset
				found = true
			}
		}
	}
	if found {
		return offset
	}
	if n := len(st.PendingPrompts); n > 0 {
		return st.PendingPrompts[n-1].StartOffset
	}
	return 0
}

// syncReadOffset is where incremental parsing resumes: the last consumed offset,
// rewound to the start of any still-unfinished prompt so its events are (re)read.
func syncReadOffset(st State) int64 {
	offset := st.LastReadOffset
	for _, p := range st.PendingPrompts {
		if p.State == PromptSent || p.State == PromptAccepted {
			if p.StartOffset < offset {
				offset = p.StartOffset
			}
		}
	}
	if offset < 0 {
		return 0
	}
	return offset
}
