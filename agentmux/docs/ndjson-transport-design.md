# claude-code-ndjson Direct Process Transport 实现文档

## 1. 背景与目标

`agentmux` 现有 Claude Code 路径通过 tmux 驱动 TUI：

```text
agentmux CLI -> tmux send-keys / capture-pane -> Claude Code TUI
```

这条路径足够通用，但它把面向人的终端界面当作机器协议使用，会带来几个结构性问题：

1. `wait` 依赖 `pane_title` 或屏幕稳定启发式，不能严格等价于“模型本轮完成”。
2. `capture` 只能拿终端文本，无法稳定获得工具调用、usage、cost、session id 等结构化信息。
3. `prompt` 通过终端粘贴和回车提交，长文本和特殊内容都比协议输入脆弱。
4. Claude Code 已经提供 `-p --input-format stream-json --output-format stream-json`，agentmux 应该直接使用这条程序化协议。

本设计新增 `claude-code-ndjson` harness type。该路径不使用 tmux，不依赖终端仿真，不读屏幕，也不发送按键。agentmux 直接启动并管理 Claude Code 进程，通过 stdin NDJSON 输入和 stdout NDJSON 输出进行控制。

```text
agentmux CLI -> FIFO(stdin) / JSONL(stdout) -> Claude Code print stream-json process
```

## 1.1 claude-agent-acp 参考校正

`~/projects/claude-agent-acp` 是一个已运行的 Claude Code 到 ACP 协议桥。它当前通过 `@anthropic-ai/claude-agent-sdk` 启动 Claude Code，而不是手写裸 CLI/FIFO，但它对 agentmux 的 ndjson 设计有直接校正价值：

1. 它使用长期输入流（`Pushable<SDKUserMessage>`）驱动 `query()`，说明 Claude Code/SDK 的 stream-json 路径适合多 turn 长连接，而不是只能一次 prompt 后退出。
2. 它启用 `replay-user-messages`，并给每个 user message 生成 `uuid`。prompt loop 依赖 replay 出来的 user uuid 识别“这一条 prompt 已经被 Claude 接收”，避免把后台任务或前一个 prompt 的 result 错归到当前 prompt。
3. 它设置 `CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1`，并在 `system/session_state_changed state=idle` 时认为会话真正空闲。`result` 说明一个 Claude 执行结果完成，但 idle 事件更适合作为“当前进程没有剩余后台工作”的状态信号。
4. 它开启 partial messages，并从 `stream_event` 的 `message_start/message_delta` 中更新 usage。agentmux 的 parser 不应只依赖最终 `result.usage`。
5. 它的集成测试确认：只创建 session 但从未发送 prompt 时，Claude 本地 transcript 没有消息，后续 `resume` 会失败。因此 agentmux 不能假设 `summon` 成功后就一定拥有可恢复的 Claude 会话；至少要等首个 prompt 产生持久消息后，才把 `resume_available` 视为 true。
6. 它在 cancel/close 时调用 `query.interrupt()`、abort controller 和 close。裸 CLI 方案没有 SDK 的 `interrupt()` 方法，所以 agentmux 若要提供中断语义，必须显式定义信号策略，而不能只依赖 FIFO。

以下设计已经吸收这些结论：强制开启 user replay、记录 prompt uuid 队列、优先用 `session_state_changed=idle` 收敛状态，同时保留 `result` 作为 turn 完成和 usage/cost 来源。

## 2. 非目标

1. 不复用 tmux 作为进程容器。
2. 不把 `capture` 伪装成终端抓屏；ndjson 路径返回的是协议视角的消息快照。
3. 不为 ndjson 路径支持 TUI 导航键语义；除 `C-c` 映射为 interrupt 外，其他 `prompt --key` 不产生协议输入。
4. 不引入常驻 daemon。所有控制命令仍然是一次性 CLI。
5. 不把 `claude-code-ndjson` 泛化为所有 harness 的标准协议；它是 Claude Code 专用 transport。

## 3. 用户可见语义

### 3.1 保持不变的部分

以下命令入口保持不变：

```text
agentmux template list
agentmux list
agentmux summon
agentmux inspect
agentmux prompt
agentmux capture
agentmux wait
agentmux attach
agentmux halt
```

配置仍通过 `harness_type` 切换：

```yaml
templates:
  claude-code-ndjson:
    description: Claude Code 通用编程智能体（NDJSON 结构化模式）
    command: claude --dangerously-skip-permissions --model $MODEL
    model: anthropic/claude-sonnet-4.5
    harness_type: claude-code-ndjson
    system_prompt: ""
    prompt: ""
    cwd: .
```

### 3.2 有意变化的部分

ndjson 路径和 tmux 路径不是完全等价的 UI 自动化，它们只保持 agentmux 的高层工作流兼容：

