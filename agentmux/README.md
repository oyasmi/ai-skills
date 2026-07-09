# agentmux

`agentmux` 是一个面向 AI Agent 的命令行控制器。它可以用隔离的 `tmux` session 运行外部终端 Agent，也可以通过 Claude Code 的 NDJSON 协议直接管理 Claude 进程，并提供适合编排器使用的实例管理、输出读取、输入注入和结构化输出。

当前目标平台：

1. macOS
2. Linux

Windows 不是首要目标。

## 特性

1. 默认使用独立 tmux socket `/tmp/agentmux.sock`，且可通过配置修改
2. 默认不加载用户 `tmux.conf`，可通过配置显式开启
3. TUI harness 使用 `1 instance = 1 tmux session`
4. 模板名和实例名支持中文
5. 关键命令支持 `--json`
6. `summon` 默认同名复用
7. `capture` 默认返回纯文本
8. `claude-code-ndjson` 可绕过 tmux 终端界面，直接使用 Claude Code 的 stream-json 协议
9. `codex-cli-execjson` 可绕过 tmux 终端界面，直接使用 `codex exec --json` 的事件流

## 近期优化

1. `prompt` 新增 `--stdin`，可从标准输入读取完整文本；对部分 TUI harness，超长输入仍更适合走文件引用模式
2. 新增 `version` 命令，支持纯文本和 `--json` 输出，便于 Agent 判断功能版本
3. 新增 `wait` 命令，用于等待 agent 完成当前工作，不返回内容，适合节省 token
4. `wait --stable` 支持整数毫秒和 Go duration 两种格式，例如 `1500`、`1500ms`、`1.5s`
5. `tmux` socket 路径从硬编码改为配置项 `defaults.tmux.socket`
6. `busy` 状态新增 TTL 自动退化，默认 `30s`，避免发送 prompt 后因缺少后续观测而永久停留在 `busy`
7. `instances.json` 现在使用文件锁和原子替换写入，降低多进程并发编排时的数据丢失和文件损坏风险
8. `capture`/`wait` 内部减少了一次重复的注册表事务，避免不必要的注册表读改写
9. 新增 `harness_type` 驱动的状态检测，`claude-code`、`codex-cli`、`gemini-cli` 可用 `pane_title` 精确判断 idle，`wait` 可提前返回
10. `inspect`、`list`、`capture`、`wait` 的 JSON 输出现在包含 `harness_type` 或 `pane_title` 等状态观测字段
11. 新增 `claude-code-ndjson` harness type，通过 Claude Code `stream-json` 协议直接读写 NDJSON，`wait` 可等待协议级完成事件，`capture --json` 可返回结构化消息和 usage 信息
12. 新增 `codex-cli-execjson` harness type，通过 `codex exec --json` 事件流驱动 Codex CLI，`wait` 等待 turn 进程退出与终局事件，`capture --json` 返回结构化消息、`thread_id` 和 usage

命令职责上建议这样理解：

1. `list` 用于批量查看实例及其当前状态
2. `inspect --json` 用于查看单个实例当前状态、`pane_title` 和元数据
3. `wait` 用于阻塞到 agent 看起来完成当前工作
4. `capture` 用于读取实例输出；TUI harness 返回终端文本，结构化 harness 返回协议消息聚合后的文本和结构化数据

### 两种结构化 harness 的差异

`claude-code-ndjson` 与 `codex-cli-execjson` 都不依赖 tmux，但底层进程模型完全不同：

| | `claude-code-ndjson` | `codex-cli-execjson` |
|---|---|---|
| 进程模型 | 1 实例 = 1 长驻进程 = N turns | 1 实例 = N 个短命进程，每 turn 一个 |
| 多轮机制 | 同一进程内连续 turn | `codex exec resume <thread_id>` |
| 实例存活 | 等同于进程存活 | 与进程无关；turn 之间没有进程 |
| `summon` | 立即启动进程 | 不启动任何进程 |
| busy 时 `prompt` | 入队 | 报错 `execjson_instance_busy` |
| 成本字段 | `total_cost_usd` | codex 不提供，恒为 0 |

