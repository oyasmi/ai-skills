package app

import (
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/output"
)

// instanceSummary is the stable list contract. Do not serialize
// instance.Instance directly: that registry model contains prompts, env, and
// transport internals that are useful to the service but inappropriate for a
// routine status query.
type instanceSummary struct {
	Name           string  `json:"name"`
	Template       string  `json:"template"`
	Status         string  `json:"status"`
	Model          string  `json:"model"`
	Effort         string  `json:"effort,omitempty"`
	HarnessType    string  `json:"harness_type,omitempty"`
	CWD            string  `json:"cwd"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	LastActivityAt string  `json:"last_activity_at"`
	EndedAt        *string `json:"ended_at,omitempty"`
	EndReason      string  `json:"end_reason,omitempty"`
	LastError      string  `json:"last_error,omitempty"`
}

// instanceDetail is the inspect contract. It keeps the operational metadata a
// human needs for diagnosis while deliberately excluding system_prompt and
// env, both of which can contain secrets or large private instructions.
type instanceDetail struct {
	Name            string  `json:"name"`
	Template        string  `json:"template"`
	SessionID       string  `json:"session_id"`
	Model           string  `json:"model"`
	Effort          string  `json:"effort,omitempty"`
	HarnessType     string  `json:"harness_type,omitempty"`
	CWD             string  `json:"cwd"`
	Command         string  `json:"command"`
	Shell           string  `json:"shell"`
	Status          string  `json:"status"`
	PaneTitle       string  `json:"pane_title,omitempty"`
	ClaudeSessionID string  `json:"claude_session_id,omitempty"`
	ThreadID        string  `json:"thread_id,omitempty"`
	PiSessionID     string  `json:"pi_session_id,omitempty"`
	TransportDir    string  `json:"transport_dir,omitempty"`
	ProcessID       int     `json:"process_id,omitempty"`
	ProcessGroupID  int     `json:"process_group_id,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	LastActivityAt  string  `json:"last_activity_at"`
	EndedAt         *string `json:"ended_at,omitempty"`
	EndReason       string  `json:"end_reason,omitempty"`
	LastError       string  `json:"last_error,omitempty"`
	BusyConfirmedAt *string `json:"busy_confirmed_at,omitempty"`
	FirstPromptSent bool    `json:"first_prompt_sent"`
	ReadCursor      string  `json:"read_cursor,omitempty"`
}

func summarizeInstance(inst instance.Instance) instanceSummary {
	return instanceSummary{
		Name:           inst.Name,
		Template:       inst.Template,
		Status:         string(inst.Status),
		Model:          inst.Model,
		Effort:         inst.Effort,
		HarnessType:    inst.HarnessType,
		CWD:            inst.CWD,
		CreatedAt:      output.LocalTime(inst.CreatedAt),
		UpdatedAt:      output.LocalTime(inst.UpdatedAt),
		LastActivityAt: output.LocalTime(inst.LastActivityAt),
		EndedAt:        output.OptionalLocalTime(inst.EndedAt),
		EndReason:      inst.EndReason,
		LastError:      inst.LastError,
	}
}

func detailInstance(inst instance.Instance) instanceDetail {
	return instanceDetail{
		Name:            inst.Name,
		Template:        inst.Template,
		SessionID:       inst.SessionID,
		Model:           inst.Model,
		Effort:          inst.Effort,
		HarnessType:     inst.HarnessType,
		CWD:             inst.CWD,
		Command:         inst.Command,
		Shell:           inst.Shell,
		Status:          string(inst.Status),
		PaneTitle:       inst.PaneTitle,
		ClaudeSessionID: inst.ClaudeSessionID,
		ThreadID:        inst.ThreadID,
		PiSessionID:     inst.PiSessionID,
		TransportDir:    inst.TransportDir,
		ProcessID:       inst.ProcessID,
		ProcessGroupID:  inst.ProcessGroupID,
		CreatedAt:       output.LocalTime(inst.CreatedAt),
		UpdatedAt:       output.LocalTime(inst.UpdatedAt),
		LastActivityAt:  output.LocalTime(inst.LastActivityAt),
		EndedAt:         output.OptionalLocalTime(inst.EndedAt),
		EndReason:       inst.EndReason,
		LastError:       inst.LastError,
		BusyConfirmedAt: output.OptionalLocalTime(inst.BusyConfirmedAt),
		FirstPromptSent: inst.FirstPromptSent,
		ReadCursor:      inst.ReadCursor,
	}
}