| 命令 | tmux 路径 | ndjson 路径 |
|------|-----------|-------------|
| `summon` | 启动 TUI | 启动 detached Claude Code stream-json 进程 |
| `prompt --text` | 粘贴文本并回车 | 写入一行 user NDJSON |
| `prompt --key` | 发送终端按键 | `C-c` 映射为 interrupt，其余 TUI 导航键 no-op |
| `capture` | 返回屏幕文本 | 返回当前/最近 turn 的结构化消息，并保留 `content` |
| `wait` | 等 pane title 或屏幕稳定 | 等 prompt replay、result 和 session idle |
| `attach` | attach 到 tmux session | tail/follow `output.jsonl` |
| `halt` | C-c 后 kill tmux session | SIGTERM/SIGKILL Claude 进程组 |

## 4. 状态目录与文件

每个 ndjson 实例拥有一个独立目录：

```text
~/.local/state/agentmux/ndjson/<instance_session_id>/
├── input.fifo          # agentmux 写入，Claude stdin 读取
├── output.jsonl        # Claude stdout，append-only
├── stderr.log          # Claude stderr
├── state.json          # ndjson transport 状态
├── state.json.lock     # state.json 文件锁
├── process.json        # 进程元数据
└── command.log         # 启动命令与关键事件，便于诊断
```

说明：

1. `<instance_session_id>` 继续使用 agentmux 内部生成的 ASCII id，例如 `i_3f8ab2c1`。
2. `input.fifo` 是持久控制入口。每次 `prompt` 打开 FIFO，写入一行 JSON，关闭写端。
3. `output.jsonl` 只追加，不自动截断。长会话的清理交给后续 `gc` 或人工处理。
4. `state.json` 只保存 agentmux 需要快速读取的派生状态，不作为协议原始事实来源。
5. `process.json` 用来判断进程是否仍是 agentmux 启动的目标进程，避免误杀 PID 复用后的无关进程。

## 5. Registry 变更

在 `internal/instance.Instance` 增加 ndjson 相关字段：

```go
type Instance struct {
    // existing fields...

    ClaudeSessionID string `json:"claude_session_id,omitempty"`
    TransportDir    string `json:"transport_dir,omitempty"`
    ProcessID       int    `json:"process_id,omitempty"`
    ProcessGroupID  int    `json:"process_group_id,omitempty"`
}
```

字段语义：

1. `SessionID` 仍是 agentmux 的实例运行 id，不再表示 tmux session。
2. `ClaudeSessionID` 是传给 `claude --session-id` 的 UUID。
3. `TransportDir` 是实例状态目录。
4. `ProcessID` 是直接子进程 PID，通常是 shell 或 wrapper 进程。
5. `ProcessGroupID` 是独立进程组 id，`halt` 应优先对整个进程组发信号。

兼容性：

1. 旧 registry 中没有这些字段时按 tmux 路径处理。
2. 只有 `HarnessType == "claude-code-ndjson"` 时才解释这些字段。
3. `PaneTitle` 对 ndjson 路径无意义，保持为空。

## 6. state.json 结构

```json
{
  "version": 1,
  "claude_session_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "idle",
  "started_at": "2026-04-28T15:30:00+08:00",
  "last_prompt_at": "2026-04-28T15:35:00+08:00",
  "last_result_at": "2026-04-28T15:36:12+08:00",
  "last_event_at": "2026-04-28T15:36:12+08:00",
  "interrupted_at": null,
  "last_read_offset": 12345,
  "last_result_offset": 12001,
  "active_prompt_uuid": "",
  "last_completed_prompt_uuid": "8ccf6784-57b6-4b45-99f3-04ef7c6bdfb9",
  "pending_prompts": [],
  "session_idle": true,
  "resume_available": true,
  "total_turns": 5,
  "total_cost_usd": 1.23,
  "total_input_tokens": 125000,
  "total_output_tokens": 8900,
  "last_error": "",
  "pending_permission": null
}
```

字段说明：

| 字段 | 说明 |
|------|------|
| `version` | state 文件 schema 版本 |
| `claude_session_id` | Claude Code 会话 UUID |
| `status` | `starting` / `idle` / `busy` / `exited` / `lost` |
| `last_event_at` | agentmux 最近一次解析到协议事件的时间 |
| `interrupted_at` | 最近一次尚待状态收敛的 SIGINT 时间；收敛后清空 |
| `last_read_offset` | agentmux 已解析到的 output.jsonl 字节位置 |
| `last_result_offset` | 最近一次 result 事件起始 offset |
| `active_prompt_uuid` | 已 replay 但尚未完成的 prompt UUID |
| `last_completed_prompt_uuid` | 最近一次完成的 prompt UUID |
| `pending_prompts` | 已写入 FIFO、等待 replay/result/idle 的 prompt 队列 |
| `session_idle` | 最近一次 `system/session_state_changed` 是否报告 idle |
| `resume_available` | 是否已经有可被 Claude `--resume` 找到的持久 transcript |
| `total_turns` | 已看到的 result 数 |
| `total_cost_usd` | 从 result/usage 事件累计得到的费用 |
| `pending_permission` | 权限请求或控制请求的原始摘要；agentmux 记录并暴露，不自动批准 |