因此 `codex-cli-execjson` 实例在两个 turn 之间是 `idle` 且 `process_id` 为 0，这是正常状态，不代表实例已退出。

补充约束：

1. `summon` 只会复用“同名且同模板”的实例
2. 若同名实例来自其他模板，命令会直接报错，调用方应改用新名字

## 依赖

运行时依赖：

1. `tmux >= 3.x`，用于 `claude-code`、`codex-cli`、`gemini-cli` 等 TUI harness
2. `claude`，用于 `claude-code-ndjson` harness
3. `codex >= 0.142`，用于 `codex-cli-execjson` harness

构建依赖：

1. `Go >= 1.24`

## 构建

```bash
cd /path/to/agentmux
go build -o ./bin/agentmux ./cmd/agentmux
```

如果当前环境对 Go 默认缓存目录有限制，可以把缓存切到项目内：

```bash
GOCACHE=$PWD/.cache/go-build \
GOPATH=$PWD/.cache/go-path \
GOMODCACHE=$PWD/.cache/go-mod \
go build -o ./bin/agentmux ./cmd/agentmux
```

## 安装

最直接的安装方式：

```bash
cd /path/to/agentmux
./scripts/install.sh
```

这个脚本会做三件事：

1. 编译并安装二进制到 `~/.local/bin/agentmux`
2. 在不存在配置时安装默认配置到 `~/.config/agentmux/config.yaml`
3. 安装配套 skill 到 `$CODEX_HOME/skills/agentmux`，如果 `CODEX_HOME` 未设置则使用 `~/.codex/skills/agentmux`

可选环境变量：

1. `BIN_DIR=/custom/bin`
2. `INSTALL_SKILL=0`
3. `OVERWRITE_CONFIG=1`
4. `SKILL_TARGET_DIR=/custom/skills/agentmux`

示例：

```bash
BIN_DIR=$HOME/bin OVERWRITE_CONFIG=1 ./scripts/install.sh
INSTALL_SKILL=0 ./scripts/install.sh
```

安装完成后，建议先确认：

```bash
agentmux template list --json
```

也可以先通过帮助探索命令：

```bash
agentmux --help
agentmux help summon
agentmux capture --help
```

排查问题时，如果需要结构化调试日志，可以临时设置：

```bash
AGENTMUX_LOG_LEVEL=debug agentmux inspect 编码助手-A --json
```

调试日志会输出到 `stderr`，命令结果仍按原格式写到 `stdout`。

## 发布

发布脚本：

```bash
./scripts/release.sh
```

GitHub Actions 也已经配置为自动打包：

1. 创建 `v*` tag 时自动构建、上传 artifact，并发布 GitHub Release
2. 支持手动触发 `AgentMux Package` workflow

默认会构建这些目标：

1. `darwin/amd64`
2. `darwin/arm64`
3. `linux/amd64`
4. `linux/arm64`

产物输出到 `dist/`，每个 tarball 包含：

1. `agentmux` 可执行文件
2. `config.yaml` 示例配置
3. `skill-agentmux/` skill 目录
4. `README.md`

可以通过环境变量覆盖版本号和输出目录：

```bash
VERSION=v0.1.0 ./scripts/release.sh
DIST_DIR=$PWD/out VERSION=v0.1.0 ./scripts/release.sh
```

发布完成后会额外生成：

```text
dist/checksums.txt
```

## 配置

主配置文件路径：

`~/.config/agentmux/config.yaml`

如果该文件不存在，`agentmux` 会在首次执行非帮助命令时自动写入默认配置。

可以先复制示例配置：

```bash
mkdir -p ~/.config/agentmux
cp /path/to/agentmux/examples/config.yaml ~/.config/agentmux/config.yaml
```

示例配置文件见 [config.yaml](examples/config.yaml)。

## Skill

配套 skill 目录位于：

