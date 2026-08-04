package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/harnessarg"
)

const (
	DefaultSocketPath = "/tmp/agentmux.sock"
	DefaultConfigYAML = `version: 1

defaults:
  tmux:
    socket: /tmp/agentmux.sock
    load_user_config: false
  status:
    busy_ttl_ms: 30000
    prompt_ack_ms: 5000
    tombstone_ttl_ms: 86400000
  shell: /bin/bash -lc
  cwd: .
  env:
    TERM: xterm-256color
  capture:
    history: 120
    stable_ms: 1500
    poll_ms: 250
  max_instances: 8

# 模板是「角色」，不是 harness 清单。
#
# description 要写清楚：什么场景用它、用它时正确的姿势是什么。编排方按角色选人，
# 不该在选人这一步纠缠底层 CLI。
#
# model + effort 一起构成强度档位；harness_type 和 command 是技术细节：
#   effort 取值 off / minimal / low / medium / high / xhigh / max，
#   agentmux 按 harness 翻译成 claude --effort、pi --thinking、
#   codex -c model_reasoning_effort=，并在目标 CLI 词表更窄时向下夹取。
#   命令里已经自己写了 --model/--effort/$MODEL/$EFFORT 时，agentmux 不再注入。
templates:
  planner:
    description: |
      规划者。用于复杂、模糊或高风险的改动：先要方案，再动手。
      正确用法：只授权调查和规划，要求返回实现方案、影响范围、关键假设、风险
      和验证方法；方案审查通过后再交给 builder 实现。read-only sandbox 是硬约束，
      它改不了工作树，可以和 builder 共享同一个 cwd。
    command: codex exec --sandbox read-only --skip-git-repo-check
    model: ""
    effort: max
    harness_type: codex-cli-execjson
    system_prompt: ""
    prompt: ""
    cwd: .

  builder:
    description: |
      实现者。用于边界清楚、可验证的常规编码任务：同一轮要求实现、测试和自查。
      正确用法：给出目标、相关路径、必须保持的行为和可判真假的完成标准；
      它会写文件，因此并行时必须每个实例一个独立 worktree 和不同的 --cwd。
    command: claude --dangerously-skip-permissions
    model: sonnet
    effort: medium
    harness_type: claude-code-ndjson
    system_prompt: ""
    prompt: ""
    cwd: .

  builder-hard:
    description: |
      硬骨头实现者。用于 builder 已经连续纠偏失败、或改动跨模块且约束复杂的任务。
      正确用法：与 builder 相同，但要附上前一次失败的具体证据和已确认的约束，
      不要让它从零重新调查。比 builder 慢且贵，不要作为默认入口。
    command: claude --dangerously-skip-permissions
    model: opus
    effort: xhigh
    harness_type: claude-code-ndjson
    system_prompt: ""
    prompt: ""
    cwd: .

  reviewer:
    description: |
      独立审查者。用于高风险改动或长时间自主执行之后的验收，刻意与 builder 用
      不同的模型家族，避免同源盲区。
      正确用法：给出原始完成标准和变更范围，要求只报告影响正确性、明确需求或
      安全性的缺口，不追逐风格偏好。read-only sandbox 保证它不会顺手"修好"问题。
    command: codex exec --sandbox read-only --skip-git-repo-check
    model: ""
    effort: xhigh
    harness_type: codex-cli-execjson
    system_prompt: ""
    prompt: ""
    cwd: .

  scout:
    description: |
      侦察兵。用于便宜快速的事实查询：某个符号定义在哪、某个配置当前是什么值、
      某个命令的实际输出。
      正确用法：一次只问一个能被单一事实回答的问题，不要交给它需要判断的任务；
      它的价值是省 token 和时间，一旦需要推理就换 planner。
    command: pi
    model: ""
    effort: low
    harness_type: pi-rpc
    system_prompt: ""
    prompt: ""
    cwd: .

  documenter:
    description: |
      文档作者。用于需求梳理、设计说明、使用文档和交付说明。
      正确用法：指明读者、文档要回答的问题和落地路径；要求结构稳定、边界清楚、
      示例可直接执行，而不是复述代码。
    command: claude --dangerously-skip-permissions
    model: sonnet
    effort: medium
    harness_type: claude-code-ndjson
    system_prompt: 你负责生成结构稳定、边界清楚、可执行的技术文档。
    prompt: ""
    cwd: .

  # 唯一一个按 harness 而不是按角色命名的模板：人要旁观、接管或调试终端时用它，
  # 只有这种场景值得付 TUI 的代价（启动提示、按键、状态推断都更脆弱）。
  claude-code-tui:
    description: |
      可 attach 的终端实例。仅用于人工旁观、接管或调试；不要用于批量委派。
      正确用法：先 summon，再 capture 确认启动页/升级提示已经过去，最后才 prompt。
    command: claude --dangerously-skip-permissions
    model: sonnet
    effort: medium
    harness_type: claude-code
    system_prompt: ""
    prompt: ""
    cwd: .
`
)

