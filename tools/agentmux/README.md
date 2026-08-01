# agentmux

`agentmux` 是一个面向 AI Agent 的 coding-agent harness 多路复用器（multiplexer）：用同一套命令行接口统一管理多种外部 coding agent 的运行方式，既可以通过结构化协议（如 Claude Code 的 NDJSON、`codex exec --json`）直接驱动 headless 进程，也可以用隔离的 `tmux` session 运行传统 TUI 终端 Agent，并为编排器提供统一的实例管理、输出读取、输入注入和结构化输出。

`mux` 得名于「multiplexer」：早期版本主要靠 tmux pane 承载多个 Agent 实例，因此曾经把 `agentmux` 理解为「tmux 的 agent 化封装」。但现在 headless/结构化 harness（`claude-code-ndjson`、`codex-cli-execjson`、`pi-rpc`）已经是更常用的方式，tmux 只是其中受支持的一种 TUI 运行时，不再是唯一或主要的手段。

当前目标平台：

1. macOS
2. Linux

Windows 不是首要目标。

## 特性

1. 模板名和实例名支持中文
2. 关键命令支持 `--json`
3. `summon` 默认同名复用
4. `capture` 默认返回纯文本，结构化 harness 的 `--json` 默认不含协议原始事件
5. `claude-code-ndjson`、`pi-rpc` 直接使用各自的流式协议驱动 headless 进程，无需 tmux 终端界面
6. `codex-cli-execjson` 直接使用 `codex exec --json` 的事件流驱动 headless 进程，无需 tmux 终端界面
7. TUI harness（`claude-code`、`codex-cli`、`gemini-cli`）默认使用独立 tmux socket `/tmp/agentmux.sock`，且可通过配置修改
8. TUI harness 默认不加载用户 `tmux.conf`，可通过配置显式开启
9. TUI harness 使用 `1 instance = 1 tmux session`

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
13. tmux session 探测现在区分“目标不存在”和运行时故障，tmux 不可用或权限失败时不会误删仍存活的实例
14. 结构化 harness 在启动状态落盘失败时会回滚新进程，`halt`/`interrupt` 也会向上返回信号与状态持久化错误
15. `pi-rpc` usage 按事件 offset 幂等累计，避免流式事件重放导致 token 和费用重复计算
16. CI 在 Linux/macOS 上运行测试与 vet，并在 Linux 上启用 race detector；发布打包前也必须通过测试
17. `prompt` 现在会确认 TUI harness 真的开始工作（`defaults.status.prompt_ack_ms`，默认 5s），消除「刚发完任务 `wait` 立即返回 idle」的假完成
18. `wait` 超时不再是错误：返回 `ok: true`、退出码 0、`status: busy`、`data.timed_out: true`，并新增 `saw_busy`、`elapsed_ms`
19. `capture --json` 对结构化 harness 默认不返回消息数组（`content` 已经是答案）；`--trace`、`--raw`、显式 `--history`、`--since` 都会带回最近 20 条（可调）消息，不返回协议原始事件；`--raw` 恢复完整原始事件
20. 结构化 harness 的 `capture`/`wait` 不再输出恒为 0 的屏幕字段
21. 实例名写在 flag 之后（`capture --history 40 worker`）会给出明确的用法错误，而不是把 flag 当成实例名
22. 新增 `run`：一次调用完成 summon + prompt + wait + capture，是编排的默认入口
23. `wait` 支持多个实例名和 `--mode all|any`，并行分片可以先处理最先完成的那个
24. `capture --since <cursor>` 只返回新产生的内容；每次结构化 `capture` 都会给出 `data.next_cursor`
25. 实例停止后保留为墓碑（`end_reason`、`ended_at`、`last_error`），`list --all` 可见，`inspect` 仍可查询，名字可被同名 `summon` 回收
26. `version --json` 返回能力清单（`commands`、`harness_types`、`features`），便于调用方做特性探测
27. 新增 `doctor`：一次性检查二进制版本/PATH 是否有旧副本遮蔽、配置、状态目录、注册表锁、每个模板命令是否在 PATH 上、以及是否需要 tmux；`version --json` 同时新增 `build_time`、`binary_path`
28. `run` 复用一个仍在忙的实例时不再直接报错或和旧任务混在一起：会在 `--timeout` 预算内先等旧任务结束（`data.queued_ms` 报告花了多久），预算耗尽则报 `instance_busy`；独立的 `prompt --wait-if-busy <duration>` 提供同样的等待
29. `capture --json` 的消息数组默认不返回（见第 19 条）；新增 `capture --new`：像 `--since` 一样只读新增内容，但游标由 agentmux 自己记在实例上并每次前移，调用方不用来回传游标
30. 新增 `run --detach`：发出任务立即返回，不等待也不读取输出，用于并行分片；新增 `wait --collect`（单实例和多实例都支持）：等到完成后顺带把精简后的输出带回来，并行场景不再需要每个分片单独调一次 `capture`
31. `summon`/`run` 在目标 `cwd` 已有其他存活实例时，`data.warnings` 会给出 `cwd_shared:<name>`（不阻断）；agentmux 隔离的是 Agent 进程而不是文件，多个写入型实例共享同一个工作目录会在 Git 状态和构建产物上互相竞争