`pending_prompts` 元素结构：

```json
{
  "uuid": "8ccf6784-57b6-4b45-99f3-04ef7c6bdfb9",
  "sent_at": "2026-04-28T15:35:00+08:00",
  "start_offset": 10000,
  "replayed_offset": 10120,
  "result_offset": 12001,
  "state": "sent"
}
```

`state` 取值：

| state | 含义 |
|-------|------|
| `sent` | agentmux 已写入 FIFO，但尚未看到 Claude replay 该 user message |
| `replayed` | 已看到相同 uuid 的 `user` replay，说明 Claude 接收了该 prompt |
| `result` | 已看到该 prompt replay 之后的第一条 `result` |
| `idle` | 已看到该 prompt result 之后的 `system/session_state_changed idle` |
| `cancelled` | prompt 被中断或进程关闭 |
| `failed` | 进程退出、写入失败或 result 表示错误 |

并发规则：

1. `state.json` 必须用 `state.json.lock` 做进程间互斥。
2. 写入使用临时文件加 `os.Rename` 原子替换。
3. `registry` 和 `state.json` 同时更新时，先拿 registry lock，再拿 state lock，所有代码遵守同一顺序，避免死锁。
4. `output.jsonl` 是 append-only 原始日志，不加锁读取；解析器必须容忍最后一行尚未写完。

## 7. process.json 结构

```json
{
  "version": 1,
  "pid": 12345,
  "pgid": 12345,
  "started_at": "2026-04-28T15:30:00+08:00",
  "cwd": "/home/me/project",
  "command": "claude --dangerously-skip-permissions --model anthropic/claude-sonnet-4.5 -p --input-format stream-json --output-format stream-json --verbose --include-partial-messages --replay-user-messages --session-id 550e8400-e29b-41d4-a716-446655440000",
  "argv0": "/bin/bash",
  "fingerprint": "agentmux:i_3f8ab2c1:550e8400-e29b-41d4-a716-446655440000"
}
```

`process.json` 不作为安全边界，只用于降低 PID 复用误判风险。`reconcile` 和 `halt` 除检查 PID 存活外，还应尽量检查：

1. `/proc/<pid>/stat` 的 start time（Linux 可用）。
2. 进程组是否仍存在。
3. `output.jsonl` 是否仍在增长或 stderr 是否记录退出。

macOS 无 `/proc`，只做 PID/PGID 存活检查。

## 8. 新增包

新增 runtime 包：

```text
internal/ndjsonctl/
├── command.go       # Claude 命令构建与 flag 注入
├── process.go       # detached process 启动、存活检测、信号终止
├── fifo.go          # FIFO 创建、open/write timeout
├── messages.go      # Claude stream-json 事件结构
├── parser.go        # output.jsonl 增量解析与 capture 聚合
├── state.go         # state.json 读写与锁
├── snapshot.go      # 转换为 capture.Snapshot 兼容对象
└── errors.go        # ndjson 专用错误码
```

`service` 层不直接操作 FIFO、PID、JSONL，而是通过 `ndjsonctl.Controller`：

```go
type Controller interface {
    Start(ctx context.Context, StartInput) (StartResult, error)
    Reconcile(ctx context.Context, instance.Instance) (instance.Instance, error)
    SendPrompt(ctx context.Context, instance.Instance, string) (instance.Instance, error)
    Capture(ctx context.Context, instance.Instance, int) (capture.Snapshot, NDJSONCaptureData, error)
    Wait(ctx context.Context, instance.Instance, time.Duration) (capture.Snapshot, error)
    Attach(inst instance.Instance) *exec.Cmd
    Halt(ctx context.Context, instance.Instance, HaltOptions) error
}
```

## 9. 启动模型

### 9.1 为什么使用 detached process

agentmux 仍然是一次性 CLI，但 ndjson Claude 进程必须在 `agentmux summon` 退出后继续存活。因此 `Start` 必须启动一个脱离当前 agentmux 进程生命周期的子进程：

1. Unix: `exec.Command` + `SysProcAttr{Setsid: true}`。
2. stdin 连接到 `input.fifo`。
3. stdout append 到 `output.jsonl`。
4. stderr append 到 `stderr.log`。
5. `cmd.Start()` 后立即记录 PID/PGID，不调用 `Wait()` 阻塞 summon。

Windows 不是目标平台。

### 9.2 FIFO 与 stdin 持久化

直接让 Claude stdin 读取 FIFO 会遇到问题：agentmux 每次写完 prompt 关闭 FIFO 后，读端可能收到 EOF，导致 Claude 退出。

因此需要一个轻量 wrapper 进程负责保持 FIFO 到 Claude stdin 的连续流：

```text
agentmux prompt -> open/write/close input.fifo
                                |
                                v
wrapper keeps FIFO readable -> claude stdin
claude stdout -> output.jsonl
claude stderr -> stderr.log
```

