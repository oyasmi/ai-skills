package instance

import (
	"encoding/json"
	"os"
	"sort"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
)

type Status string

const (
	StatusStarting Status = "starting"
	StatusIdle     Status = "idle"
	StatusBusy     Status = "busy"
	StatusExited   Status = "exited"
	StatusLost     Status = "lost"
)

type Instance struct {
	Name            string            `json:"name"`
	Template        string            `json:"template"`
	SessionID       string            `json:"session_id"`
	Model           string            `json:"model"`
	SystemPrompt    string            `json:"system_prompt"`
	CWD             string            `json:"cwd"`
	Command         string            `json:"command"`
	Shell           string            `json:"shell"`
	Env             map[string]string `json:"env"`
	Status          Status            `json:"status"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	LastActivityAt  time.Time         `json:"last_activity_at"`
	FirstPromptSent bool              `json:"first_prompt_sent"`
}

type Registry struct {
	Instances map[string]Instance `json:"instances"`
}

func Load(path string) (Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Registry{Instances: map[string]Instance{}}, nil
		}
		return Registry{}, apperr.Wrap("config_invalid", err, "read registry")
	}
	var r Registry
	if err := json.Unmarshal(b, &r); err != nil {
		return Registry{}, apperr.Wrap("config_invalid", err, "parse registry")
	}
	if r.Instances == nil {
		r.Instances = map[string]Instance{}
	}
	return r, nil
}

func Save(path string, reg Registry) error {
	if reg.Instances == nil {
		reg.Instances = map[string]Instance{}
	}
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return apperr.Wrap("config_invalid", err, "marshal registry")
	}
	return os.WriteFile(path, b, 0o644)
}

func (r Registry) Get(name string) (Instance, bool) {
	inst, ok := r.Instances[name]
	return inst, ok
}

func (r Registry) Put(inst Instance) {
	if r.Instances == nil {
		r.Instances = map[string]Instance{}
	}
	r.Instances[inst.Name] = inst
}

func (r Registry) Delete(name string) {
	if r.Instances == nil {
		return
	}
	delete(r.Instances, name)
}

func (r Registry) Sorted() []Instance {
	items := make([]Instance, 0, len(r.Instances))
	for _, inst := range r.Instances {
		items = append(items, inst)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items
}
