package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Found {
		t.Fatal("expected Found=false for a missing file")
	}
	if !cfg.ClaudeEnabled || !cfg.CodexEnabled {
		t.Fatal("expected defaults to enable Claude and Codex")
	}
	if cfg.ZaiEnabled {
		t.Fatal("expected z.ai disabled by default")
	}
}

func TestLoadPartialFilePreservesDefaultsForMissingKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"zaiEnabled": true, "zaiToken": "tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Found {
		t.Fatal("expected Found=true")
	}
	if !cfg.ClaudeEnabled || !cfg.CodexEnabled {
		t.Fatal("expected untouched keys to keep their defaults")
	}
	if !cfg.ZaiEnabled || cfg.ZaiToken != "tok" {
		t.Fatal("expected zai fields to be read from file")
	}
}

func TestLoadInvalidJSONErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestCustomProviderDetectEnabledDefaultsTrue(t *testing.T) {
	var c CustomProvider
	if !c.DetectEnabled() {
		t.Fatal("expected autoDetect to default to true when unset")
	}
	off := false
	c.AutoDetect = &off
	if c.DetectEnabled() {
		t.Fatal("expected autoDetect to respect an explicit false")
	}
}
