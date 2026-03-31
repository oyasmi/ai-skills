package service

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/oyasmi/agentmux/internal/apperr"
	"github.com/oyasmi/agentmux/internal/capture"
	"github.com/oyasmi/agentmux/internal/config"
	"github.com/oyasmi/agentmux/internal/instance"
	"github.com/oyasmi/agentmux/internal/naming"
	"github.com/oyasmi/agentmux/internal/tmuxctl"
)

const (
	haltSecondInterruptDelay = 500 * time.Millisecond
	haltPollInterval         = 100 * time.Millisecond
	claudeCodeHarnessType    = "claude-code"
)

type tmuxClient interface {
	HasSession(ctx context.Context, sessionID string) bool
	NewSession(ctx context.Context, sessionID, cwd, command string, env map[string]string) error
	KillSession(ctx context.Context, sessionID string) error
	CapturePane(ctx context.Context, target string, history int) (string, error)
	LoadBuffer(ctx context.Context, data string) error
	PasteBuffer(ctx context.Context, target string) error
	SendKeys(ctx context.Context, target string, keys ...string) error
	Attach(sessionID string) *exec.Cmd
	PaneInfo(ctx context.Context, target string) (tmuxctl.PaneInfo, error)
}

type Service struct {
	Paths  config.Paths
	Config config.Config
	Tmux   tmuxClient
}

type SummonInput struct {
	TemplateName string
	Name         string
	CWD          *string
	Model        *string
	Command      *string
	SystemPrompt *string
	Prompt       *string
}

type SummonResult struct {
	Instance instance.Instance
	Reused   bool
}

func New(paths config.Paths, cfg config.Config) Service {
	return Service{
		Paths:  paths,
		Config: cfg,
		Tmux: tmuxctl.Client{
			Socket:         cfg.Defaults.Tmux.Socket,
			LoadUserConfig: cfg.Defaults.Tmux.LoadUserConfig,
		},
	}
}

func (s Service) TemplateList() []map[string]string {
	out := make([]map[string]string, 0, len(s.Config.Templates))
	for name, tpl := range s.Config.Templates {
		out = append(out, map[string]string{
			"name":         name,
			"model":        tpl.Model,
			"cwd":          tpl.CWD,
			"description":  tpl.Description,
			"harness_type": firstNonEmpty(tpl.HarnessType, s.Config.Defaults.HarnessType),
		})
	}
	return out
}

func (s Service) withRegistry(ctx context.Context, fn func(*instance.Registry) error) error {
	return instance.WithLocked(s.Paths.Registry, func(reg *instance.Registry) error {
		s.reconcileRegistry(ctx, reg)
		return fn(reg)
	})
}

func (s Service) List(ctx context.Context) ([]instance.Instance, error) {
	var items []instance.Instance
	err := s.withRegistry(ctx, func(reg *instance.Registry) error {
		items = reg.Sorted()
		return nil
	})
	return items, err
}

func (s Service) Inspect(ctx context.Context, name string) (instance.Instance, error) {
	var out instance.Instance
	err := s.withRegistry(ctx, func(reg *instance.Registry) error {
		inst, err := s.requireActiveInstance(reg, name)
		if err != nil {
			return err
		}
		out = inst
		return nil
	})
	return out, err
}