实现方式使用 POSIX shell wrapper：

```sh
#!/bin/sh
set -eu

DIR="$1"
shift

FIFO="$DIR/input.fifo"
OUT="$DIR/output.jsonl"
ERR="$DIR/stderr.log"

mkdir -p "$DIR"
[ -p "$FIFO" ] || mkfifo "$FIFO"

# Keep one read-write fd open on the FIFO so transient writer disconnects do
# not deliver EOF to Claude. The actual stdin stream is still read from FIFO.
exec 3<>"$FIFO"

"$@" < "$FIFO" >> "$OUT" 2>> "$ERR"
```

实现可以不落盘 wrapper 文件，而是通过 `/bin/sh -c` 执行等价脚本。为了可诊断性，推荐把实际 wrapper 写入状态目录：

```text
~/.local/state/agentmux/ndjson/<session_id>/run.sh
```

注意：因为 wrapper 自己持有 FIFO 的读写 fd，不能通过关闭 FIFO 写端来优雅 EOF 停止 Claude。`halt` 必须通过进程组信号终止。

### 9.3 启动顺序

`summon` 新建 ndjson 实例：

1. 解析模板、cwd、name。
2. 生成 agentmux `SessionID`。
3. 生成 `ClaudeSessionID`，必须是 UUID v4。
4. 创建 `TransportDir`。
5. 创建 `input.fifo`、空 `output.jsonl`、空 `stderr.log`。
6. 构建 Claude 命令。
7. 启动 detached wrapper 进程，记录 `process.json`。
8. 短暂确认进程仍存活；不在 `summon` 阶段等待 `system/init`。
9. 初始化 `state.json`，状态置为 `idle`，但 `resume_available=false`。
10. 写入 registry。
11. 如果模板或 CLI 带初始 prompt，则通过 FIFO 发送 user 消息。
12. 后续 `prompt/wait/capture/reconcile` 解析到 `system/init` 后校验 `session_id`；真实 Claude Code 在 `stream-json` stdin 模式下可能要收到第一条 user message 后才输出 init。

如果第 8 步失败：

1. 若进程已退出，返回 `process_not_running`，错误消息包含 `stderr.log` 末尾摘要。
2. 清理 registry 中的半成品记录。
3. 保留 transport 目录用于诊断，不自动删除日志。

## 10. Claude 命令构建

### 10.1 基础原则

用户配置的 `command` 是 shell fragment，例如：

```yaml
command: claude --dangerously-skip-permissions --model $MODEL
```

agentmux 先执行现有变量替换：

1. `$MODEL`
2. `$CWD`
3. `$NAME`
4. `$TEMPLATE`

然后为 `claude-code-ndjson` 追加协议 flags。

### 10.2 自动追加 flags

必须确保最终命令包含：

```text
-p
--input-format stream-json
--output-format stream-json
--verbose
--include-partial-messages
--replay-user-messages
--session-id <uuid>
```

同时必须注入环境变量：

```text
CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1
```

这些选项的作用：

1. `--replay-user-messages` 让 stdout 中出现 agentmux 刚写入的 user message。agentmux 用 message uuid 做 prompt/result 归属，避免误吃背景任务或上一轮的 result。
2. `--include-partial-messages` 让 `stream_event` 中出现增量 token 和更及时的 usage 信息。
3. `CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1` 让 Claude 输出 `system/session_state_changed`，其中 `state=idle` 是 ndjson 路径最可靠的空闲信号。

system prompt：

1. 若 `system_prompt` 非空，默认追加 `--append-system-prompt <text>`。
2. 不使用 tmux 路径的 `[SYSTEM] ... [USER] ...` 文本拼接。
3. 若用户 command 已显式包含 `--system-prompt`、`--system-prompt-file`、`--append-system-prompt` 或 `--append-system-prompt-file`，则不自动追加，避免冲突。

权限：

1. 若 command 已包含 `--dangerously-skip-permissions` 或 `--permission-mode`，不追加权限 flag。
2. 若没有权限 flag，不默认追加 `--permission-prompt-tool`，因为该 flag 需要外部 MCP tool 名称，不能假设存在内建 `stdio`。
3. 文档推荐用户在 ndjson 模式下显式配置 `--dangerously-skip-permissions`、`--permission-mode acceptEdits` 或足够的 `--allowedTools`。

### 10.3 shell fragment 的边界

Go 标准库没有可靠的 shell fragment AST。为避免破坏已有 `command` 语义，本实现采用保守策略：

1. 不尝试解析并去重任意复杂 shell。
2. 只用 token scanner 识别简单未嵌套 token 是否包含关键 flags。
3. 若无法可靠判断，仍追加必要 flags。
4. 文档要求 ndjson 模板的 `command` 不使用管道、重定向、后台执行或多命令串联。

推荐配置：

```yaml
command: claude --dangerously-skip-permissions --model $MODEL
```

不推荐：