命令职责上建议这样理解：

0. `run` 用于一次性委派：创建或复用实例、发指令、等待、读回结果，只有一次调用
1. `list` 用于批量查看实例及其当前状态
2. `inspect --json` 用于查看单个实例当前状态、`pane_title` 和元数据
3. `wait` 用于阻塞到 agent 看起来完成当前工作；超时返回 `timed_out: true` 而不是报错
4. `capture` 用于读取实例输出；TUI harness 返回终端文本，结构化 harness 返回协议消息聚合后的文本和结构化数据

面向 Agent 编排的读取建议：只要目的是「看外部 Agent 说了什么」，用不带 `--json` 的 `capture`，它只打印聚合后的 `content`；需要 `usage`、`thread_id`、`turn_state` 时再加 `--json`。

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
cd /path/to/ai-skills/tools/agentmux
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
cd /path/to/ai-skills/tools/agentmux
./scripts/install.sh
```

这个脚本会做两件事：

1. 编译并安装二进制到 `~/.local/bin/agentmux`
2. 在不存在配置时安装默认配置到 `~/.config/agentmux/config.yaml`

可选环境变量：

1. `BIN_DIR=/custom/bin`
2. `OVERWRITE_CONFIG=1`

示例：

```bash
BIN_DIR=$HOME/bin OVERWRITE_CONFIG=1 ./scripts/install.sh
```

安装完成后，建议先跑一次环境自检：

```bash
agentmux doctor --json
```

`doctor` 会一次性检查：这次调用实际跑的是哪个二进制、PATH 上是否有旧版本在遮蔽它（这是最常见的“装了新版本但行为还是老的”原因）、配置和状态目录、注册表锁、每个模板命令是否能在 PATH 上找到、以及需要 tmux 的模板是否真的有 tmux。只有 `fail` 状态会让退出码非零；`warn` 是提醒但不阻塞。

确认环境健康后再看看有哪些模板：

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
3. `README.md`

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
cp /path/to/ai-skills/tools/agentmux/examples/config.yaml ~/.config/agentmux/config.yaml
```

示例配置文件见 [config.yaml](examples/config.yaml)。

## 最小配置示例

```yaml
version: 1

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

一次性委派一个任务（默认入口）：

```bash
agentmux run --template codex-cli-execjson --cwd ~/work/project --prompt "修复登录重试" --timeout 10m --json
agentmux run --template claude-code-ndjson --name 审查-A --prompt-file ./task.md --json
cat task.md | agentmux run --template pi-rpc --stdin --json
```

`run` 结束后实例保留，可以继续追加指令或检查；重复 `run` 同一个名字会在同一会话里继续。单路阻塞式的 `run` 不适合并行——它会阻塞到自己这一路结束；并行场景改用 `run --detach`（发出即返回）逐个分片调用，再统一 `wait --mode any --collect`：

```bash
agentmux run --template codex-cli-execjson --name 分片-A --cwd /wt/a --prompt "..." --detach --json
agentmux run --template codex-cli-execjson --name 分片-B --cwd /wt/b --prompt "..." --detach --json
agentmux wait 分片-A 分片-B --mode any --timeout 5m --collect --json
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

立即读取当前输出：

