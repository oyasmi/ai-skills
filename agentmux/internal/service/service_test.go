package service

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/oyasmi/agentmux/internal/config"
	"github.com/oyasmi/agentmux/internal/instance"
	"github.com/oyasmi/agentmux/internal/tmuxctl"
)

type fakeTmux struct {
	sessions map[string]bool
}

func (f fakeTmux) HasSession(_ context.Context, sessionID string) bool {
	return f.sessions[sessionID]
}

func (f fakeTmux) NewSession(_ context.Context, sessionID, _ string, _ string, _ map[string]string) error {
	f.sessions[sessionID] = true
	return nil
}

func (f fakeTmux) KillSession(_ context.Context, sessionID string) error {
	delete(f.sessions, sessionID)
	return nil
}

func (f fakeTmux) CapturePane(context.Context, string, int) (string, error) {
	return "", nil
}

func (f fakeTmux) LoadBuffer(context.Context, string) error {
	return nil
}

func (f fakeTmux) PasteBuffer(context.Context, string) error {
	return nil
}

func (f fakeTmux) SendKeys(context.Context, string, ...string) error {
	return nil
}

func (f fakeTmux) Attach(string) *exec.Cmd {
	return nil
}

func (f fakeTmux) PaneInfo(context.Context, string) (tmuxctl.PaneInfo, error) {
	return tmuxctl.PaneInfo{}, nil
}

func TestListPrunesMissingSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, fakeTmux{
		sessions: map[string]bool{"live-session": true},
	})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"live": {
				Name:      "live",
				SessionID: "live-session",
				Status:    instance.StatusIdle,
				UpdatedAt: now,
			},
			"stale": {
				Name:      "stale",
				SessionID: "stale-session",
				Status:    instance.StatusIdle,
				UpdatedAt: now,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	items, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 live instance, got %d", len(items))
	}
	if items[0].Name != "live" {
		t.Fatalf("expected live instance to remain, got %q", items[0].Name)
	}

	saved, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if _, ok := saved.Get("stale"); ok {
		t.Fatalf("stale instance should have been removed from registry")
	}
}

func TestSummonIgnoresStaleInstancesForLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, registryPath := newTestService(t, fakeTmux{sessions: map[string]bool{}})
	now := time.Now().UTC()
	reg := instance.Registry{
		Instances: map[string]instance.Instance{
			"stale": {
				Name:      "stale",
				SessionID: "stale-session",
				Status:    instance.StatusIdle,
				UpdatedAt: now,
			},
		},
	}
	if err := instance.Save(registryPath, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	res, err := svc.Summon(ctx, SummonInput{TemplateName: "worker", Name: "fresh"})
	if err != nil {
		t.Fatalf("summon: %v", err)
	}
	if res.Instance.Name != "fresh" {
		t.Fatalf("expected fresh instance, got %q", res.Instance.Name)
	}

	saved, err := instance.Load(registryPath)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if len(saved.Instances) != 1 {
		t.Fatalf("expected only the fresh instance in registry, got %d", len(saved.Instances))
	}
	if _, ok := saved.Get("stale"); ok {
		t.Fatalf("stale instance should have been removed before summon")
	}
}

func newTestService(t *testing.T, tmux tmuxClient) (Service, string) {
	t.Helper()

	dir := t.TempDir()
	registryPath := filepath.Join(dir, "instances.json")
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Shell:        "/bin/bash -lc",
			CWD:          dir,
			Env:          map[string]string{},
			Capture:      config.CaptureDefaults{History: 120, StableMS: 1500, PollMS: 250},
			MaxInstances: 1,
		},
		Templates: map[string]config.Template{
			"worker": {
				Command: "echo test",
				Model:   "openai/gpt-5.4",
				CWD:     dir,
				Shell:   "/bin/bash -lc",
				Env:     map[string]string{},
			},
		},
	}

	return Service{
		Paths:  config.Paths{Registry: registryPath},
		Config: cfg,
		Tmux:   tmux,
	}, registryPath
}