```yaml
command: FOO=1 claude "$@" 2>/tmp/claude.log | tee out.log
```

## 11. 输入消息

`prompt --text` 写入一行 NDJSON。每条 prompt 必须生成 UUID，并写入消息顶层 `uuid` 字段：

```json
{"type":"user","uuid":"8ccf6784-57b6-4b45-99f3-04ef7c6bdfb9","message":{"role":"user","content":[{"type":"text","text":"继续修复测试"}]}}
```

实现要求：

1. 用 `encoding/json` 构造消息，不手工拼接 JSON。
2. 每条消息后追加 `\n`。
3. 打开 FIFO 必须带超时，默认 5s。
4. 写入失败时执行一次 reconcile，若进程已死返回 `ndjson_fifo_broken` 或 `process_not_running`。
5. 发送前记录 `start_offset = output.jsonl 当前大小`。
6. 发送成功后把 `{uuid, sent_at, start_offset, state:"sent"}` 追加到 `state.pending_prompts`。
7. 如果之前没有 active prompt，则设置 `active_prompt_uuid` 为该 uuid。
8. 将 registry 和 state 标记为 `busy`，并设置 `session_idle=false`。

prompt 排队：

1. ndjson 路径允许在 `busy` 时继续 `prompt --text`，这与 claude-agent-acp 的 `promptQueueing` 能力一致。
2. 多个 prompt 通过 `pending_prompts` 队列排序，Claude 的 `--replay-user-messages` 负责确认每条 prompt 进入处理流。
3. `wait` 默认等待队列中截至调用时已经存在的全部 prompt 完成并进入 idle；这比只等“下一条 result”更符合 agentmux 上层编排习惯。
4. 如果用户希望只确认写入成功，`prompt` 命令本身已经在 FIFO write 成功后返回；完成检测交给 `wait`。

`prompt --stdin` 复用同一逻辑，只是 text 来自 stdin。

`prompt --key`：

1. `C-c` 映射为向 Claude 进程组发送 `SIGINT`，作为 best-effort interrupt。随后将所有未完成 prompt 标记为 `cancelled`；reconcile 优先服从后续协议事件，但若进程仍存活且连续 5 秒没有新事件，则兜底回到 `idle` 并保留 `last_error: "interrupted"`。
2. `Enter`、`Escape`、`Up`、`Down`、`Tab` 等 TUI 导航键在 ndjson harness 下不发送任何内容。
3. 对 no-op key 返回成功，并在 JSON data 增加：

```json
{
  "sent_text": false,
  "sent_key": "C-c",
  "noop": true,
  "noop_reason": "key input is not applicable for claude-code-ndjson"
}
```

如果同时有 text 和 key，则先发送 text；只有 key 是 `C-c` 时再执行 interrupt，其余 key 部分 no-op。调用方一般不应混用 text 和 `C-c`。

## 12. 输出事件解析

### 12.1 原始事件

`output.jsonl` 中每一行是 Claude Code stream-json 事件。解析器必须至少识别：

| type | 用途 |
|------|------|
| `system` / subtype `init` | 启动成功、session id、模型和工具元数据 |
| `system` / subtype `api_retry` | API 重试进度，记录为系统消息 |
| `system` / subtype `session_state_changed` | `state=idle` 表示会话真正空闲 |
| `system` / subtype `status` | compacting 等状态提示 |
| `system` / subtype `compact_boundary` | compact 完成边界，更新 usage/context 估计 |
| `system` / subtype `local_command_output` | slash/local command 输出 |
| `assistant` | assistant message，提取 text/thinking/tool_use |
| `user` | 由 `--replay-user-messages` 输出，用 uuid 确认 prompt 被接收 |
| `result` | 一轮执行结果，更新 usage/cost/turns；不单独等价于 session idle |
| `stream_event` | partial token 或底层 API event；保留 raw，并尽量提取 text delta |
| `tool_progress` / `tool_use_summary` | 工具进度和摘要，归一化为 tool message |
| `auth_status` / `rate_limit_event` / `prompt_suggestion` | 运行状态事件，保留 raw 摘要 |

未知事件不得导致整个 capture/wait 失败。解析器应保存 raw JSON，并在 debug/capture messages 中返回 `type: "unknown"` 摘要。

prompt 归属规则：

1. 解析到 `user` replay 且 `uuid` 命中 `pending_prompts` 时，将对应 prompt 从 `sent` 更新为 `replayed`，记录 `replayed_offset`。
2. 解析到 `result` 时，只归属给最早一个 `replayed` 但尚无 result 的 prompt；如果没有这样的 prompt，则视为 background result，只更新 usage/raw messages，不让当前 `wait` 提前返回。
3. 解析到 `system/session_state_changed state=idle` 时，将所有已 `result` 且未 `idle` 的 prompt 标记为 `idle`，设置 `session_idle=true`。
4. 如果看到 `state` 为非 idle，设置 `session_idle=false`，但不强行改变 prompt 队列状态。

### 12.2 增量解析