func (s Service) Summon(ctx context.Context, in SummonInput) (SummonResult, error) {
	resolved, err := config.Resolve(s.Config, in.TemplateName, config.Override{
		CWD:          in.CWD,
		Model:        in.Model,
		Command:      in.Command,
		SystemPrompt: in.SystemPrompt,
		Prompt:       in.Prompt,
	})
	if err != nil {
		return SummonResult{}, err
	}
	cwd, err := config.ExpandPath(resolved.CWD)
	if err != nil {
		return SummonResult{}, apperr.Wrap("config_invalid", err, "resolve cwd")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = naming.GenerateName(resolved.Name, filepath.Base(cwd))
	}
	var res SummonResult
	err = s.withRegistry(ctx, func(reg *instance.Registry) error {
		if inst, ok := reg.Get(name); ok {
			if inst.Status == instance.StatusLost {
				reg.Delete(name)
			} else {
				if prompt := strings.TrimSpace(resolved.Prompt); prompt != "" {
					if err := s.sendPrompt(ctx, &inst, prompt, false); err != nil {
						return err
					}
					reg.Put(inst)
				}
				res = SummonResult{Instance: inst, Reused: true}
				return nil
			}
		}
		if s.Config.Defaults.MaxInstances > 0 && len(reg.Instances) >= s.Config.Defaults.MaxInstances {
			return apperr.New("config_invalid", "max_instances exceeded")
		}
		sessionID := naming.GenerateSessionID()
		command := expandCommand(resolved.Command, resolved.Model, cwd, name, resolved.Name)
		now := time.Now()
		inst := instance.Instance{
			Name:            name,
			Template:        resolved.Name,
			SessionID:       sessionID,
			Model:           resolved.Model,
			HarnessType:     resolved.HarnessType,
			SystemPrompt:    resolved.SystemPrompt,
			CWD:             cwd,
			Command:         command,
			Shell:           resolved.Shell,
			Env:             resolved.Env,
			Status:          instance.StatusStarting,
			CreatedAt:       now,
			UpdatedAt:       now,
			LastActivityAt:  now,
			FirstPromptSent: false,
		}
		if err := s.Tmux.NewSession(ctx, sessionID, cwd, shellCommand(inst.Shell, inst.Command), inst.Env); err != nil {
			return err
		}
		inst.Status = instance.StatusIdle
		reg.Put(inst)
		if prompt := strings.TrimSpace(resolved.Prompt); prompt != "" {
			if err := s.sendPrompt(ctx, &inst, prompt, true); err != nil {
				return err
			}
			reg.Put(inst)
		}
		res = SummonResult{Instance: inst, Reused: false}
		return nil
	})
	if err != nil {
		return SummonResult{}, err
	}
	return res, nil
}

func (s Service) Prompt(ctx context.Context, name string, text string, key string, enter bool) (instance.Instance, error) {
	if strings.TrimSpace(text) == "" && strings.TrimSpace(key) == "" {
		return instance.Instance{}, apperr.New("invalid_arguments", "prompt requires --text or --key")
	}
	var out instance.Instance
	err := s.withRegistry(ctx, func(reg *instance.Registry) error {
		inst, ok := reg.Get(name)
		if !ok {
			return apperr.New("instance_not_found", fmt.Sprintf("instance %q not found", name))
		}
		if inst.Status == instance.StatusLost || inst.Status == instance.StatusExited {
			reg.Delete(name)
			return apperr.New("process_not_running", fmt.Sprintf("instance %q is not running", name))
		}
		if strings.TrimSpace(text) != "" {
			if err := s.sendPrompt(ctx, &inst, text, !inst.FirstPromptSent); err != nil {
				return err
			}
			if enter {
				if err := s.Tmux.SendKeys(ctx, target(inst.SessionID), "Enter"); err != nil {
					return err
				}
			}
		}
		if key != "" {
			mapped, ok := validKey(key)
			if !ok {
				return apperr.New("invalid_key", fmt.Sprintf("unsupported key %q", key))
			}
			if err := s.Tmux.SendKeys(ctx, target(inst.SessionID), mapped); err != nil {
				return err
			}
		}
		inst.Status = instance.StatusBusy
		inst.UpdatedAt = time.Now()
		inst.LastActivityAt = inst.UpdatedAt
		reg.Put(inst)
		out = inst
		return nil
	})
	if err != nil {
		return instance.Instance{}, err
	}
	return out, nil
}

func (s Service) Capture(ctx context.Context, name string, history, stableMS, timeoutMS int) (instance.Instance, capture.Snapshot, error) {
	return s.captureLike(ctx, name, history, stableMS, timeoutMS, true)
}