```bash
agentmux capture 编码助手-A                       # 只输出聚合文本，最省 token
agentmux capture 编码助手-A --history 120         # TUI harness：向上抓 120 行屏幕历史
agentmux capture 编码助手-A --json                # 结构化 harness：默认只有 content 和状态字段，没有 messages
agentmux capture 编码助手-A --trace --json        # 需要消息轨迹时再加，默认最近 20 条
agentmux capture 编码助手-A --scope session --history 40 --json   # --history 在结构化 harness 上等价于 --trace + 限制条数
agentmux capture 编码助手-A --json --since 18422       # 只看上次之后的新内容（隐含 --trace）
agentmux capture 编码助手-A --new --json               # 同上，但游标由 agentmux 自己记账，不用手动传
agentmux capture 编码助手-A --json --history 0 --raw   # 调试用：完整消息与原始事件
```

等待 agent 完成当前工作，不返回内容：

```bash
agentmux wait 编码助手-A --stable 1500 --timeout 30s --json
agentmux wait 编码助手-A --timeout 3m --json   # 超时返回 timed_out: true，退出码仍为 0
agentmux wait 分片-A 分片-B 分片-C --mode any --timeout 5m --json   # 谁先完成就返回谁
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

查看版本和能力：

```bash
agentmux version
agentmux version --json   # 额外返回 commands / harness_types / features
```

查看实例（含已停止的墓碑）：

```bash
agentmux list
agentmux list --all --json
```

## 命令语义

### `summon`

1. 同名实例存在时默认复用
2. 同名实例不存在时创建
3. 新建实例时，若给 `--prompt`，立即发送该消息
4. 复用实例时，若给 `--prompt`，也发送该消息
5. 复用实例时不会隐式修改既有实例配置
6. 若目标 `cwd` 已有其他存活实例，`data.warnings` 会给出 `cwd_shared:<name>`，不阻断创建

### `run`

1. `run` = `summon`（不带 prompt）→ 若复用到的实例正忙则在 `--timeout` 预算内等待 → 发送本次任务的 prompt → `wait` → `capture`，一次调用、一个退出码、一份回执
2. 复用到忙实例时不会立刻报错或把新任务和旧任务混在一起：先等旧任务收尾，`data.queued_ms` 报告花掉的等待时间；预算耗尽则报 `instance_busy`
3. `--detach` 在发送完 prompt 后立即返回，不等待也不读取输出，`data.detached: true`；仍会先按上一条等待忙实例，仍不能与 `--history`/`--trace`/`--raw` 同时使用
4. `--timeout` 到期且已发送 prompt 之后，不是失败：`data.timed_out: true`，可以对同一个实例再次 `wait`

### `capture`

1. `capture` 统一表示“立即读取实例当前可观测输出”，不等待工作完成
2. 默认 `--scope current`：TUI harness 读取当前屏幕；结构化 harness 读取当前或最近 turn
3. `--scope session`：结构化 harness 读取整段已记录会话；TUI harness 仍按当前屏幕/历史行读取
4. TUI harness 通过 `tmux capture-pane` 抓纯文本
5. `claude-code-ndjson` 读取 Claude Code 的 `output.jsonl`，文本模式只输出聚合后的 `content`
6. `codex-cli-execjson` 读取 `codex exec --json` 写入的 `output.jsonl`，文本模式只输出聚合后的 `content`
7. `capture --json` 对 TUI harness 返回屏幕字段；对结构化 harness 默认只返回 `content`、`usage`、`turns`、`last_error`、`next_cursor` 等状态字段，不含逐条 `messages`——`--trace`、`--raw`、显式 `--history`、`--since`、`--new` 中任意一个都会带回 `messages`（`claude-code-ndjson` 额外有 `claude_session_id`；`codex-cli-execjson` 额外有 `thread_id`、`turn_state`）
8. TUI harness 下 `--history` 控制向上抓取的历史行数；结构化 harness 下表示最近 N 条归一化消息，同时隐含开启 `messages`
9. `--since <cursor>` 显式传游标；`--new` 效果相同，但游标由 agentmux 记在实例上并自动前移，调用方不用来回传递（两者互斥）
10. `capture` 的主要职责是读输出，不是做状态查询，也不是等待接口
11. 若只想获知某个实例当前状态，应使用 `inspect --json`
12. 若需要等待 agent 完成工作，应先执行 `wait`

### `wait`

1. 语义上表示“等待 agent 完成当前工作”，不返回屏幕内容
2. 适合上层 Agent 只想阻塞等待、避免传回大段文本时使用
3. 若实例的 `harness_type` 支持 `pane_title` 信号（如 `claude-code`、`codex-cli`、`gemini-cli`），优先通过 `pane_title` 判定是否完成
4. `claude-code-ndjson` 通过 user replay、`result` 和 `session_state_changed=idle` 等协议事件判定完成，不依赖屏幕稳定
5. `codex-cli-execjson` 等待 turn 进程退出并解析 `turn.completed`/`turn.failed`；turn 失败也算“等到了”，失败原因通过 `capture --json` 的 `last_error` 暴露
6. 其他 harness 则回退到“屏幕静止”这类通用启发式
7. 支持 `pane_title` 信号的 harness 会走轻量 pane 元信息轮询，不再抓取屏幕文本
8. 若只是想知道当前是 `idle` 还是 `busy`，单实例使用 `inspect --json`，多实例使用 `list --json`
9. `--collect` 让每个 `done` 的实例附带一次精简 `capture` 的结果（`content` 及状态字段），单实例和多实例都支持；仍在 pending 的实例不读取。并行分片因此是 `run --detach` × N 之后跟一次 `wait --mode any --collect`，不用再对每个分片单独 `capture`

### `prompt`

1. `--text` 发送文本
2. `--stdin` 从标准输入读取完整文本
3. `--key` 发送白名单特殊键
4. `--text` 与 `--stdin` 会在粘贴文本后自动提交
5. 若文本已进入输入框但未开始执行，可补发 `--key Enter`
6. `claude-code-ndjson` 下 `--text`/`--stdin` 写入一条 user NDJSON 消息；`--key C-c` 会尝试中断进程，其余 TUI 导航键为 no-op
7. `codex-cli-execjson` 下 `--text`/`--stdin` 启动一个新 turn 进程；实例正在跑 turn 时会报错 `execjson_instance_busy`，因为 codex 无法向执行中的 turn 追加输入
8. `codex-cli-execjson` 下 `--key C-c` 会中断当前 turn（进程直接结束），其余 TUI 导航键为 no-op
9. `--wait-if-busy <duration>` 让 `prompt` 在发送前先等实例把当前工作收尾，跨三种 harness 行为一致；默认（不传）保持发送前不等待的历史行为

### `wait` 的完成判定

1. TUI harness 的 `pane_title` 在 harness 反应过来之前仍然描述上一轮，直接相信会把未开始的工作判成已完成
2. 因此 `prompt` 发送文本后会先确认 `pane_title` 切到 busy 标记，最多等 `defaults.status.prompt_ack_ms`（默认 `5000`，`0` 关闭）
3. 确认成功后，本轮的 idle 信号立即可信，`wait` 可以马上返回
4. 未确认时，`wait` 在 `--stable` 窗口内不接受 idle 信号；`data.saw_busy` 说明本次等待是否真的观察到 harness 在工作
5. 超时返回 `ok: true` + `status: busy` + `data.timed_out: true`，表示「仍在工作」，不是失败

### `busy` 状态

1. `prompt` 后实例会进入 `busy`
2. 若后续执行 `wait`，状态会正常收敛回 `idle`
3. 若实例的 `harness_type` 支持 `pane_title` 信号（如 `claude-code`、`codex-cli`、`gemini-cli`），还可以通过 `pane_title` 精确收敛到 `idle`
4. `claude-code-ndjson` 会根据 Claude Code 协议事件收敛到 `idle`；中断后若连续 5 秒没有新事件，也会兜底回到 `idle` 并记录 `interrupted`
5. `codex-cli-execjson` 在 turn 进程退出后收敛到 `idle`；两个 turn 之间没有进程存在，`idle` 且 `process_id=0` 是正常状态
6. 若调用方没有继续观测，通用 TUI harness 的 `busy` 会在 `defaults.status.busy_ttl_ms` 到期后自动退化为 `idle`
7. 有 `pane_title` 信号的 harness 在启动确认窗口内不会仅凭上一轮遗留的标题退回 `idle`
8. 若 `busy_ttl_ms: 0`，表示禁用自动退化，实例不会仅因 TTL 到期而自动回到 `idle`

### 墓碑

1. 实例停止后不会从注册表消失，而是保留 `status`（`exited`/`lost`）、`end_reason`、`ended_at` 和 `last_error`
2. 这样调用方能区分「名字写错了」（`instance_not_found`）和「worker 死了，原因是 X」（`process_not_running`）
3. `list` 默认隐藏墓碑，`list --all` 显示；`inspect` 对墓碑正常返回
4. 墓碑不占 `max_instances` 配额，同名 `summon` 会直接回收名字
5. 超过 `defaults.status.tombstone_ttl_ms`（默认 24 小时）自动清除

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
