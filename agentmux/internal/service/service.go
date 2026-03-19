package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
	"github.com/oyasmi/agentmux/internal/capture"
	"github.com/oyasmi/agentmux/internal/config"
	"github.com/oyasmi/agentmux/internal/instance"
	"github.com/oyasmi/agentmux/internal/naming"
	"github.com/oyasmi/agentmux/internal/tmuxctl"
)

type Service struct {
	Paths  config.Paths
	Config config.Config
	Tmux   tmuxctl.Client
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
		Tmux:   tmuxctl.Client{Socket: config.DefaultSocketPath},
	}
}

func (s Service) TemplateList() []map[string]string {
	out := make([]map[string]string, 0, len(s.Config.Templates))
	for name, tpl := range s.Config.Templates {
		out = append(out, map[string]string{
			"name":        name,
			"model":       tpl.Model,
			"cwd":         tpl.CWD,
			"description": tpl.Description,
		})
	}
	return out
}

func (s Service) loadRegistry() (instance.Registry, error) {
	return instance.Load(s.Paths.Registry)
}

func (s Service) saveRegistry(reg instance.Registry) error {
	return instance.Save(s.Paths.Registry, reg)
}

func (s Service) List(ctx context.Context) ([]instance.Instance, error) {
	reg, err := s.loadRegistry()
	if err != nil {
		return nil, err
	}
	changed := false
	for name, inst := range reg.Instances {
		next := s.reconcile(ctx, inst)
		if next.Status != inst.Status || next.UpdatedAt != inst.UpdatedAt {
			reg.Instances[name] = next
			changed = true
		}
	}
	if changed {
		if err := s.saveRegistry(reg); err != nil {
			return nil, err
		}
	}
	return reg.Sorted(), nil
}

func (s Service) Inspect(ctx context.Context, name string) (instance.Instance, error) {
	reg, err := s.loadRegistry()
	if err != nil {
		return instance.Instance{}, err
	}
	inst, ok := reg.Get(name)
	if !ok {
		return instance.Instance{}, apperr.New("instance_not_found", fmt.Sprintf("instance %q not found", name))
	}
	inst = s.reconcile(ctx, inst)
	reg.Put(inst)
	if err := s.saveRegistry(reg); err != nil {
		return instance.Instance{}, err
	}
	return inst, nil
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
	reg, err := s.loadRegistry()
	if err != nil {
		return SummonResult{}, err
	}
	if inst, ok := reg.Get(name); ok {
		inst = s.reconcile(ctx, inst)
		if inst.Status == instance.StatusLost {
			delete(reg.Instances, name)
		} else {
			if prompt := strings.TrimSpace(resolved.Prompt); prompt != "" {
				if err := s.sendPrompt(ctx, &inst, prompt, false); err != nil {
					return SummonResult{}, err
				}
				reg.Put(inst)
				if err := s.saveRegistry(reg); err != nil {
					return SummonResult{}, err
				}
			}
			return SummonResult{Instance: inst, Reused: true}, nil
		}
	}
	if s.Config.Defaults.MaxInstances > 0 && len(reg.Instances) >= s.Config.Defaults.MaxInstances {
		return SummonResult{}, apperr.New("config_invalid", "max_instances exceeded")
	}
	sessionID := naming.GenerateSessionID()
	command := expandCommand(resolved.Command, resolved.Model, cwd, name, resolved.Name)
	now := time.Now()
	inst := instance.Instance{
		Name:            name,
		Template:        resolved.Name,
		SessionID:       sessionID,
		Model:           resolved.Model,
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
		return SummonResult{}, err
	}
	inst.Status = instance.StatusIdle
	reg.Put(inst)
	if err := s.saveRegistry(reg); err != nil {
		return SummonResult{}, err
	}
	if prompt := strings.TrimSpace(resolved.Prompt); prompt != "" {
		if err := s.sendPrompt(ctx, &inst, prompt, true); err != nil {
			return SummonResult{}, err
		}
		reg.Put(inst)
		if err := s.saveRegistry(reg); err != nil {
			return SummonResult{}, err
		}
	}
	return SummonResult{Instance: inst, Reused: false}, nil
}