func (s Service) Wait(ctx context.Context, name string, stableMS, timeoutMS int) (instance.Instance, capture.Snapshot, error) {
	inst, err := s.getInstanceForCapture(ctx, name)
	if err != nil {
		return instance.Instance{}, capture.Snapshot{}, err
	}
	if inst.HarnessType == claudeCodeHarnessType {
		return s.waitByPaneTitle(ctx, inst, timeoutMS)
	}
	return s.captureLike(ctx, name, -1, stableMS, timeoutMS, false)
}

func (s Service) captureLike(ctx context.Context, name string, history, stableMS, timeoutMS int, includeContent bool) (instance.Instance, capture.Snapshot, error) {
	inst, err := s.getInstanceForCapture(ctx, name)
	if err != nil {
		return instance.Instance{}, capture.Snapshot{}, err
	}
	if inst.Status == instance.StatusLost {
		return instance.Instance{}, capture.Snapshot{}, apperr.New("session_not_found", fmt.Sprintf("session for %q not found", name))
	}
	h := history
	if h < 0 {
		h = s.Config.Defaults.Capture.History
	}
	var titleIdle capture.TitleIdleFunc
	if inst.HarnessType == claudeCodeHarnessType {
		titleIdle = claudeCodeTitleIsIdle
	}
	snap, err := capture.WaitStable(ctx, s.Tmux, target(inst.SessionID), h, stableMS, timeoutMS, s.Config.Defaults.Capture.PollMS, titleIdle)
	if err != nil {
		return instance.Instance{}, capture.Snapshot{}, err
	}
	err = s.withRegistry(ctx, func(reg *instance.Registry) error {
		current, ok := reg.Get(name)
		if !ok {
			return apperr.New("instance_not_found", fmt.Sprintf("instance %q not found", name))
		}
		inst = current
		if snap.Dead {
			reg.Delete(name)
			inst.Status = instance.StatusExited
			inst.PaneTitle = snap.PaneTitle
			inst.UpdatedAt = time.Now()
			return nil
		}
		if stableMS > 0 {
			inst.Status = instance.StatusIdle
		}
		inst.PaneTitle = snap.PaneTitle
		inst.UpdatedAt = time.Now()
		reg.Put(inst)
		return nil
	})
	if err != nil {
		return instance.Instance{}, capture.Snapshot{}, err
	}
	if !includeContent {
		snap.Content = ""
		snap.Digest = ""
	}
	return inst, snap, nil
}

func (s Service) waitByPaneTitle(ctx context.Context, inst instance.Instance, timeoutMS int) (instance.Instance, capture.Snapshot, error) {
	if inst.Status == instance.StatusLost {
		return instance.Instance{}, capture.Snapshot{}, apperr.New("session_not_found", fmt.Sprintf("session for %q not found", inst.Name))
	}
	snap, err := capture.WaitUntilTitleIdle(ctx, s.Tmux, target(inst.SessionID), timeoutMS, s.Config.Defaults.Capture.PollMS, claudeCodeTitleIsIdle)
	if err != nil {
		return instance.Instance{}, capture.Snapshot{}, err
	}
	err = s.withRegistry(ctx, func(reg *instance.Registry) error {
		current, ok := reg.Get(inst.Name)
		if !ok {
			return apperr.New("instance_not_found", fmt.Sprintf("instance %q not found", inst.Name))
		}
		inst = current
		if snap.Dead {
			reg.Delete(inst.Name)
			inst.Status = instance.StatusExited
			inst.PaneTitle = snap.PaneTitle
			inst.UpdatedAt = time.Now()
			return nil
		}
		inst.Status = instance.StatusIdle
		inst.PaneTitle = snap.PaneTitle
		inst.UpdatedAt = time.Now()
		reg.Put(inst)
		return nil
	})
	if err != nil {
		return instance.Instance{}, capture.Snapshot{}, err
	}
	return inst, snap, nil
}

func (s Service) getInstanceForCapture(ctx context.Context, name string) (instance.Instance, error) {
	var out instance.Instance
	err := s.withRegistry(ctx, func(reg *instance.Registry) error {
		inst, err := s.requireActiveInstance(reg, name)
		if err != nil {
			return err
		}
		out = inst
		return nil
	})
	return out, err
}