解析函数：

```go
func ReadEvents(path string, from int64, limit int) (events []Event, nextOffset int64, incomplete bool, err error)
```

要求：

1. `from` 必须是字节 offset。
2. 逐行读取，记录每行起始 offset。
3. 最后一行没有 `\n` 时视为 incomplete，暂不解析，`nextOffset` 不越过该行。
4. 单行 JSON 解析失败返回 `ndjson_parse_error`，但错误消息包含 offset，方便定位。
5. capture 可以选择跳过损坏行并返回 warning；wait 对损坏行应返回错误，避免误判完成。

## 13. Capture 语义

### 13.1 对外 JSON 兼容字段

`capture` JSON 继续返回现有字段：

```json
{
  "cursor_x": 0,
  "cursor_y": 0,
  "width": 0,
  "height": 0,
  "history_lines": 0,
  "pane_title": "",
  "content": "最终回复文本"
}
```

ndjson 路径下：

1. `cursor_x/cursor_y/width/height` 固定为 0。
2. `pane_title` 固定为空。
3. `history_lines` 原样返回传入 history 值，但含义见下。
4. `content` 是最近可用的 assistant/result 文本。

### 13.2 ndjson 扩展字段

JSON mode 下 `capture` 的 `data` 增加：

```json
{
  "messages": [],
  "usage": {
    "input_tokens": 0,
    "output_tokens": 0,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 0,
    "total_cost_usd": 0
  },
  "claude_session_id": "550e8400-e29b-41d4-a716-446655440000",
  "turns": 5,
  "raw_event_count": 42
}
```

文本 mode 仍只打印 `content`，不打印 JSONL 原文。

### 13.3 content 选择规则

按优先级选择：

1. 最近已完成 prompt 归属范围内的 `result.result` 字段非空，则使用它。
2. 否则使用该 prompt 范围内 assistant message 中的 text content 聚合。
3. 否则如果正在 busy，返回当前 turn 已出现的 assistant text 聚合。
4. 否则返回空字符串。

### 13.4 history 参数

tmux 路径中 `--history N` 表示屏幕历史行数。ndjson 路径中保留参数名，但含义调整：

1. `--history 0` 或未指定：返回 active prompt 的消息；若没有 active prompt，则返回最近 completed prompt 的消息。范围由 `pending_prompts[].start_offset/replayed_offset/result_offset` 和后续 idle 事件确定。
2. `--history N`：返回最近 N 条已归一化 messages，可跨 turn。
3. `history_lines` 字段仍填 N，表示调用方传入值，不表示实际终端行数。

## 14. Wait 语义

`wait` 等待调用时已入队的 prompt 全部完成，并等待 Claude 报告 session idle。

关键规则：

1. wait 开始时读取 `pending_prompts`，确定目标集合：所有 state 不是 `idle/cancelled/failed` 的 prompt。
2. 如果目标集合为空，且 `session_idle=true`，立即返回成功。
3. 否则从这些 prompt 的最小 `start_offset` 开始扫描，不能从文件末尾开始，否则会错过已经完成但尚未被 wait 观察到的 replay/result/idle。
4. 对每个目标 prompt，必须先看到同 uuid 的 user replay，再看到归属到该 prompt 的 result。
5. `system/session_state_changed state=idle` 是最终空闲信号。只有所有目标 prompt 已有 result 且 session idle，`wait` 才返回成功。
6. 若进程退出且目标 prompt 未完成，返回 `process_not_running`，状态置为 `exited`。
7. 超时返回 `capture_timeout`，保持状态为 `busy`。
8. `--stable` 在 ndjson 路径下忽略，返回 `stable_for_ms: 0`。
9. `--timeout` 保持原语义。

轮询间隔使用 `defaults.capture.poll_ms`，默认 250ms。实现上是文件 tail 语义，不依赖 tmux。

## 15. Inspect/List/Reconcile

### 15.1 存活检测

ndjson reconcile 不查 tmux。步骤：

1. 若 `ProcessID <= 0`，返回 `lost`。
2. 检查 PID 或 PGID 是否存在。
3. 若不存在，状态为 `exited`。
4. 若存在，解析 `output.jsonl` 中从 `state.last_read_offset` 开始的新事件。
5. 按 §12.1 的 prompt 归属规则更新 `pending_prompts`、usage、cost 和 session idle。
6. 若 `pending_prompts` 中仍有 `sent/replayed/result` 且未 idle 的 prompt，状态为 `busy`。
7. 若没有未完成 prompt 且 `session_idle=true`，状态为 `idle`。
8. 若没有未完成 prompt 但尚未收到 idle 事件，保持当前状态；不要仅凭 result 抢先置 idle。

### 15.2 inspect JSON

`inspect --json` 的 `data` 仍是 `Instance`，会天然包含新增字段：

```json
{
  "harness_type": "claude-code-ndjson",
  "claude_session_id": "...",
  "transport_dir": "...",
  "process_id": 12345,
  "process_group_id": 12345
}
```