1. [SKILL.md](skills/agentmux/SKILL.md)
2. [openai.yaml](skills/agentmux/agents/openai.yaml)

这个 skill 面向上层编排型 Agent，要求优先通过 `agentmux ... --json` 管理外部终端 Agent 实例，而不是直接调用 `tmux`。

## 最小配置示例

```yaml
version: 1

defaults:
  tmux:
    socket: /tmp/agentmux.sock
    load_user_config: false
  status:
    busy_ttl_ms: 30000
  shell: /bin/bash -lc
  cwd: .
  env:
    TERM: xterm-256color
  capture:
    history: 120
    stable_ms: 1500
    poll_ms: 250

templates:
  claude-code:
    description: Claude Code 通用编程智能体
    command: claude --dangerously-skip-permissions --model $MODEL
    model: anthropic/claude-sonnet-4.5
    harness_type: claude-code
    system_prompt: ""
    prompt: ""
    cwd: .

  claude-code-ndjson:
    description: Claude Code 通用编程智能体（NDJSON 结构化模式）
    command: claude --dangerously-skip-permissions --model $MODEL
    model: anthropic/claude-sonnet-4.5
    harness_type: claude-code-ndjson
    system_prompt: ""
    prompt: ""
    cwd: .
```

Codex CLI 模板建议显式声明 `harness_type`，这样 `busy -> idle` 检测会更精确：

```yaml
templates:
  codex-cli:
    command: codex --model $MODEL
    model: openai/gpt-5.4
    harness_type: codex-cli
```

Claude Code NDJSON 模板适合上层编排器使用。它不启动 TUI，不使用 tmux，而是通过 Claude Code 的 `-p --input-format stream-json --output-format stream-json` 协议直接交互：

```yaml
templates:
  claude-code-ndjson:
    command: claude --dangerously-skip-permissions --model $MODEL
    model: anthropic/claude-sonnet-4.5
    harness_type: claude-code-ndjson
```

`agentmux` 会自动追加 NDJSON 所需的协议参数，例如 `-p`、`--input-format stream-json`、`--output-format stream-json`、`--verbose`、`--include-partial-messages`、`--replay-user-messages` 和会话参数；这些参数不需要写进模板命令。

Codex CLI execjson 模板同样不启动 TUI、不使用 tmux，而是每个 turn 拉起一个 `codex exec --json` 进程：

```yaml
templates:
  codex-cli-execjson:
    command: codex exec --sandbox workspace-write --skip-git-repo-check
    model: ""
    harness_type: codex-cli-execjson
```

`command` 必须是一个只带父级 flag 的 `codex exec` 前缀。`agentmux` 会自行追加 `resume <thread_id>`、`--json` 和读取 prompt 的 `-`，这些不要写进模板命令。

`agentmux` 会在 `summon` 阶段拒绝以下命令，以便尽早失败：

1. 含 `--json` / `-o` / `--output-last-message`：由 agentmux 管理
2. 含 `resume` / `review` 子命令：由 agentmux 注入
3. 含 `--ask-for-approval` / `-a`：`codex exec` 不接受该参数，会直接报错退出；权限请用 `--sandbox`
4. 含 `--ephemeral`：不落盘 session，会使多轮 `resume` 永久不可用
5. 含管道、重定向、`&&` 或命令替换

默认模板不传 `--model`，交由 codex 自身配置决定，因为可用模型取决于账号与套餐。需要固定模型时自行加上 `--model $MODEL` 并设置 `model`。

## 常用命令

列出模板：

```bash
agentmux template list
agentmux template list --json
```

创建或复用实例：

```bash
agentmux summon --template claude-code --name 编码助手-A --cwd ~/work/project
agentmux summon --template claude-code-ndjson --name 编码助手-N --cwd ~/work/project
```

创建并发送首条消息：

```bash
agentmux summon --template claude-code --name 编码助手-A --prompt "先阅读项目并总结结构" --json
agentmux summon --template claude-code-ndjson --name 编码助手-N --prompt "先阅读项目并总结结构" --json
```

