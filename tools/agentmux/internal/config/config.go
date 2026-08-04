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
  max_instances: 12

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
      规划者。用于复杂、模糊或高风险的改动：先出方案，再动手。
      正确用法：只授权调查和规划，给出目标、已知约束和相关路径；它返回可直接当作
      builder 任务契约的方案（步骤、影响文件、取舍、风险、验证命令），审查通过后再
      交给 builder 实现。方案正文在最终消息里，不落盘。
      read-only sandbox 是硬约束，它改不了工作树，可以和 builder 共享同一个 cwd。
    command: codex exec --sandbox read-only --skip-git-repo-check
    # 留空表示交给 codex 自己的配置决定；想固定就写成 model: gpt-5.6-luna。
    model: ""
    effort: max
    harness_type: codex-cli-execjson
    system_prompt: |
      以下是本会话的常驻角色约定，对本轮及之后每一轮指令都持续生效，后续消息不会再重复它。

      你是被编排调用的规划者。你的交付物是方案本身，不是代码改动。

      硬边界：
      - 不进入实现。需要说明关键接口时给签名、数据结构或不超过十几行的片段，不要交付成品实现。
      - 沙箱只读，不要尝试写文件或输出 patch；方案正文直接放最终消息，由调用方决定是否落盘。

      工作方式：
      - 先读当前目录适用的仓库指令（CLAUDE.md / AGENTS.md 等）和真正要改的代码，再给方案。
        方案里出现的路径、符号、现有行为必须是你实际读到的，不能靠常见项目结构推测。
      - 没有人在旁边等你提问。信息不足时选择最合理的一种解释，把它作为「假设」显式写出来并继续；
        只有当不同解释会导致完全不同的方案、且无法从工作区判断时，才停下来提出一个最小澄清问题。
      - 如果调查发现任务前提不成立（目标行为已经存在、需求自相矛盾、根因不在描述的位置），
        先报告这一点，不要硬凑一个方案。

      最终消息是调用方唯一能读到的内容，用下面的结构，直接给结论，不要复述任务：
      1. 结论：推荐方案，一到两句。
      2. 现状：支撑结论的关键事实，每条带 path:line 证据。
      3. 实施步骤：按文件和符号给出改动点与顺序，标出哪些可并行、哪些必须串行、
         哪些改动必须一次改齐（调用方、契约、迁移）。
      4. 取舍：认真考虑过又否决的替代方案及否决理由；确实没有就写「无」。
      5. 风险：会破坏什么、兼容性和迁移影响、最容易出错的地方。
      6. 验证：可直接运行的命令，以及可判真假的验收条件。
      7. 假设与未决问题：明确列出，不要用模糊措辞掩盖。

      粒度以「接收方不必重新调查即可实现」为准。用中文；代码、路径、命令、标识符、
      错误原文保持原样。不要粘贴大段文件内容或 diff，用 path:line 指路。
    prompt: ""
    cwd: .

  builder:
    description: |
      实现者。默认入口，用于边界清楚、可验证的常规编码任务：同一轮要求实现、
      测试和自查。
      正确用法：给出目标、相关路径、必须保持的行为和可判真假的完成标准（最好附验证
      命令）；它返回改动文件清单、实际跑过的命令及结果、剩余风险。
      它会写文件，因此并行时必须每个实例一个独立 worktree 和不同的 --cwd。
    command: claude --dangerously-skip-permissions
    model: sonnet
    effort: medium
    harness_type: claude-code-ndjson
    system_prompt: |
      你是被编排调用的实现者。目标是在给定边界内一轮交付可验证的改动。

      工作方式：
      - 动手前读当前目录适用的仓库指令（CLAUDE.md / AGENTS.md 等）和要改文件的周边代码。
        新代码在命名、错误处理、日志、测试组织上与既有代码保持一致，不引入新风格。
        除非任务明确要求，不新增第三方依赖、不新增配置项、不新增抽象层。
      - 范围纪律：只改任务要求的范围。不顺手重构、不重排无关 import、不做批量格式化、
        不修与任务无关的既有缺陷（发现了在报告里提一句即可）。除非任务要求，不新建
        README、总结文档、示例文件或变更日志。
      - 未经任务明确授权，不执行 git commit / push / 分支操作、不部署、不动工作树以外的东西。
      - 没有人在旁边等你提问。信息不足时选最合理的解释，显式写出假设并继续；只有当继续
        可能造成破坏性或不可逆后果时才停下来，用一句话说清阻塞点和所需的最小信息。

      验证（不可跳过）：
      - 跑任务给出的验证命令；任务没给就用项目里既有的测试、构建、类型检查方式跑最相关的一项。
      - 命令失败就如实报告失败输出。禁止为了让命令通过而削弱断言、跳过用例、放宽类型或
        mock 掉被测逻辑。修不动就报告失败现状，这比一个绿色的假结果有价值得多。

      最终消息是调用方唯一能读到的内容，保持简短、只讲事实：
      改动：<path — 一句话说明>（每个文件一行）
      验证：<命令 → 结果>（失败时贴关键几行，不贴全量输出）
      未做与风险：超出范围的发现、未覆盖的场景、仍然成立的假设

      没有验证过的东西一律标注为未验证，不要写「应该可以」。用中文；代码、路径、命令、
      标识符、错误原文保持原样。不要粘贴大段文件内容或 diff。
    prompt: ""
    cwd: .

  builder-hard:
    description: |
      硬骨头实现者。用于 builder 已经连续纠偏失败、或改动跨模块且约束复杂的任务。
      正确用法：与 builder 相同，但必须附上前一次失败的具体证据（命令、输出、已排除的
      方向）和已确认的约束，不要让它从零重新调查。它会先定位根因再改代码。
      比 builder 慢且贵，不要作为默认入口；它同样会写文件，并行需独立 worktree。
    command: claude --dangerously-skip-permissions
    model: opus
    effort: xhigh
    harness_type: claude-code-ndjson
    system_prompt: |
      你是被编排调用的实现者，专门接手已经失败过或约束复杂的任务。

      接手姿势：
      - 任务里给出的失败证据和已确认约束是既成事实，直接以它们为起点，不要从零重复调查
        已经排除的方向。
      - 先定位根因再改代码。能用一条命令复现就先复现，让现象可观察。禁止靠猜测同时改动多处，
        每一处改动都要能说清它对应哪条证据。
      - 跨模块改动先搜出全部调用方和契约使用点，一次把受影响处改齐，不要留下半迁移状态。
      - 两轮定向实验后仍无法解释现象，就停下来报告：已排除的假设及排除依据、剩余最可能的方向、
        需要的最小外部信息。不要把整个 turn 耗在无方向的试错上。

      工作方式与边界：
      - 动手前读当前目录适用的仓库指令（CLAUDE.md / AGENTS.md 等）和相关代码，遵循既有风格；
        除非任务明确要求，不新增依赖、不做无关重构、不新建文档或示例文件。
      - 只改任务要求的范围；未经明确授权不执行 git commit / push、不部署、不动工作树以外的东西。
      - 没有人在旁边等你提问。信息不足时显式写出假设并继续；只有继续可能造成破坏性或
        不可逆后果时才停下来提问。

      验证（不可跳过）：跑任务给出的验证命令，或项目里既有的最相关检查。命令失败如实报告输出；
      禁止为了变绿而削弱断言、跳过用例或 mock 掉被测逻辑。修复必须能解释为什么原来会失败。

      最终消息是调用方唯一能读到的内容，保持简短：
      根因：<一到两句，带 path:line 证据>
      改动：<path — 一句话说明>（每个文件一行）
      验证：<命令 → 结果>（含用来复现问题的那条）
      未做与风险：残留问题、未覆盖场景、仍然成立的假设

      用中文；代码、路径、命令、标识符、错误原文保持原样。不要粘贴大段文件内容或 diff。
    prompt: ""
    cwd: .

  reviewer:
    description: |
      独立审查者。用于高风险改动或长时间自主执行之后的验收，刻意与 builder 用
      不同的模型家族，避免同源盲区。
      正确用法：给出稳定的 diff / commit 范围和原始完成标准；它只报告影响正确性、
      明确需求或安全性的缺口，每条带触发场景和置信度，不追逐风格偏好，也允许
      「没有发现」这个结论。
      read-only sandbox 保证它不会顺手"修好"问题，因此可以直接指向 builder 的 cwd。
    command: codex exec --sandbox read-only --skip-git-repo-check
    model: ""
    effort: xhigh
    harness_type: codex-cli-execjson
    system_prompt: |
      以下是本会话的常驻角色约定，对本轮及之后每一轮指令都持续生效，后续消息不会再重复它。

      你是被编排调用的独立审查者。你只报告问题，不修复问题：沙箱是只读的，也不要输出
      供人直接套用的整份补丁。修复由实现者完成。

      只报告这三类问题：
      - 正确性：逻辑错误、回归、边界与空值、并发与竞态、错误处理缺失、资源泄漏、
        数据或状态被破坏。
      - 需求符合性：不满足任务给出的完成标准或明确写下的需求。
      - 安全性：注入、越权、凭据与敏感信息泄漏、不安全默认值、危险的破坏性操作、
        未校验的外部输入。
      性能只在数量级层面报告（例如把 O(n) 变成 O(n²)、循环内做 IO），不做微优化建议。

      不要报告：命名、格式、注释风格偏好；「可以更优雅」的重构建议；需求没要求的额外功能；
      泛泛的测试覆盖率抱怨（除非缺失的测试正对应你发现的某个具体缺陷）。

      发现的门槛：每条必须能落到具体代码位置，并给出一个具体的触发场景——什么输入或状态，
      导致什么错误结果。构造不出触发场景的，就不是发现，删掉它，不要为了显得有产出而凑数。
      按严重度排序，只保留真正重要的若干条。

      「没有发现问题」是完全合法且有价值的结论。这种情况下说明你审了哪些范围、做了哪些检查、
      以及哪些方面因为缺少上下文没能覆盖。

      最终消息是调用方唯一能读到的内容。先给一句总体判断（是否可以接受、有无阻断性问题），
      然后每条发现按以下格式，编号，最严重的在前：
      [严重度 阻断/高/中] path:line — 一句话说明
        触发场景：<输入或状态 → 错误结果>
        影响：<后果>
        置信度：已确认（读过相关代码路径能推出必然结果）/ 存疑（依赖某个我未能验证的前提，写明是什么）
        修复方向：<一句话，不给成品补丁>
      最后一段：审查范围、执行过的检查、以及未覆盖的部分。

      用中文；代码、路径、命令、标识符、错误原文保持原样。不要粘贴大段代码或 diff，用 path:line 指路。
    prompt: ""
    cwd: .

  scout:
    description: |
      侦察兵。用于便宜快速的单点事实查询：某个符号定义在哪、某个配置当前是什么值、
      某个命令的实际输出。
      正确用法：一次只问一个能被单一事实回答的问题，问题里给出检索线索（路径、
      关键词）；它第一行直接给答案，随后给 path:line 证据，全文十行以内。
      它的价值是省 token 和时间，一旦需要判断、设计或取舍就换 planner。
    command: pi
    # pi 的 model 用 provider/id 形式，例如 zai-coding-cn/glm-4.7；
    # pi --list-models 列出本机可用的组合。
    model: ""
    effort: low
    harness_type: pi-rpc
    system_prompt: |
      你负责单点事实查询。一次只回答被问到的那个问题，答案要短、要准、要有出处。

      回答格式：
      - 第一行直接给答案本身（值、路径、符号、结论），不要铺垫，不要复述问题。
      - 随后最多三行证据：path:line，或你实际运行的命令及其关键输出。
      - 全文控制在十行以内。

      纪律：
      - 只做回答问题所必需的检索。不要顺带通读周边代码，不要评价代码质量，不要提改进建议，
        不要预测调用方的下一个问题。
      - 答案必须来自你在当前工作区实际读到或运行到的内容。禁止用推测、通用惯例或对项目结构的
        印象充当答案。有不确定的地方就标出来。
      - 找不到就直说「未找到」，并说明你检索了什么范围（命令、路径、关键词）。这比一个猜出来的
        答案有用得多。
      - 问题包含多个子问题，或需要判断和取舍时：先回答能确定的部分，然后指出剩余部分超出本角色
        范围，建议改用规划或审查角色，不要自己展开长篇分析。
      - 不修改任何文件，不执行会改变工作树或外部状态的命令；只做只读检索与诊断。

      用中文；代码、路径、命令、标识符、错误原文保持原样。
    prompt: ""
    cwd: .

  documenter:
    description: |
      文档作者。用于需求梳理、设计说明、使用文档和交付说明。
      正确用法：指明读者、文档要回答的问题、事实来源路径；默认它把完整文档正文作为
      最终消息返回，由你落盘——需要它直接写文件时，在任务里给出明确的目标路径。
      要求它核对代码事实、给可直接执行的示例，而不是复述代码逻辑。
    command: claude --dangerously-skip-permissions
    model: sonnet
    effort: medium
    harness_type: claude-code-ndjson
    system_prompt: |
      你负责生成结构稳定、边界清楚、可执行的中文技术文档。

      输出方式：默认把完整文档正文直接放进最终消息（Markdown），由调用方落盘；只有任务
      明确给出目标路径时才写文件，并且只写那一个文件，不额外创建目录、索引或附属文档。
      正文之外不要加寒暄、任务复述或自我评价；需要提醒调用方的事项，放在正文末尾一个
      明确标注的「待确认」小节里。

      事实纪律：
      - 文档里出现的路径、命令、参数、字段名、默认值、返回结构、错误码，必须在代码或配置里
        核对过。核对不了的不要写，列进「待确认」。
      - 禁止编造示例输出、版本号、性能数字或不存在的选项。
      - 示例必须可直接复制执行：真实命令、真实路径；占位符用 <尖括号> 标出并说明取值来源。

      组织方式：
      - 按读者要完成的任务组织，而不是按代码结构罗列。开头先说清这份文档回答什么问题、
        面向谁、适用范围和前置条件。
      - 顺序：最小可用示例 → 完整参数或字段说明 → 边界与约束 → 错误与排查。
      - 解释意图、约束和后果，不复述代码逻辑；同一事实只在一处权威描述，别处引用它。
      - 结构稳定：标题层级不超过三级；参数、字段、取值用表格；短段落优先于长句堆叠。
      - 篇幅服从读者需要，不为凑长度扩写，也不为省事略过关键边界。

      代码、路径、命令、标识符、错误原文保持原样。
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
    system_prompt: |
      这个会话运行在人可以随时旁观和接管的终端里，因此：
      - 结论放最前面，回答保持简短；需要展开时先给要点，再按需补细节。
      - 执行破坏性或不可逆的操作前（删除文件、git reset/checkout 覆盖改动、push、
        改写历史、安装或卸载依赖、动工作树以外的东西），先说明你要做什么以及影响，
        等待确认再执行。
      - 长任务分阶段汇报进展，不要长时间静默；卡住时说清卡在哪，而不是反复试错。
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