func (s Service) Prompt(ctx context.Context, name string, text string, key string, enter bool) (instance.Instance, error) {
	reg, err := s.loadRegistry()
	if err != nil {
		return instance.Instance{}, err
	}
	inst, ok := reg.Get(name)
	if !ok {
		return instance.Instance{}, apperr.New("instance_not_found", fmt.Sprintf("instance %q not found", name))
	}
	inst = s.reconcile(ctx, inst)
	if inst.Status == instance.StatusLost || inst.Status == instance.StatusExited {
		return instance.Instance{}, apperr.New("process_not_running", fmt.Sprintf("instance %q is not running", name))
	}
	if strings.TrimSpace(text) == "" && strings.TrimSpace(key) == "" {
		return instance.Instance{}, apperr.New("invalid_arguments", "prompt requires --text or --key")
	}
	if strings.TrimSpace(text) != "" {
		if err := s.sendPrompt(ctx, &inst, text, !inst.FirstPromptSent); err != nil {
			return instance.Instance{}, err
		}
		if enter {
			if err := s.Tmux.SendKeys(ctx, target(inst.SessionID), "Enter"); err != nil {
				return instance.Instance{}, err
			}
		}
	}
	if key != "" {
		mapped, ok := validKey(key)
		if !ok {
			return instance.Instance{}, apperr.New("invalid_key", fmt.Sprintf("unsupported key %q", key))
		}
		if err := s.Tmux.SendKeys(ctx, target(inst.SessionID), mapped); err != nil {
			return instance.Instance{}, err
		}
	}
	inst.Status = instance.StatusBusy
	inst.UpdatedAt = time.Now()
	inst.LastActivityAt = inst.UpdatedAt
	reg.Put(inst)
	if err := s.saveRegistry(reg); err != nil {
		return instance.Instance{}, err
	}
	return inst, nil
}

func (s Service) Capture(ctx context.Context, name string, history, stableMS, timeoutMS int) (instance.Instance, capture.Snapshot, error) {
	inst, err := s.Inspect(ctx, name)
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
	snap, err := capture.WaitStable(ctx, s.Tmux, target(inst.SessionID), h, stableMS, timeoutMS, s.Config.Defaults.Capture.PollMS)
	if err != nil {
		return instance.Instance{}, capture.Snapshot{}, err
	}
	reg, err := s.loadRegistry()
	if err != nil {
		return instance.Instance{}, capture.Snapshot{}, err
	}
	if snap.Dead {
		inst.Status = instance.StatusExited
	} else if stableMS > 0 {
		inst.Status = instance.StatusIdle
	}
	inst.UpdatedAt = time.Now()
	reg.Put(inst)
	if err := s.saveRegistry(reg); err != nil {
		return instance.Instance{}, capture.Snapshot{}, err
	}
	return inst, snap, nil
}

func (s Service) Halt(ctx context.Context, name string) (instance.Instance, error) {
	reg, err := s.loadRegistry()
	if err != nil {
		return instance.Instance{}, err
	}
	inst, ok := reg.Get(name)
	if !ok {
		return instance.Instance{}, apperr.New("instance_not_found", fmt.Sprintf("instance %q not found", name))
	}
	if s.Tmux.HasSession(ctx, inst.SessionID) {
		if err := s.Tmux.KillSession(ctx, inst.SessionID); err != nil {
			return instance.Instance{}, err
		}
	}
	inst.Status = instance.StatusExited
	inst.UpdatedAt = time.Now()
	reg.Put(inst)
	if err := s.saveRegistry(reg); err != nil {
		return instance.Instance{}, err
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
	if info.Dead {
		inst.Status = instance.StatusExited
	} else if inst.Status != instance.StatusBusy {
		inst.Status = instance.StatusIdle
	}
	inst.UpdatedAt = time.Now()
	return inst
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
