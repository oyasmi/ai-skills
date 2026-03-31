package instance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"syscall"
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
	HarnessType     string            `json:"harness_type,omitempty"`
	SystemPrompt    string            `json:"system_prompt"`
	CWD             string            `json:"cwd"`
	Command         string            `json:"command"`
	Shell           string            `json:"shell"`
	Env             map[string]string `json:"env"`
	Status          Status            `json:"status"`
	PaneTitle       string            `json:"pane_title,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	LastActivityAt  time.Time         `json:"last_activity_at"`
	FirstPromptSent bool              `json:"first_prompt_sent"`
}

type Registry struct {
	Instances map[string]Instance `json:"instances"`
}

func Load(path string) (Registry, error) {
	return loadUnlocked(path)
}

func loadUnlocked(path string) (Registry, error) {
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
	return saveUnlocked(path, reg)
}

func saveUnlocked(path string, reg Registry) error {
	if reg.Instances == nil {
		reg.Instances = map[string]Instance{}
	}
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return apperr.Wrap("config_invalid", err, "marshal registry")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "instances.json.*.tmp")
	if err != nil {
		return apperr.Wrap("config_invalid", err, "create registry temp file")
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return apperr.Wrap("config_invalid", err, "write registry temp file")
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return apperr.Wrap("config_invalid", err, "close registry temp file")
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return apperr.Wrap("config_invalid", err, "chmod registry temp file")
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return apperr.Wrap("config_invalid", err, "replace registry file")
	}
	return nil
}

func WithLocked(path string, fn func(*Registry) error) error {
	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return apperr.Wrap("config_invalid", err, "open registry lock file")
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return apperr.Wrap("config_invalid", err, "lock registry file")
	}
	defer func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	}()

	reg, err := loadUnlocked(path)
	if err != nil {
		return err
	}
	if reg.Instances == nil {
		reg.Instances = map[string]Instance{}
	}
	if err := fn(&reg); err != nil {
		return err
	}
	if err := saveUnlocked(path, reg); err != nil {
		return err
	}
	return nil
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