可选增强：在 `app` 层增加 `runtime` 子对象不是本设计的必需项，避免破坏现有输出形状。

## 16. Halt 语义

ndjson 路径没有 TUI，也不能通过 FIFO EOF 可靠退出。`halt` 使用进程组信号：

1. 默认优雅停止：
   - 向 `-ProcessGroupID` 发送 `SIGTERM`。
   - 等待 `--timeout`，默认沿用现有 halt 默认值。
   - 若仍存活，发送 `SIGKILL`。
2. `--immediately`：
   - 直接向进程组发送 `SIGKILL`。
3. 停止成功后：
   - registry 删除实例。
   - `state.json` 标记 `exited`。
   - 删除 `input.fifo`。
   - 保留 `output.jsonl`、`stderr.log`、`state.json`、`process.json` 供审计。

如果进程已经不存在，`halt` 视为成功，并清理 registry。

## 17. Attach 语义

ndjson 路径没有可交互 UI。`attach <name>` 执行：

```text
tail -f ~/.local/state/agentmux/ndjson/<session_id>/output.jsonl
```

实现：

1. Linux/macOS 优先使用系统 `tail -f`。
2. 如果找不到 `tail`，agentmux 自己实现简单 follow reader。
3. 用户 Ctrl-C 只退出 attach，不影响 Claude 进程。

可以另行增加 `agentmux attach --pretty`，用内置 formatter 把 JSONL 渲染为人类可读事件流；这属于调试体验增强，不影响本 transport 的完整控制能力。

## 18. Resume 与会话恢复

### 18.1 同名复用

`summon` 遇到同名同模板实例：

1. reconcile。
2. 若状态为 `idle` 或 `busy`，复用现有进程。
3. 若提供 prompt，发送到现有 FIFO。
4. 若状态为 `exited` 或 `lost`，尝试恢复。

### 18.2 恢复策略

恢复时：

1. 读取 registry/state 中的 `ClaudeSessionID`。
2. 若 `state.resume_available=false`，直接返回 `process_not_running` 或专用恢复错误；不要盲目 `--resume`，因为 Claude 对“从未产生消息的 session”会报告找不到 conversation。
3. 生成新的 agentmux `SessionID` 和新的 `TransportDir`，或复用旧目录并追加日志。
4. 构建命令时使用 `--resume <claude_session_id>`，不再使用 `--session-id <uuid>`。
5. 启动新进程并短暂确认存活；`system/init` 仍由后续事件解析处理。
6. 更新 registry 中的 `SessionID`、`TransportDir`、`ProcessID`、`ProcessGroupID`。
7. 保留旧 output.jsonl，不自动合并；新目录中的日志从新的 process 生命周期开始。

推荐选择“新 TransportDir”，因为一个目录一个进程生命周期更容易诊断。`ClaudeSessionID` 负责跨进程对话连续性。

`resume_available` 更新规则：

1. `summon` 刚启动进程时为 false。
2. 第一次看到成功 replay 的 user message 或第一条 result 后置为 true。
3. 若 `--resume` 返回 “No conversation found with session ID” 等错误，将其置回 false，并向用户暴露恢复失败原因。

### 18.3 恢复失败

若 `--resume` 失败：

1. 返回错误，不自动创建新 Claude 会话。
2. 错误中包含 `stderr.log` 摘要。
3. 用户可通过新实例名或显式删除旧实例后重新创建全新会话。

## 19. 错误码

新增错误码：

| 错误码 | 场景 |
|--------|------|
| `ndjson_fifo_broken` | FIFO 打开或写入失败 |
| `ndjson_parse_error` | output.jsonl 存在不可解析事件 |
| `ndjson_process_error` | detached process 启动或信号控制失败 |
| `ndjson_state_error` | state.json 读写或加锁失败 |

复用现有错误码：

| 错误码 | 场景 |
|--------|------|
| `process_not_running` | 进程已退出或不可达 |
| `capture_timeout` | wait 超时 |
| `instance_not_found` | registry 无实例 |
| `instance_template_mismatch` | 同名不同模板 |
| `config_invalid` | ndjson command 不符合约束 |

## 20. 测试计划

### 20.1 单元测试

新增：

1. `internal/ndjsonctl/command_test.go`
   - flag 注入。
   - `--include-partial-messages`、`--replay-user-messages` 和 `CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1`。
   - system prompt flag 冲突处理。
   - resume/session-id 互斥。
2. `internal/ndjsonctl/parser_test.go`
   - system/init、assistant、tool_use、result、session_state_changed。
   - user replay uuid 与 pending prompt 归属。
   - background result 不应完成当前 wait。
   - incomplete trailing line。
   - unknown event。
   - parse error offset。
3. `internal/ndjsonctl/state_test.go`
   - lock + atomic write。
   - concurrent update。
4. `internal/ndjsonctl/fifo_test.go`
   - write timeout。
   - broken FIFO。