func (s Service) Halt(ctx context.Context, name string) (instance.Instance, error) {
	return s.HaltWithOptions(ctx, name, false, 5*time.Second)
}

func (s Service) HaltWithOptions(ctx context.Context, name string, immediately bool, timeout time.Duration) (instance.Instance, error) {
	var out instance.Instance
	err := s.withRegistry(ctx, func(reg *instance.Registry) error {
		inst, ok := reg.Get(name)
		if !ok {
			return apperr.New("instance_not_found", fmt.Sprintf("instance %q not found", name))
		}
		if s.Tmux.HasSession(ctx, inst.SessionID) {
			if immediately {
				if err := s.Tmux.KillSession(ctx, inst.SessionID); err != nil {
					return err
				}
			} else {
				if err := s.haltGracefully(ctx, inst.SessionID, timeout); err != nil {
					return err
				}
			}
		}
		inst.Status = instance.StatusExited
		inst.UpdatedAt = time.Now()
		reg.Delete(name)
		out = inst
		return nil
	})
	if err != nil {
		return instance.Instance{}, err
	}
	return out, nil
}

func (s Service) haltGracefully(ctx context.Context, sessionID string, timeout time.Duration) error {
	if err := s.Tmux.SendKeys(ctx, target(sessionID), "C-c"); err != nil {
		return err
	}
	if !s.Tmux.HasSession(ctx, sessionID) {
		return nil
	}

	deadline := time.Now().Add(timeout)
	secondInterruptAt := time.Now().Add(haltSecondInterruptDelay)
	secondSent := false

	for {
		if !s.Tmux.HasSession(ctx, sessionID) {
			return nil
		}
		now := time.Now()
		if !secondSent && !now.Before(secondInterruptAt) {
			if err := s.Tmux.SendKeys(ctx, target(sessionID), "C-c"); err != nil {
				return err
			}
			secondSent = true
			if !s.Tmux.HasSession(ctx, sessionID) {
				return nil
			}
		}
		if !now.Before(deadline) {
			if !secondSent {
				if err := s.Tmux.SendKeys(ctx, target(sessionID), "C-c"); err != nil {
					return err
				}
				secondSent = true
				if !s.Tmux.HasSession(ctx, sessionID) {
					return nil
				}
			}
			break
		}

		wait := haltPollInterval
		if remaining := time.Until(deadline); remaining < wait {
			wait = remaining
		}
		if !secondSent {
			if untilSecond := time.Until(secondInterruptAt); untilSecond > 0 && untilSecond < wait {
				wait = untilSecond
			}
		}
		if wait <= 0 {
			continue
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	if s.Tmux.HasSession(ctx, sessionID) {
		return s.Tmux.KillSession(ctx, sessionID)
	}
	return nil
}

func (s Service) reconcileRegistry(ctx context.Context, reg *instance.Registry) {
	for name, inst := range reg.Instances {
		next := s.reconcile(ctx, inst)
		if next.Status == instance.StatusLost || next.Status == instance.StatusExited {
			reg.Delete(name)
			continue
		}
		reg.Put(next)
	}
}

func (s Service) requireActiveInstance(reg *instance.Registry, name string) (instance.Instance, error) {
	inst, ok := reg.Get(name)
	if !ok {
		return instance.Instance{}, apperr.New("instance_not_found", fmt.Sprintf("instance %q not found", name))
	}
	if inst.Status == instance.StatusLost || inst.Status == instance.StatusExited {
		reg.Delete(name)
		return instance.Instance{}, apperr.New("process_not_running", fmt.Sprintf("instance %q is not running", name))
	}
	return inst, nil
}

func (s Service) sendPrompt(ctx context.Context, inst *instance.Instance, text string, allowSystem bool) error {
	payload := strings.TrimSpace(text)
	if payload == "" {
		return nil
	}
	systemPrompt := strings.TrimSpace(inst.SystemPrompt)
	if allowSystem && !inst.FirstPromptSent && systemPrompt != "" {
		payload = "[SYSTEM]\n" + systemPrompt + "\n\n[USER]\n" + payload
	}
	if err := s.Tmux.LoadBuffer(ctx, payload); err != nil {
		return err
	}
	if err := s.Tmux.PasteBuffer(ctx, target(inst.SessionID)); err != nil {
		return err
	}
	if err := s.Tmux.SendKeys(ctx, target(inst.SessionID), "Enter"); err != nil {
		return err
	}
	inst.FirstPromptSent = true
	inst.Status = instance.StatusBusy
	inst.UpdatedAt = time.Now()
	inst.LastActivityAt = inst.UpdatedAt
	return nil
}

func (s Service) reconcile(ctx context.Context, inst instance.Instance) instance.Instance {
	if !s.Tmux.HasSession(ctx, inst.SessionID) {
		if inst.Status != instance.StatusExited {
			inst.Status = instance.StatusLost
		}
		inst.UpdatedAt = time.Now()
		return inst
	}
	info, err := s.Tmux.PaneInfo(ctx, target(inst.SessionID))
	if err != nil {
		return inst
	}
	inst.PaneTitle = info.PaneTitle
	if info.Dead {
		inst.Status = instance.StatusExited
	} else if inst.Status == instance.StatusBusy {
		if inst.HarnessType == claudeCodeHarnessType && claudeCodeTitleIsIdle(info.PaneTitle) {
			inst.Status = instance.StatusIdle
		} else if s.busyExpired(inst) {
			inst.Status = instance.StatusIdle
		}
	} else if inst.Status != instance.StatusBusy {
		inst.Status = instance.StatusIdle
	}
	inst.UpdatedAt = time.Now()
	return inst
}

func claudeCodeTitleIsIdle(paneTitle string) bool {
	switch claudeCodeTitleState(paneTitle) {
	case "idle":
		return true
	default:
		return false
	}
}

func claudeCodeTitleState(paneTitle string) string {
	trimmed := strings.TrimSpace(paneTitle)
	if trimmed == "" {
		return "unknown"
	}
	significant := firstClaudeCodeMarkerRunes(trimmed, 4)
	if len(significant) == 0 {
		return "unknown"
	}
	for _, r := range significant {
		switch {
		case r == '\u2733':
			return "idle"
		case r >= '\u2800' && r <= '\u28ff':
			return "busy"
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			return "unknown"
		}
	}
	return "unknown"
}

func firstClaudeCodeMarkerRunes(s string, limit int) []rune {
	if limit <= 0 {
		return nil
	}
	var out []rune
	for _, r := range []rune(s) {
		if unicode.IsSpace(r) {
			continue
		}
		switch r {
		case '[', ']', '(', ')', '{', '}', '<', '>', ':', '-', '|':
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (s Service) busyExpired(inst instance.Instance) bool {
	if inst.Status != instance.StatusBusy {
		return false
	}
	if s.Config.Defaults.Status.BusyTTLMS == nil {
		return false
	}
	if *s.Config.Defaults.Status.BusyTTLMS == 0 {
		return false
	}
	last := inst.LastActivityAt
	if last.IsZero() {
		last = inst.UpdatedAt
	}
	return time.Since(last) >= time.Duration(*s.Config.Defaults.Status.BusyTTLMS)*time.Millisecond
}

func target(sessionID string) string {
	return sessionID + ":0.0"
}

func shellCommand(shell, command string) string {
	if shell == "" {
		shell = "/bin/bash -lc"
	}
	return shell + " " + quote(command)
}

func quote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'"
}

func expandCommand(command, model, cwd, instanceName, templateName string) string {
	r := strings.NewReplacer(
		"$MODEL", model,
		"$CWD", cwd,
		"$INSTANCE", instanceName,
		"$TEMPLATE", templateName,
	)
	return r.Replace(command)
}

func validKey(key string) (string, bool) {
	switch key {
	case "Enter":
		return "Enter", true
	case "C-c":
		return "C-c", true
	case "Escape":
		return "Escape", true
	case "Up":
		return "Up", true
	case "Down":
		return "Down", true
	case "Tab":
		return "Tab", true
	default:
		return "", false
	}
}
