package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/oyasmi/agentmux/internal/apperr"
)

const (
	DefaultSocketPath = "/tmp/agentmux.sock"
	DefaultConfigYAML = `version: 1

defaults:
  tmux:
    socket: /tmp/agentmux.sock
    load_user_config: false
  status:
    busy_ttl_ms: 10000
  shell: /bin/bash -lc
  cwd: .
  env:
    TERM: xterm-256color
  capture:
    history: 120
    stable_ms: 1500
    poll_ms: 250
  max_instances: 8

templates:
  深度编码专家:
    description: 面向复杂编码、调试、重构和测试修复任务的通用专家
    command: codex --model $MODEL
    model: openai/gpt-5.4
    system_prompt: 你是深度编码专家，先建立代码上下文，再直接推进修改、验证和收尾。
    prompt: ""
    cwd: .

  文档专家:
    description: 面向需求梳理、设计说明、使用文档和交付说明的专家
    command: codex --model $MODEL
    model: openai/gpt-5.4
    system_prompt: 你负责生成结构稳定、边界清楚、可执行的技术文档。
    prompt: ""
    cwd: .

  工作项管理助手:
    description: 面向任务拆分、优先级整理、进度跟踪和状态汇总的助手
    command: claude --dangerously-skip-permissions --model $MODEL
    model: anthropic/claude-sonnet-4.5
    system_prompt: 你负责管理工作项，输出要短、准、可执行。
    prompt: ""
    cwd: .

  通用终端助手:
    description: 面向轻量命令执行和终端观察的通用模板
    command: codex --model $MODEL
    model: openai/gpt-5.4
    system_prompt: ""
    prompt: ""
    cwd: .
`
)

type Config struct {
	Version   int                 `yaml:"version"`
	Defaults  Defaults            `yaml:"defaults"`
	Templates map[string]Template `yaml:"templates"`
}

type Defaults struct {
	Shell        string            `yaml:"shell"`
	CWD          string            `yaml:"cwd"`
	Env          map[string]string `yaml:"env"`
	Tmux         TmuxDefaults      `yaml:"tmux"`
	Status       StatusDefaults    `yaml:"status"`
	Capture      CaptureDefaults   `yaml:"capture"`
	MaxInstances int               `yaml:"max_instances"`
}

type TmuxDefaults struct {
	Socket         string `yaml:"socket"`
	LoadUserConfig bool   `yaml:"load_user_config"`
}

type StatusDefaults struct {
	BusyTTLMS *int `yaml:"busy_ttl_ms"`
}

type CaptureDefaults struct {
	History  int `yaml:"history"`
	StableMS int `yaml:"stable_ms"`
	PollMS   int `yaml:"poll_ms"`
}

type Template struct {
	Description  string            `yaml:"description"`
	Command      string            `yaml:"command"`
	Model        string            `yaml:"model"`
	SystemPrompt string            `yaml:"system_prompt"`
	Prompt       string            `yaml:"prompt"`
	CWD          string            `yaml:"cwd"`
	Shell        string            `yaml:"shell"`
	Env          map[string]string `yaml:"env"`
}

type Paths struct {
	ConfigFile string
	StateDir   string
	Registry   string
}

func DiscoverPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, apperr.Wrap("config_invalid", err, "resolve home dir")
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		switch runtime.GOOS {
		case "darwin", "linux":
			configHome = filepath.Join(home, ".config")
		default:
			configHome, err = os.UserConfigDir()
			if err != nil {
				return Paths{}, apperr.Wrap("config_invalid", err, "resolve user config dir")
			}
		}
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		switch runtime.GOOS {
		case "darwin", "linux":
			stateHome = filepath.Join(home, ".local", "state")
		default:
			stateHome = filepath.Join(home, ".local", "state")
		}
	}
	stateDir := filepath.Join(stateHome, "agentmux")
	return Paths{
		ConfigFile: filepath.Join(configHome, "agentmux", "config.yaml"),
		StateDir:   stateDir,
		Registry:   filepath.Join(stateDir, "instances.json"),
	}, nil
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, apperr.Wrap("config_invalid", err, "read config file %s", path)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, apperr.Wrap("config_invalid", err, "parse config file %s", path)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.Defaults.Tmux.Socket == "" {
		c.Defaults.Tmux.Socket = DefaultSocketPath
	}
	if c.Defaults.Shell == "" {
		c.Defaults.Shell = "/bin/bash -lc"
	}
	if c.Defaults.Status.BusyTTLMS == nil {
		c.Defaults.Status.BusyTTLMS = intPtr(10000)
	}
	if c.Defaults.CWD == "" {
		c.Defaults.CWD = "."
	}
	if c.Defaults.Capture.History == 0 {
		c.Defaults.Capture.History = 120
	}
	if c.Defaults.Capture.StableMS == 0 {
		c.Defaults.Capture.StableMS = 1500
	}
	if c.Defaults.Capture.PollMS == 0 {
		c.Defaults.Capture.PollMS = 250
	}
	if c.Defaults.Env == nil {
		c.Defaults.Env = map[string]string{}
	}
	if _, ok := c.Defaults.Env["TERM"]; !ok {
		c.Defaults.Env["TERM"] = "xterm-256color"
	}
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return apperr.New("config_invalid", "config version must be 1")
	}
	if len(c.Templates) == 0 {
		return apperr.New("config_invalid", "templates must not be empty")
	}
	if c.Defaults.Capture.History < 0 || c.Defaults.Capture.StableMS < 0 || c.Defaults.Capture.PollMS < 0 {
		return apperr.New("config_invalid", "capture settings must be non-negative")
	}
	if c.Defaults.Status.BusyTTLMS != nil && *c.Defaults.Status.BusyTTLMS < 0 {
		return apperr.New("config_invalid", "status.busy_ttl_ms must be non-negative")
	}
	if strings.TrimSpace(c.Defaults.Tmux.Socket) == "" {
		return apperr.New("config_invalid", "tmux socket must not be empty")
	}
	if c.Defaults.MaxInstances < 0 {
		return apperr.New("config_invalid", "max_instances must be positive")
	}
	for name, tpl := range c.Templates {
		if strings.TrimSpace(name) == "" {
			return apperr.New("config_invalid", "template name must not be empty")
		}
		if strings.TrimSpace(tpl.Command) == "" {
			return apperr.New("config_invalid", fmt.Sprintf("template %q command must not be empty", name))
		}
	}
	return nil
}

func EnsureStateDir(paths Paths) error {
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile), 0o755); err != nil {
		return apperr.Wrap("config_invalid", err, "create config dir")
	}
	if err := os.MkdirAll(paths.StateDir, 0o755); err != nil {
		return apperr.Wrap("config_invalid", err, "create state dir")
	}
	return nil
}

func EnsureDefaultConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return apperr.Wrap("config_invalid", err, "stat config file %s", path)
	}
	if err := os.WriteFile(path, []byte(DefaultConfigYAML), 0o644); err != nil {
		return apperr.Wrap("config_invalid", err, "write default config file %s", path)
	}
	return nil
}

type ResolvedTemplate struct {
	Name         string
	Description  string
	Command      string
	Model        string
	SystemPrompt string
	Prompt       string
	CWD          string
	Shell        string
	Env          map[string]string
}

type Override struct {
	CWD          *string
	Model        *string
	Command      *string
	SystemPrompt *string
	Prompt       *string
}

func Resolve(cfg Config, templateName string, override Override) (ResolvedTemplate, error) {
	tpl, ok := cfg.Templates[templateName]
	if !ok {
		return ResolvedTemplate{}, apperr.New("template_not_found", fmt.Sprintf("template %q not found", templateName))
	}
	rt := ResolvedTemplate{
		Name:         templateName,
		Description:  tpl.Description,
		Command:      firstNonEmpty(tpl.Command),
		Model:        firstNonEmpty(tpl.Model),
		SystemPrompt: tpl.SystemPrompt,
		Prompt:       tpl.Prompt,
		CWD:          firstNonEmpty(tpl.CWD, cfg.Defaults.CWD),
		Shell:        firstNonEmpty(tpl.Shell, cfg.Defaults.Shell),
		Env:          map[string]string{},
	}
	for k, v := range cfg.Defaults.Env {
		rt.Env[k] = v
	}
	for k, v := range tpl.Env {
		rt.Env[k] = v
	}
	if override.CWD != nil {
		rt.CWD = *override.CWD
	}
	if override.Model != nil {
		rt.Model = *override.Model
	}
	if override.Command != nil {
		rt.Command = *override.Command
	}
	if override.SystemPrompt != nil {
		rt.SystemPrompt = *override.SystemPrompt
	}
	if override.Prompt != nil {
		rt.Prompt = *override.Prompt
	}
	if strings.TrimSpace(rt.Command) == "" {
		return ResolvedTemplate{}, apperr.New("config_invalid", fmt.Sprintf("template %q resolved command is empty", templateName))
	}
	return rt, nil
}

func ExpandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	return filepath.Abs(path)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func intPtr(v int) *int {
	return &v
}

func IsNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