// RecommendedSocketPath returns a user-isolated tmux socket path.
// It prefers $XDG_RUNTIME_DIR/agentmux.sock and falls back to
// /tmp/agentmux-$UID.sock when XDG_RUNTIME_DIR is not set.
func RecommendedSocketPath() string {
	if rd := os.Getenv("XDG_RUNTIME_DIR"); rd != "" {
		return filepath.Join(rd, "agentmux.sock")
	}
	return fmt.Sprintf("/tmp/agentmux-%d.sock", os.Getuid())
}

type Config struct {
	Version   int                 `yaml:"version"`
	Defaults  Defaults            `yaml:"defaults"`
	Templates map[string]Template `yaml:"templates"`
}

type Defaults struct {
	Shell        string            `yaml:"shell"`
	CWD          string            `yaml:"cwd"`
	HarnessType  string            `yaml:"harness_type"`
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
	// PromptAckMS bounds how long a prompt waits for a TUI harness to visibly
	// start working before giving up on observing the transition. 0 disables
	// the confirmation.
	PromptAckMS *int `yaml:"prompt_ack_ms"`
	// TombstoneTTLMS is how long a stopped instance stays queryable before it
	// is swept from the registry. 0 keeps tombstones forever.
	TombstoneTTLMS *int `yaml:"tombstone_ttl_ms"`
}

type CaptureDefaults struct {
	History  int `yaml:"history"`
	StableMS int `yaml:"stable_ms"`
	PollMS   int `yaml:"poll_ms"`
}