5. `internal/service/service_test.go`
   - ndjson harness dispatch。
   - prompt/capture/wait 不调用 tmux client。
   - reconcile PID dead -> exited。

### 20.2 集成测试

不依赖真实 Claude 的 fake harness：

```sh
fake-claude-ndjson \
  --input-format stream-json \
  --output-format stream-json \
  --replay-user-messages \
  --session-id <uuid>
```

fake 行为：

1. 可配置为启动时立即输出 `system/init`，或等第一条 user message 后输出，以覆盖真实 Claude 行为。
2. 每读到 user message，先按原 uuid replay `user` event。
3. 输出 assistant/stream_event、result、`system/session_state_changed idle`。
4. 可通过 env 控制延迟、background result、损坏 JSON、提前退出。

集成覆盖：

1. `summon --json` 创建进程。
2. `prompt --text` 写入 FIFO。
3. `wait` 从 turn start offset 观察 result。
4. `wait` 不被 background result 提前唤醒，必须等目标 prompt 的 replay/result/idle。
5. `capture --json` 返回 content/messages/usage。
6. `prompt --key C-c` 发送 interrupt 并取消未完成 prompt。
7. `halt` 终止进程组。
8. agentmux CLI 退出后 fake harness 仍存活。
9. 未发送过 prompt 的 session 不标记为 `resume_available`。

### 20.3 真实 smoke test

手动验证真实 Claude Code：

```text
agentmux summon --template claude-code-ndjson --name ndjson-smoke --prompt "回复 ok"
agentmux wait ndjson-smoke --timeout 60s --json
agentmux capture ndjson-smoke --json
agentmux prompt ndjson-smoke --text "再回复一行"
agentmux wait ndjson-smoke --timeout 60s --json
agentmux halt ndjson-smoke --json
```

验收点：

1. `output.jsonl` 在首个 prompt 后包含 `system/init` 和 `result`。
2. `capture.data.content` 是最终回复。
3. `wait` 不依赖 tmux。
4. `halt` 后进程组不存在。
5. stderr 无权限 prompt 卡死。

## 21. 实施顺序

推荐按以下 PR 拆分：

1. **数据模型与配置**
   - 新增 harness type 常量。
   - Instance 增加 ndjson 字段。
   - 默认配置增加 `claude-code-ndjson` 模板。
2. **ndjsonctl 基础**
   - state/process/fifo/command。
   - fake harness 测试夹具。
3. **启动与 halt**
   - detached process 启动。
   - 短暂存活检查；首 prompt 后解析 system/init。
   - 进程组终止。
4. **prompt/wait**
   - FIFO 写入。
   - result 等待。
   - state offset 正确维护。
5. **capture/parser**
   - messages 归一化。
   - usage/cost/content 聚合。
   - app JSON data 增强。
6. **reconcile/list/inspect/attach**
   - PID/PGID 存活检测。
   - `tail -f` attach。
7. **resume 与诊断**
   - `--resume` 恢复。
   - stderr 摘要。
   - command.log。
8. **文档与 smoke test**
   - README/config-spec/cli-spec 更新。
   - 真实 Claude Code 手动验证记录。

## 22. 关键实现注意事项

1. ndjson 路径绝不能调用 `tmuxctl`。
2. `wait` 必须从 turn start offset 或 last read offset 扫描，不能从 EOF 起等。
3. `state.json` 必须加锁；仅 registry 加锁不够。
4. FIFO EOF 不能作为优雅 halt 机制，因为 wrapper 会持有 FIFO fd。
5. 不要默认追加 `--permission-prompt-tool stdio`，除非真实验证 Claude Code 存在该内建工具。
6. `command` 是 shell fragment，flag 注入要保守，并在文档中明确限制。
7. 所有 JSONL 解析都要容忍未知事件，Claude Code 协议可能扩展。
8. `capture` 的文本 mode 必须保持只输出最终文本，避免把原始 JSONL 泄给上层普通消费者。
9. Linux/macOS 的进程检测差异要封装在 `ndjsonctl/process.go`，service 不写平台分支。
10. 真实 Claude Code smoke test 是必要验收项，fake harness 只能证明 agentmux 自身逻辑正确。

## 23. 待确认问题

以下问题需要实现前或实现中用真实 Claude Code 验证：

1. `claude -p --input-format stream-json` 在长期 stdin 流模式下，是否会在每个 user message 后持续等待下一行，而不是 result 后退出。
2. `--session-id <uuid>` 与 `--resume <id>` 在 `-p --input-format stream-json` 下的精确互斥/组合规则。
3. `result` 事件中的字段名是否稳定包含 `result`、`usage`、`total_cost_usd`、`session_id`。
4. 权限请求在 stream-json 下的真实事件形态；本设计记录 raw 并在 `inspect`/`capture --json` 暴露，不自动批准。
5. SIGTERM 是否能让 Claude Code 正常落盘 session；如果不能，是否需要先发送特定 control message 或改用更长 timeout。