查看实例详情：

```bash
agentmux inspect 编码助手-A --json
```

立即抓取当前屏幕文本：

```bash
agentmux capture 编码助手-A --history 120 --json
```

等待 agent 完成当前工作，不返回内容：

```bash
agentmux wait 编码助手-A --stable 1500 --timeout 30s --json
```

继续发送消息：

```bash
agentmux prompt 编码助手-A --text "继续修复剩余失败测试" --json
printf '%s\n' "补充说明第一行" "补充说明第二行" | agentmux prompt 编码助手-A --stdin --json
```

发送特殊键：

```bash
agentmux prompt 编码助手-A --key C-c --json
agentmux prompt 编码助手-A --key Enter --json
```

人工 attach：

```bash
agentmux attach 编码助手-A
```

停止实例：

```bash
agentmux halt 编码助手-A
agentmux halt 编码助手-A --timeout 8s
agentmux halt 编码助手-A --immediately
agentmux halt 编码助手-A --json
```

查看版本：

```bash
agentmux version
agentmux version --json
```

## 命令语义

### `summon`

1. 同名实例存在时默认复用
2. 同名实例不存在时创建
3. 新建实例时，若给 `--prompt`，立即发送该消息
4. 复用实例时，若给 `--prompt`，也发送该消息
5. 复用实例时不会隐式修改既有实例配置

### `capture`

1. TUI harness 通过 `tmux capture-pane` 抓纯文本
2. `claude-code-ndjson` 读取 Claude Code 的 `output.jsonl`，文本模式只输出聚合后的 `content`
3. `codex-cli-execjson` 读取 `codex exec --json` 写入的 `output.jsonl`，文本模式只输出聚合后的 `content`
4. `capture --json` 对 TUI harness 返回屏幕字段；对 `claude-code-ndjson` 额外返回 `messages`、`usage`、`claude_session_id`、`turns`；对 `codex-cli-execjson` 额外返回 `messages`、`usage`、`thread_id`、`turns`、`turn_state`、`last_error`
5. TUI harness 下 `--history` 控制向上抓取的历史行数；结构化 harness 下表示最近 N 条归一化消息
6. 总是立即返回当前可见/可解析输出，不等待“工作完成”
7. `capture` 的主要职责是读输出，不是做状态查询，也不是等待接口
8. 若只想获知某个实例当前状态，应使用 `inspect --json`
9. 若需要等待 agent 完成工作，应先执行 `wait`

### `wait`

1. 语义上表示“等待 agent 完成当前工作”，不返回屏幕内容
2. 适合上层 Agent 只想阻塞等待、避免传回大段文本时使用
3. 若实例的 `harness_type` 支持 `pane_title` 信号（如 `claude-code`、`codex-cli`、`gemini-cli`），优先通过 `pane_title` 判定是否完成
4. `claude-code-ndjson` 通过 user replay、`result` 和 `session_state_changed=idle` 等协议事件判定完成，不依赖屏幕稳定
5. `codex-cli-execjson` 等待 turn 进程退出并解析 `turn.completed`/`turn.failed`；turn 失败也算“等到了”，失败原因通过 `capture --json` 的 `last_error` 暴露
6. 其他 harness 则回退到“屏幕静止”这类通用启发式
7. 支持 `pane_title` 信号的 harness 会走轻量 pane 元信息轮询，不再抓取屏幕文本
8. 若只是想知道当前是 `idle` 还是 `busy`，单实例使用 `inspect --json`，多实例使用 `list --json`

### `prompt`

1. `--text` 发送文本
2. `--stdin` 从标准输入读取完整文本
3. `--key` 发送白名单特殊键
4. `--text` 与 `--stdin` 会在粘贴文本后自动提交
5. 若文本已进入输入框但未开始执行，可补发 `--key Enter`
6. `claude-code-ndjson` 下 `--text`/`--stdin` 写入一条 user NDJSON 消息；`--key C-c` 会尝试中断进程，其余 TUI 导航键为 no-op
7. `codex-cli-execjson` 下 `--text`/`--stdin` 启动一个新 turn 进程；实例正在跑 turn 时会报错 `execjson_instance_busy`，因为 codex 无法向执行中的 turn 追加输入
8. `codex-cli-execjson` 下 `--key C-c` 会中断当前 turn（进程直接结束），其余 TUI 导航键为 no-op