type Template struct {
	Description string `yaml:"description"`
	Command     string `yaml:"command"`
	Model       string `yaml:"model"`
	// Effort is how hard the model should think, in agentmux's own vocabulary
	// (see harnessarg.Levels). It pairs with Model to make a template a role
	// rather than a harness listing: a reviewer can be the strongest model at
	// the highest effort while a builder stays mid-range. Which flag carries it
	// is a per-harness detail resolved by harnessarg.
	Effort       string            `yaml:"effort"`
	SystemPrompt string            `yaml:"system_prompt"`
	Prompt       string            `yaml:"prompt"`
	CWD          string            `yaml:"cwd"`
	Shell        string            `yaml:"shell"`
	HarnessType  string            `yaml:"harness_type"`
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
		return Paths{}, apperr.Wrap("config_io_error", err, "resolve home dir")
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		switch runtime.GOOS {
		case "darwin", "linux":
			configHome = filepath.Join(home, ".config")
		default:
			configHome, err = os.UserConfigDir()
			if err != nil {
				return Paths{}, apperr.Wrap("config_io_error", err, "resolve user config dir")
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
		return Config{}, apperr.Wrap("config_io_error", err, "read config file %s", path)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, apperr.Wrap("config_parse_error", err, "parse config file %s", path)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.Defaults.Tmux.Socket == "" || c.Defaults.Tmux.Socket == DefaultSocketPath {
		c.Defaults.Tmux.Socket = RecommendedSocketPath()
	}
	if c.Defaults.Shell == "" {
		c.Defaults.Shell = "/bin/bash -lc"
	}
	if c.Defaults.Status.BusyTTLMS == nil {
		c.Defaults.Status.BusyTTLMS = intPtr(30000)
	}
	if c.Defaults.Status.PromptAckMS == nil {
		c.Defaults.Status.PromptAckMS = intPtr(5000)
	}
	if c.Defaults.Status.TombstoneTTLMS == nil {
		c.Defaults.Status.TombstoneTTLMS = intPtr(24 * 60 * 60 * 1000)
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
	if c.Defaults.Status.PromptAckMS != nil && *c.Defaults.Status.PromptAckMS < 0 {
		return apperr.New("config_invalid", "status.prompt_ack_ms must be non-negative")
	}
	if c.Defaults.Status.TombstoneTTLMS != nil && *c.Defaults.Status.TombstoneTTLMS < 0 {
		return apperr.New("config_invalid", "status.tombstone_ttl_ms must be non-negative")
	}
	if strings.TrimSpace(c.Defaults.Tmux.Socket) == "" {
		return apperr.New("config_invalid", "tmux socket must not be empty")
	}
	if c.Defaults.MaxInstances < 0 {
		return apperr.New("config_invalid", "max_instances must be non-negative")
	}
	for name, tpl := range c.Templates {
		if strings.TrimSpace(name) == "" {
			return apperr.New("config_invalid", "template name must not be empty")
		}
		if strings.TrimSpace(tpl.Command) == "" {
			return apperr.New("config_invalid", fmt.Sprintf("template %q command must not be empty", name))
		}
		// A misspelled effort is a typo worth failing the load over: it would
		// otherwise resolve to "whatever the harness defaults to", which looks
		// like the role working and is invisible in every later command.
		if effort := strings.TrimSpace(tpl.Effort); effort != "" && !harnessarg.ValidLevel(effort) {
			return apperr.New("config_invalid", fmt.Sprintf("template %q effort %q is unknown; valid levels are %s", name, effort, harnessarg.LevelList()))
		}
	}
	return nil
}

func EnsureStateDir(paths Paths) error {
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile), 0o755); err != nil {
		return apperr.Wrap("config_io_error", err, "create config dir")
	}
	if err := os.MkdirAll(paths.StateDir, 0o755); err != nil {
		return apperr.Wrap("config_io_error", err, "create state dir")
	}
	return nil
}

func EnsureDefaultConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return apperr.Wrap("config_io_error", err, "stat config file %s", path)
	}
	if err := os.WriteFile(path, []byte(DefaultConfigYAML), 0o600); err != nil {
		return apperr.Wrap("config_io_error", err, "write default config file %s", path)
	}
	return nil
}

type ResolvedTemplate struct {
	Name         string
	Description  string
	Command      string
	Model        string
	Effort       string
	SystemPrompt string
	Prompt       string
	CWD          string
	Shell        string
	HarnessType  string
	Env          map[string]string
}

type Override struct {
	CWD          *string
	Model        *string
	Effort       *string
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
		Effort:       strings.TrimSpace(tpl.Effort),
		SystemPrompt: tpl.SystemPrompt,
		Prompt:       tpl.Prompt,
		CWD:          firstNonEmpty(tpl.CWD, cfg.Defaults.CWD),
		Shell:        firstNonEmpty(tpl.Shell, cfg.Defaults.Shell),
		HarnessType:  firstNonEmpty(tpl.HarnessType, cfg.Defaults.HarnessType),
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
	if override.Effort != nil {
		rt.Effort = strings.TrimSpace(*override.Effort)
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