### `busy` 状态

1. `prompt` 后实例会进入 `busy`
2. 若后续执行 `wait`，状态会正常收敛回 `idle`
3. 若实例的 `harness_type` 支持 `pane_title` 信号（如 `claude-code`、`codex-cli`、`gemini-cli`），还可以通过 `pane_title` 精确收敛到 `idle`
4. `claude-code-ndjson` 会根据 Claude Code 协议事件收敛到 `idle`
5. `codex-cli-execjson` 在 turn 进程退出后收敛到 `idle`；两个 turn 之间没有进程存在，`idle` 且 `process_id=0` 是正常状态
6. 若调用方没有继续观测，通用 TUI harness 的 `busy` 会在 `defaults.status.busy_ttl_ms` 到期后自动退化为 `idle`
7. 若 `busy_ttl_ms: 0`，表示禁用自动退化，实例不会仅因 TTL 到期而自动回到 `idle`

### `attach`

1. TUI harness 会进入对应 tmux session
2. `claude-code-ndjson` 和 `codex-cli-execjson` 没有交互式 TUI，`attach` 会跟随实例的 `output.jsonl`，用于调试事件流

## 并发安全

1. `instances.json` 的读改写现在在文件锁保护下执行
2. 注册表写入使用临时文件加原子替换，避免出现半写入 JSON
3. 这能显著降低多个 `agentmux` 进程并发操作同一注册表时的丢写风险

## 输出格式

面向 Agent 使用时，优先添加 `--json`。

成功输出示例：

```json
{
  "ok": true,
  "command": "inspect",
  "instance": "编码助手-A",
  "status": "idle",
  "data": {
    "harness_type": "claude-code",
    "pane_title": "✳ Task complete"
  }
}
```

`claude-code-ndjson` 的 `capture --json` 会包含额外协议字段，例如：

```json
{
  "ok": true,
  "command": "capture",
  "instance": "编码助手-N",
  "status": "idle",
  "data": {
    "content": "完成。",
    "claude_session_id": "1b94e52d-fbe1-496b-859b-e05731e52801",
    "messages": [],
    "usage": {
      "input_tokens": 22104,
      "output_tokens": 53,
      "cache_creation_input_tokens": 0,
      "cache_read_input_tokens": 3584,
      "total_cost_usd": 0.0681822
    },
    "turns": 1
  }
}
```

`codex-cli-execjson` 的 `capture --json` 字段略有不同：codex 不提供成本，`cache_read_input_tokens` 来自 `cached_input_tokens`，并额外给出 `reasoning_output_tokens`：

```json
{
  "ok": true,
  "command": "capture",
  "instance": "codex-smoke",
  "status": "idle",
  "data": {
    "content": "alpha",
    "thread_id": "019f46a1-90c1-7751-8ccc-ad04a6c65f4b",
    "messages": [],
    "usage": {
      "input_tokens": 11924,
      "output_tokens": 5,
      "cache_creation_input_tokens": 0,
      "cache_read_input_tokens": 9600,
      "reasoning_output_tokens": 0,
      "total_cost_usd": 0
    },
    "turns": 1,
    "turn_state": "completed",
    "last_error": ""
  }
}
```

错误输出示例：

```json
{
  "ok": false,
  "command": "capture",
  "instance": "编码助手-A",
  "error_code": "instance_not_found",
  "error": "instance \"编码助手-A\" not found"
}
```

## 项目文档

设计和规格文档位于：

1. [design.md](docs/design.md)
2. [cli-spec.md](docs/cli-spec.md)
3. [config-spec.md](docs/config-spec.md)
4. [skill-spec.md](docs/skill-spec.md)
