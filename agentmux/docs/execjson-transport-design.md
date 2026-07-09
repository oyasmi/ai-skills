# codex-cli-execjson Transport 设计文档

## 0. 定位

新增 `codex-cli-execjson` harness type，让 Codex CLI 具备与 `claude-code-ndjson` 同级的结构化控制能力：不读屏、不发按键、不依赖 tmux。

```text
agentmux prompt -> turns/<n>.prompt -> codex exec [resume <tid>] --json - -> output.jsonl
```

新增独立包 `internal/execjsonctl`。**`internal/ndjsonctl` 不做任何改动，继续只服务 Claude Code。两个包不共享代码。**

命名刻意区分：Claude 走的是 `--output-format stream-json` 的**双向 NDJSON 协议**（ndjson）；Codex 走的是 `codex exec --json` 的**单向事件流**（execjson）。名字不同是因为东西本来就不同。

---

## 1. 为什么不抽共享层

`codex exec` 和 `claude -p --input-format stream-json` 只是表面都叫「headless + JSON Lines」，底层模型是两回事：

| 维度 | `claude-code-ndjson` | `codex-cli-execjson` |
|---|---|---|
| 进程模型 | 1 实例 = 1 长驻进程 = N turns | 1 实例 = N 个短命进程，每 turn 一个 |
| 输入通道 | `input.fifo`，持续写 user NDJSON | `turns/<n>.prompt` 文件，重定向到 stdin |
| 多轮机制 | 同一进程内连续 turn | `codex exec resume <thread_id>` 重新拉起 |
| turn 归属 | `--replay-user-messages` 回显的 uuid | byte offset 区间（turn 串行，天然不重叠） |
| 完成信号 | `result` + `system/session_state_changed idle` | **进程退出** + `turn.completed`/`turn.failed` |
| 实例存活 | ≡ 进程存活 | 与进程无关（turn 之间没有进程） |
| 排队 | 支持（进程自带输入队列） | 不支持（见 §7.3） |
| 中断 | SIGINT，进程继续存活 | SIGINT，turn 进程直接结束 |
| 审批 | 事件里可见，可暴露 | 不存在（`exec` 非交互，权限只由 sandbox 决定） |
| 成本 | `result.total_cost_usd` | **没有** cost 字段 |

看似可共享的只有四小块：JSONL 按 offset 增量扫行、state 文件 flock + 原子写、`processAlive`/`killGroup`、`fileSize`/`tailFile`。加起来约 150 行，且**状态机、事件结构、归一化这些真正的复杂度全部不可共享**。

强行抽一层，收益是省 150 行样板，代价是：改 Claude 的 turn 归属逻辑时要先想清楚会不会崩到 Codex，反之亦然。两个协议都还在各自演进（Codex 的 `item.updated` 形状我们甚至还没完全观测到），此时把它们绑在一起是拿未来的迭代自由换眼下的行数。

**结论：各自实现。少量重复是正确的代价。** 若将来出现第三个 execjson 类 harness，届时再从两份实现里归纳共性，比现在从一份实现里预测共性靠谱。

---

## 2. 事实核验（codex-cli 0.142.0）

以下全部用真实 CLI 跑出来，不是照文档推断。这些是设计的地基。

### 2.1 `codex exec` 是单轮进程

没有 `--input-format`。stdin 只读一次 prompt 正文（`-` 或管道），读到 EOF 开始执行，turn 结束进程退出。多轮只能靠 `codex exec resume <SESSION_ID> [PROMPT]`。

**stdin 不能悬空**：留给 FIFO 或终端时，`codex exec` 会打印 `Reading additional input from stdin...` 并阻塞等 EOF。必须显式重定向到 prompt 文件。

### 2.2 `--ask-for-approval` 在 `codex exec` 上不存在

```
$ codex exec --ask-for-approval never ...
error: unexpected argument '--ask-for-approval' found
```

`-a/--ask-for-approval` 只属于交互式 `codex`。`exec` 是非交互模式，不会弹审批，权限完全由 `-s/--sandbox` 与 `--dangerously-bypass-approvals-and-sandbox` 决定。

推论：execjson 路径的 `pending_permission` 恒为 null。审批语义只存在于实验性的 `codex app-server`（JSON-RPC，含 `item/commandExecution/requestApproval` 等方法），不在 `exec` 里。

### 2.3 事件 schema：thread / turn / item

正常路径实测：

```jsonl
{"type":"thread.started","thread_id":"019f4659-1a62-70d0-b77d-dc2bf1464648"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"/bin/bash -lc 'cat sample.txt'","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"/bin/bash -lc 'cat sample.txt'","aggregated_output":"hello world\n","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"`sample.txt` contains:\n\n```text\nhello world\n```"}}
{"type":"turn.completed","usage":{"input_tokens":23294,"cached_input_tokens":20736,"output_tokens":104,"reasoning_output_tokens":0}}
```

失败路径（`-m` 传无效模型，进程 exit code 1）：

```jsonl
{"type":"thread.started","thread_id":"019f4656-854a-7de1-b4f7-0626a4e10e19"}
{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Model metadata for `...` not found. ..."}}
{"type":"turn.started"}
{"type":"error","message":"{\"type\":\"error\",\"status\":400,...}"}
{"type":"turn.failed","error":{"message":"{\"type\":\"error\",\"status\":400,...}"}}
```

完整枚举（从二进制符号表 dump，非猜测）：

| 顶层事件 | `item.type` |
|---|---|
| `thread.started` `turn.started` `turn.completed` `turn.failed` `item.started` `item.updated` `item.completed` `error` | `agent_message` `reasoning` `command_execution` `file_change` `mcp_tool_call` `web_search` `todo_list` `error` |

几个必须记住的细节：

1. **没有 `total_cost_usd`。** usage 字段名也和 Claude 不同：`cached_input_tokens`（非 `cache_read_input_tokens`）、多出 `reasoning_output_tokens`、没有 `cache_creation_input_tokens`。
2. **`item.id` 每个 turn 从 `item_0` 重新开始**，跨 turn 不唯一。去重必须限定在 turn 的 offset 区间内。
3. `thread.started` 在 `resume` 时也会重新发出，且 `thread_id` 与原会话一致（已实测）。
4. `turn.failed` 时进程 exit code 为 1。

### 2.4 flag 位置有硬约束

`resume` 是子命令，只接受父级 flag 的一个子集：

```
$ codex exec resume <id> --sandbox read-only ...
error: unexpected argument '--sandbox' found
```

放到 `resume` 之前则可用。划分：

| 位置 | flag |
|---|---|
| 父级（必须在 `resume` 之前） | `-s/--sandbox` `-C/--cd` `--add-dir` `--color` `--skip-git-repo-check` |
| 子命令级（`resume` 之后） | `--json` `-o/--output-last-message` `--output-schema` `-m/--model` |

因此拼装必须是 `<用户前缀> resume <tid> --json -`，不能把 flag 简单追加到尾部。

### 2.5 prompt 走 stdin

`codex exec [PROMPT]` 与 `codex exec resume <ID> [PROMPT]` 都支持 `-` 表示从 stdin 读正文（两条路径均已实测）。

一律用 `- < turns/<n>.prompt`：绕开 ARG_MAX、shell 引用、多行与特殊字符。

### 2.6 npm 版 codex 是 node wrapper

`bin/codex.js` spawn 真正的 rust 二进制。所以 kill 必须打**进程组**，不能只打 pid。

---

## 3. 非目标

1. 不使用 `codex app-server` / `exec-server`（实验性、JSON-RPC、协议面大）。演进路径见 §12。
2. 不实现 prompt 排队（§7.3）。
3. 不支持审批交互（`exec` 模式不存在审批）。
4. 不支持 TUI 导航键；仅 `C-c` 映射为 interrupt。
5. 不引入常驻 daemon。所有控制命令仍是一次性 CLI。
6. 不改动 `internal/ndjsonctl`。

---

## 4. 用户可见语义

```yaml
templates:
  codex-cli-execjson:
    description: Codex CLI 通用编程智能体（execjson 结构化模式）
    command: codex exec --sandbox workspace-write --skip-git-repo-check --model $MODEL
    model: gpt-5.1-codex
    harness_type: codex-cli-execjson
    system_prompt: ""
    prompt: ""
    cwd: .
```

`command` 是**前缀片段**，必须以 `codex exec` 开头，只含父级 flag。agentmux 负责补 `resume <tid>` / `--json` / `-`。

### 4.1 三条路径对照

| 命令 | tmux (`codex-cli`) | `claude-code-ndjson` | `codex-cli-execjson` |
|------|-----------|------|------|
| `summon` | 启动 TUI | 启动长驻进程 | **不启动任何进程**，仅建目录与注册表 |
| `prompt --text` | 粘贴 + 回车 | 写一行 NDJSON 到 FIFO | 落盘 prompt 文件，spawn 一个 turn 进程 |
| `prompt`（busy 时） | 排到 TUI 输入框 | 入队 | 返回 `execjson_instance_busy` |
| `prompt --key C-c` | 发按键 | SIGINT，进程存活 | SIGINT，turn 进程结束 |
| `capture` | 屏幕文本 | 协议消息快照 | 协议消息快照 |
| `wait` | pane_title 启发式 | replay + result + idle | **turn 进程退出 + 终局事件** |
| `attach` | attach tmux | `tail -f output.jsonl` | `tail -f output.jsonl` |
| `halt` | C-c + kill session | SIGTERM 进程组 | kill 在跑的 turn（若有）+ 删注册表 |

### 4.2 最关键的语义分歧：实例存活 ≠ 进程存活

`claude-code-ndjson` 的实例就是那个长驻进程，进程死了实例就 `exited`。

`codex-cli-execjson` 的实例是「一个 `thread_id` + 一个 transport 目录」。**turn 之间根本没有进程存在。**

因此 execjson 的 reconcile **绝不能**因为 `!processAlive(pid)` 就判 `exited`——那会让每个空闲实例在下一次 `list` 时被误删。状态定义：

| 状态 | 判据 |
|------|------|
| `busy` | 当前 turn 的进程仍存活 |
| `idle` | 没有在跑的 turn（含从未 prompt、上一 turn 成功、上一 turn 失败或被中断） |
| `exited` | 仅由 `halt` 显式产生 |
| `lost` | transport 目录或 `state.json` 丢失 |

`turn.failed` 不让实例进终态：错误写进 `state.last_error`，实例回 `idle`，用户可以继续 `prompt` 走 resume 重试。

---

## 5. 状态目录

```text
~/.local/state/agentmux/execjson/<instance_session_id>/
├── output.jsonl        # 所有 turn 的事件流，append-only
├── stderr.log          # 所有 turn 的 stderr
├── state.json          # + state.json.lock
├── process.json        # 当前/最近一个 turn 的进程元数据
├── command.log         # 每个 turn 的启动命令
└── turns/
    ├── 000.prompt      # prompt 正文（stdin 源）
    ├── 000.run.sh
    ├── 001.prompt
    └── 001.run.sh
```

没有 `input.fifo`，没有常驻 wrapper。目录名用 `execjson/`，与 `ndjson/` 并列互不干扰。

`output.jsonl` 由多个短命进程以 `O_APPEND` 追加。turn 严格串行（§7.3 保证），不会交错。

---

## 6. Registry 变更

Codex 需要持久化 `thread_id`。**不复用也不改名 `ClaudeSessionID`**——那是 Claude 的字段，让 Codex 借住会逼出一次全局迁移，且两个 harness 从此互相牵制。直接加自己的字段：

```go
type Instance struct {
    // existing fields...

    ClaudeSessionID string `json:"claude_session_id,omitempty"` // claude-code-ndjson 专用，不动
    ThreadID        string `json:"thread_id,omitempty"`         // codex-cli-execjson 专用
    TransportDir    string `json:"transport_dir,omitempty"`     // 两者共用（只是一个路径）
    ProcessID       int    `json:"process_id,omitempty"`        // execjson: 当前 turn 的 pid，无 turn 时为 0
    ProcessGroupID  int    `json:"process_group_id,omitempty"`
}
```

零迁移、零兼容风险。代价是 registry 里多一个可空字段，可以接受。

`ProcessID` 在 execjson 下语义变了：它是**当前 turn** 的进程，没有 running turn 时为 0。这正是 §4.2 的直接体现。

---

## 7. state.json

```json
{
  "version": 1,
  "thread_id": "019f4659-1a62-70d0-b77d-dc2bf1464648",
  "status": "idle",
  "started_at": "2026-07-09T10:00:00Z",
  "last_read_offset": 812,
  "resume_available": true,
  "turns": [
    {
      "index": 0,
      "started_at": "2026-07-09T10:00:01Z",
      "ended_at": "2026-07-09T10:00:09Z",
      "start_offset": 0,
      "end_offset": 812,
      "pid": 41207,
      "pgid": 41207,
      "state": "completed",
      "exit_code": 0,
      "error": ""
    }
  ],
  "total_turns": 1,
  "total_input_tokens": 23294,
  "total_output_tokens": 104,
  "total_cached_input_tokens": 20736,
  "total_reasoning_output_tokens": 0,
  "last_error": ""
}
```

`turns[].state`：

| state | 含义 |
|-------|------|
| `running` | 进程已 spawn 且存活，未见终局事件 |
| `completed` | 见到 `turn.completed` |
| `failed` | 见到 `turn.failed`，或进程非 0 退出且无终局事件 |
| `cancelled` | 被 `C-c` 或 `halt` 中断 |

`resume_available`：首次解析到 `thread.started` 时置 true。

并发规则与 ndjson 路径相同（各自实现，不共享代码）：flock + 临时文件 `os.Rename` 原子替换；registry lock 永远先于 state lock；`output.jsonl` 不加锁读，解析器容忍未写完的尾行。

---

## 8. 代码结构

### 8.1 新增包

```text
internal/execjsonctl/
├── command.go     # 前缀校验 + resume/--json/- 拼装 + run.sh 生成
├── controller.go  # Start/Reconcile/SendPrompt/Capture/Wait/Interrupt/Halt/Attach
├── turn.go        # per-turn 进程 spawn、存活检测、收尾
├── messages.go    # thread/turn/item 事件结构 + NormalizedMessage
├── parser.go      # 增量解析 + turn 归属 + item 去重 + 归一化 + usage 聚合
├── state.go       # State / Turn / 状态机 / flock 原子写
└── files.go       # fileSize / tailFile / sleepPoll
```

`state.go` 与 `files.go` 里会有约 150 行与 `ndjsonctl` 形似的代码。这是 §1 里明确接受的重复。

### 8.2 service 层：dispatch 而非共享实现

现在 `service.go` 里散落 7 处 `if s.isNDJSON(inst)`。加第二个结构化 harness 会退化成 `if claude {} else if codex {} else {tmux}`，不可维护。

引入一个**纯 dispatch 接口**——它不共享任何实现，只是让 service 别写三分支：

```go
// internal/service/harness.go
type harness interface {
    Start(context.Context, StartInput) (instance.Instance, error)
    Reconcile(context.Context, instance.Instance) (instance.Instance, error)
    SendPrompt(context.Context, instance.Instance, string) (instance.Instance, error)
    Capture(context.Context, instance.Instance, int) (capture.Snapshot, error)
    Wait(context.Context, instance.Instance, time.Duration) (capture.Snapshot, error)
    Interrupt(context.Context, instance.Instance) (instance.Instance, error)
    Halt(context.Context, instance.Instance, HaltOptions) error
    Attach(instance.Instance) *exec.Cmd
    CanResume(instance.Instance) bool
}

func (s Service) harnessFor(inst instance.Instance) (harness, bool) {
    switch inst.HarnessType {
    case ndjsonctl.HarnessType:   return s.NDJSON, true
    case execjsonctl.HarnessType: return s.Codex, true
    default:                      return nil, false // tmux 路径
    }
}
```

对 `ndjsonctl` 的改动仅限于签名，不触碰其协议逻辑：

1. `Start` 的入参从 `StartInput` 摊平为 `(inst, command, systemPrompt, resume)`，返回从 `StartResult` 改为 `instance.Instance`（两者都只是单字段的壳，已删除）。Claude session id 改从 `inst.ClaudeSessionID` 读取。
2. `Halt` 的 `HaltOptions` 摊平为 `(immediately, timeout)`。
3. 新增 `CanResume`：`ClaudeSessionID` 非空且 `state.resume_available` 为真。

`resumeNDJSON` 泛化为 `resumeStructured`，用 `CanResume(inst)` 取代硬编码的 `next.ClaudeSessionID != ""`。

这样 `Instance` 上 `ClaudeSessionID` 与 `ThreadID` 各归其主，两个 controller 之间没有任何共享实现。

---

## 9. 命令构建

### 9.1 前缀校验（summon 时，失败即 `config_invalid`）

1. 首两个 token 是 `codex exec`（允许绝对路径，如 `/usr/bin/codex exec`）。
2. 不含 `resume` / `review` 子命令。
3. 不含 `--json` / `-o` / `--output-last-message`（由 agentmux 注入与管理）。
4. 不含 `--ask-for-approval` / `-a`——`codex exec` 直接报错退出（§2.2），早失败早止损。
5. 不含 `--ephemeral`——不落盘 session 则 resume 永久不可用，多轮直接废掉。
6. 不含管道 / 重定向 / `&&` / 后台符号（保守 token scanner，不解析完整 shell AST）。

变量替换沿用 `$MODEL` / `$CWD` / `$INSTANCE` / `$TEMPLATE`。

### 9.2 拼装

turn 0：

```sh
exec codex exec --sandbox workspace-write --skip-git-repo-check --model gpt-5.1-codex \
     --json - < "$DIR/turns/000.prompt" >> "$DIR/output.jsonl" 2>> "$DIR/stderr.log"
```

turn N（N ≥ 1）：

```sh
exec codex exec --sandbox workspace-write --skip-git-repo-check --model gpt-5.1-codex \
     resume '019f4659-...' --json - < "$DIR/turns/00N.prompt" >> "$DIR/output.jsonl" 2>> "$DIR/stderr.log"
```

`resume` 插在用户前缀之后、`--json` 之前——§2.4 的硬约束。

`exec` 让 sh 被替换，`cmd.Process.Pid` 直接指向 codex 启动器。仍需 `Setsid: true` 建独立进程组，因为 node wrapper 会再 fork rust 二进制（§2.6）。

### 9.3 system_prompt

`codex exec` 没有 `--append-system-prompt`。首个 turn 的 prompt 文件写成与 tmux 路径一致的形状，保持跨 harness 行为可预期：

```text
[SYSTEM]
<system_prompt>

[USER]
<prompt>
```

只在 `FirstPromptSent == false` 时注入。不碰 `-c experimental_instructions_file`（未文档化，易碎）。

---

## 10. 生命周期

### 10.1 Start（summon）

不 spawn 任何东西：

1. 校验 command 前缀（§9.1）。
2. 建 transport 目录 + `turns/`，建空 `output.jsonl` / `stderr.log`。
3. 写 `state.json`：`status=idle`、`thread_id=""`、`resume_available=false`。
4. 写 registry，`ProcessID=0`。
5. 若模板或 CLI 带初始 prompt，走 §10.2 起 turn 0。

`summon` 因此即时返回，也不存在「进程启动即死」的失败模式。

### 10.2 SendPrompt（起一个 turn）

1. state lock 内检查：存在 `running` turn → `execjson_instance_busy`。
2. `n = len(turns)`；写 `turns/<n>.prompt`（首轮按 §9.3 拼 system prompt）。
3. `start_offset = fileSize(output.jsonl)`（spawn 前记录）。
4. 生成 `turns/<n>.run.sh`：`thread_id == ""` 用 turn-0 形态，否则用 resume 形态。
5. detached spawn（`Setsid: true`，stdin 在 run.sh 内重定向到 prompt 文件），记录 pid/pgid → `process.json` + `command.log`。
6. 追加 `turns[n] = {state: running, pid, pgid, start_offset}`，`status = busy`，registry 的 `ProcessID/ProcessGroupID` 同步更新。
7. 立即返回。完成检测交给 `wait`。

### 10.3 为什么不做 prompt 排队

Claude 能排队，是因为长驻进程自带输入队列。Codex 每个 turn 是独立进程，且 `resume` 需要上一个 turn 的 session 已落盘——并发 spawn 两个 `resume` 会撞同一个 session 文件。

而 agentmux 是一次性 CLI，没有 daemon 去「等上一 turn 结束再起下一 turn」。硬做排队就得引入后台 drainer，违背 §3.5。

所以 busy 时 `prompt` 直接返回 `execjson_instance_busy`。上层编排本来就该 `prompt` → `wait` → `prompt`。这是诚实的失败，不是缺陷。

### 10.4 Reconcile

```
transport 目录或 state.json 缺失         -> lost
存在 running turn 且 processAlive(pid)  -> busy（顺带增量解析新事件）
存在 running turn 但进程已死            -> 收尾(§10.5)，再判 idle
无 running turn                         -> idle
```

**不查 tmux；不因 pid 死而判 exited。**

### 10.5 turn 收尾

进程已死后，从 `turn.start_offset` 增量解析：

1. 见 `turn.completed` → `state=completed`，累加 usage。
2. 见 `turn.failed` → `state=failed`，`last_error = error.message`。
3. 两者都没见（崩溃 / OOM / 被 kill）→ `state=failed`，`last_error` 取 `stderr.log` 尾部摘要。
4. 该 turn 已被 `C-c` 标记 → `state=cancelled`。
5. `end_offset` = 解析到的最后一行 `EndOffset`；见过 `thread.started` 则 `resume_available=true`。
6. `thread_id` 为空时从 `thread.started` 填入；非空时校验一致，不一致记 `last_error` 但不中断。
7. registry 的 `ProcessID/ProcessGroupID` 归零。

### 10.6 Wait

```
1. reconcile 一次。
2. 无 running turn -> 立即成功返回。
3. 轮询（defaults.capture.poll_ms，默认 250ms）：
     processAlive(turn.pid) ? 继续 : 收尾(§10.5) -> 返回
4. 超时 -> capture_timeout，状态保持 busy。
```

`wait` 成功不代表 turn 成功。`turn.failed` 也是「等到了」。失败通过 `capture --json` 的 `data.last_error` 暴露，`wait` 退出码不变。这与 ndjson 路径一致（`result.is_error` 同样不让 `wait` 报错）。

`--stable` 忽略，返回 `stable_for_ms: 0`。

### 10.7 Interrupt（`prompt --key C-c`）

对 running turn 的 pgid 发 `SIGINT`，标记 `cancelled`，等进程退出（最长 halt timeout）后走收尾。没有 running turn 时是 no-op：

```json
{"sent_text": false, "sent_key": "C-c", "noop": true, "noop_reason": "no running turn"}
```

其余导航键一律 no-op，与 ndjson 路径一致。

### 10.8 Halt

1. 有 running turn：SIGTERM 进程组 → 等 timeout → SIGKILL；`--immediately` 直接 SIGKILL。标记 `cancelled`。
2. `state.status = exited`。
3. registry 删除实例。
4. 保留 `output.jsonl` / `stderr.log` / `state.json` / `turns/` 供审计。

没有 running turn 时，halt 就是「删注册表 + 标记 exited」，必定成功。

### 10.9 Resume

execjson 实例不会自然进入 `exited`/`lost`，所以「进程死了去 resume」这条路基本用不上。同名 summon 的实际路径是：实例还在 → 复用 → 有 prompt 就起新 turn（自动带 `resume <thread_id>`）。

只有 registry 丢了而 transport 目录还在时，才需要从 `state.json` 读回 `thread_id` 重建实例。

`resume_available == false`（从未产生过 `thread.started`）时不要盲目 `resume`，直接当新会话起 turn 0。

---

## 11. 事件解析与归一化

### 11.1 turn 归属

不需要 uuid。turn `n` 的事件 = `output.jsonl` 中 `[start_offset, next_turn.start_offset)` 区间的所有行。turn 串行保证区间不重叠。

### 11.2 item 去重

`item.started` → `item.updated`* → `item.completed` 描述同一个 item。**`item.id` 只在 turn 内唯一**（每 turn 从 `item_0` 重新计数）。

归一化时按 `(turn_index, item.id)` 建索引，last-write-wins，保留首次出现顺序。turn 仍在跑时，只有 `item.started` 的 item 也要输出，这样 `capture` 能看到正在执行的命令。

### 11.3 事件 → NormalizedMessage

`execjsonctl` 定义自己的 `NormalizedMessage`（形状与 ndjsonctl 的巧合相似，但不共享类型）：

| codex 事件 | NormalizedMessage |
|---|---|
| `item.*` / `agent_message` | `{type:"assistant", role:"assistant", content_type:"text", text}` |
| `item.*` / `reasoning` | `{type:"assistant", role:"assistant", content_type:"thinking", text}` |
| `item.*` / `command_execution` | `{type:"tool_use", tool:"shell", input:{command,exit_code,status}, raw}` |
| `item.*` / `file_change` | `{type:"tool_use", tool:"file_change", raw}` |
| `item.*` / `mcp_tool_call` | `{type:"tool_use", tool:"<server>/<name>", raw}` |
| `item.*` / `web_search` | `{type:"tool_use", tool:"web_search", raw}` |
| `item.*` / `todo_list` | `{type:"system", content_type:"todo_list", raw}` |
| `item.*` / `error` | `{type:"system", content_type:"error", text:message, raw}` |
| `error`（顶层） | `{type:"system", content_type:"error", text:message, raw}` |
| `thread.started` | `{type:"system", content_type:"thread_started", text:thread_id}` |
| `turn.started` | `{type:"system", content_type:"turn_started"}` |
| `turn.completed` | `{type:"result", raw}` |
| `turn.failed` | `{type:"result", text:error.message, raw}` |
| 未知 type | `{type:"unknown", raw}` — 绝不报错 |

`command_execution` 的 `aggregated_output` 可能是几十 MB 的编译日志。归一化时按 `defaults.capture` 上限截断（默认 8KB），原文仍留在 `raw` 与 `output.jsonl` 里。

### 11.4 content 选择

1. 当前范围内最后一条 `agent_message` 的 `text`。
2. 否则 `turn.failed.error.message`。
3. 否则空串。

Codex 没有 Claude 那种 `result.result` 汇总字段，也没有 partial token 流可拼，所以规则比 ndjson 短。

### 11.5 usage

`turn.completed.usage` 逐 turn 累加，映射到 agentmux 的 `data.usage`：

| codex | agentmux |
|---|---|
| `input_tokens` | `input_tokens` |
| `output_tokens` | `output_tokens` |
| `cached_input_tokens` | `cache_read_input_tokens` |
| — | `cache_creation_input_tokens` = 0 |
| `reasoning_output_tokens` | `reasoning_output_tokens`（codex 独有，附加字段） |
| — | `total_cost_usd` = 0（codex 不提供） |

保留 `total_cost_usd: 0` 而非省略，让下游 schema 在两个 harness 间保持稳定形状。

### 11.6 capture 输出

兼容字段照旧（`cursor_x/y`、`width/height` 为 0，`pane_title` 为空）。`data` 扩展：

```json
{
  "messages": [],
  "usage": { "...": 0 },
  "thread_id": "019f4659-...",
  "turns": 2,
  "turn_state": "completed",
  "last_error": "",
  "raw_event_count": 12
}
```

`--history 0` 或未指定 → 当前（或最近一个）turn 的消息；`--history N` → 最近 N 条归一化消息，可跨 turn。文本模式仍只打 `content`，不泄露 JSONL 原文。

---

## 12. 错误码

execjson 用自己的前缀，不复用 `ndjson_*`：

| 错误码 | 场景 |
|--------|------|
| `execjson_process_error` | detached spawn 或信号控制失败 |
| `execjson_parse_error` | `output.jsonl` 存在不可解析事件（错误消息含 offset） |
| `execjson_state_error` | `state.json` 读写或加锁失败 |
| `execjson_instance_busy` | 已有 running turn 时再次 `prompt` |

复用现有：`process_not_running`（halt 后再操作）、`capture_timeout`、`instance_not_found`、`instance_template_mismatch`、`config_invalid`（前缀校验失败）。

`capture` 对损坏行可跳过并返回 warning；`wait` 遇损坏行必须报错，避免误判完成。

---

## 13. 测试计划

### 13.1 单元

1. `execjsonctl/command_test.go`：§9.1 六条校验规则各一例；turn-0 与 resume 两种拼装；**`resume` 位置在用户 flag 之后、`--json` 之前**；`$MODEL` 展开；`--ephemeral` 与 `--ask-for-approval` 被拒。
2. `execjsonctl/parser_test.go`：四类顶层事件 + 八类 item；`item.started`→`updated`→`completed` 去重；**跨 turn 的 `item_0` 不互相覆盖**；`turn.failed` 归一化；未知事件不报错；未写完的尾行；usage 累加与字段改名；`aggregated_output` 截断。
3. `execjsonctl/state_test.go`：turn 状态机 `running`→`completed`/`failed`/`cancelled`；进程死但无终局事件 → `failed` + stderr 摘要；flock + 原子写；并发 update。
4. `service/harness_test.go`：dispatch 到正确 controller；execjson 路径不触碰 tmux client；**pid 为 0 且无 running turn → `idle` 而非 `exited`**（§4.2 的回归护栏，这是本设计最容易被后续改动破坏的不变式）。

### 13.2 集成（fake harness，不烧 token）

`fake-codex-execjson` 脚本，行为可用 env 调：读 stdin 当 prompt → 吐 `thread.started`（`resume` 时回显传入 id）→ `turn.started` → `item.*` → `turn.completed` 或 `turn.failed` → 按 exit code 退出。

覆盖：

1. `summon` 不产生进程（`ProcessID == 0`，`status == idle`）。
2. `prompt` spawn turn 0；`wait` 等到进程退出。
3. 第二次 `prompt` 生成的 run.sh 含 `resume <thread_id>`。
4. busy 时 `prompt` → `execjson_instance_busy`。
5. turn 崩溃（无终局事件）→ `failed` + stderr 摘要，实例回 `idle` 而非 `exited`。
6. `C-c` 取消 running turn。
7. `halt` 在无 running turn 时也成功。
8. agentmux CLI 退出后 turn 进程仍存活。
9. `--ephemeral` 模板被 `config_invalid` 拒绝。
10. `list` 反复执行不会误删空闲 execjson 实例。

### 13.3 真实 smoke（必须做；fake 只能证明 agentmux 自身逻辑正确）

```bash
agentmux summon  --template codex-cli-execjson --name codex-smoke
agentmux prompt  codex-smoke --text "回复 ok"
agentmux wait    codex-smoke --timeout 120s --json
agentmux capture codex-smoke --json                  # data.content == "ok"，thread_id 非空
agentmux prompt  codex-smoke --text "你刚才说了什么"    # 应走 resume
agentmux wait    codex-smoke --timeout 120s --json
agentmux list                                        # 仍在，status=idle
agentmux halt    codex-smoke --json
```

验收点：

1. `output.jsonl` 含两段 `thread.started`，`thread_id` 相同。
2. `turns/001.run.sh` 含 `resume`。
3. 两个 turn 之间 `list` 显示 `idle` 且实例未被删除。
4. `halt` 后进程组不存在。
5. stderr 无 `Reading additional input from stdin...`（证明 stdin 已正确重定向）。

---

## 14. 实施顺序

| PR | 内容 | 风险 |
|----|------|------|
| 1 | `service.harness` dispatch 接口；`ndjsonctl.Controller.Start` 签名调整 + `CanResume` | 低（现有 ndjson 测试须原样绿） |
| 2 | `Instance.ThreadID` 字段；`config` 新增 `codex-cli-execjson` 模板与前缀校验 | 低（纯新增，无迁移） |
| 3 | `execjsonctl`：`command.go` + `state.go` + `messages.go` + `parser.go`（纯函数，先测透） | 低 |
| 4 | `execjsonctl`：`turn.go` + `controller.go` 的 Start / SendPrompt / Reconcile / Wait | 中 |
| 5 | `execjsonctl`：Interrupt / Halt / Attach | 低 |
| 6 | capture 归一化 + app JSON `data` 扩展 | 低 |
| 7 | fake harness + 集成测试（含 §13.2.10 回归护栏） | 低 |
| 8 | 文档（README / cli-spec / config-spec）+ 真实 smoke 记录 | — |

PR 1–2 是给 execjson 腾地方，本身不引入新功能，可以先合。

---

## 15. 演进：什么时候该换到 app-server

`codex exec` 换来简单，代价是三件事做不了：

1. **prompt 排队**（§10.3）。
2. **turn 内中断后继续**——SIGINT 直接结束进程。
3. **审批交互**——`item/commandExecution/requestApproval` 只存在于 app-server。

若将来需要其中任意一项，应新增第三个 harness type `codex-app-server`（JSON-RPC over stdio，长驻进程，形态反而更接近 `claude-code-ndjson`），而不是把 `codex-cli-execjson` 改造成它。两者可以共存：exec 路径简单可靠，适合批量编排；app-server 路径功能全，适合交互式代理。

届时如果 `codex-app-server` 与 `claude-code-ndjson` 真的长出共同形状，再从两份实现里归纳共性——比现在预测共性靠谱。

---

## 16. 实现状态

已实现并通过真实 Codex CLI（0.142.0）smoke test。验收记录：

| 验收点 | 结果 |
|--------|------|
| `summon` 不产生进程 | `status=idle`，`process_id` 缺省（0） |
| 两个 turn 之间 `list` 不误删实例 | `codex-smoke idle pid=0`，实例存活 |
| turn 0 → `wait` → `capture` | `content="alpha"`，`turn_state=completed` |
| turn 1 走 resume | `turns/001.run.sh` 含 `resume '019f46a1-…'` |
| 跨进程会话连续性 | 第二轮答出上一轮的 "alpha"；两段 `thread.started` 同 id |
| busy 时 `prompt` | `execjson_instance_busy`，退出码 1 |
| `--key C-c` 中断 | turn 标记 `cancelled`，实例回 `idle` |
| `halt` | `status=exited`，进程组无残留 |
| stdin 重定向正确 | stderr 无 `Reading additional input from stdin...` |
| 无效模型（`turn.failed`） | turn 标记 `failed`，`last_error` 可见，实例仍 `idle` 且可继续 prompt |
| `--sandbox workspace-write` 真实工作 | agent 成功写出 `proof.txt`，`capture` 中出现 `tool_use` 消息 |
| 5 个 turn 的会话连续性 | `output.jsonl` 中 5 条 `thread.started` 同一 id；`turns/000` 无 resume，其余全部 `resume '<tid>'` |
| 被 `C-c` 取消的 turn 不污染线程 | turn 3 `cancelled` 之后，turn 4 正常 `completed` |
| 前缀校验 | `--ask-for-approval` / `--ephemeral` / `--json` / 缺 `exec` / 管道 均在 summon 阶段被 `config_invalid` 拒绝 |

53 条断言，全部通过（一次失败是 smoke 脚本自身把 `jq` 多行输出当行数计，非产品问题，已修）。

两条原「待确认」由本次验证结清：

1. **SIGINT 经 npm 的 node wrapper 转发给 rust 子进程是可靠的。** 进程组信号，实测 `C-c` 与 `halt` 之后均无残留进程。
2. **`thread.started` 已产生但 turn 失败的 session 可以 resume。** 实测对一个仅有 `turn.failed` 的 thread 执行 `codex exec resume`，成功恢复并完成新 turn；codex 只是提示模型不一致（`This session was recorded with model X but is resuming with Y`），不阻断。§10.5 的假设成立。

## 17. 仍待确认

1. `item.updated` 在何种 item 上真实出现（至今未观测到；`command_execution` 长输出、`todo_list` 更新是候选）。归一化已按 last-write-wins 处理，出现时不会出错。
2. `file_change` / `mcp_tool_call` / `todo_list` 的 item 字段形状（未触发，归一化先保 `raw`）。`mcp_tool_call` 的工具名字段猜测为 `server`+`tool`/`name`，需实测校正。
3. `codex exec` 收到 SIGTERM 时是否会正常落盘 session；若不会，`halt` 后的 `thread_id` 可能不可 resume。（`halt` 会删除 registry 记录，实践中很少再 resume，优先级低。）
4. 默认模板不传 `--model`：可用模型取决于账号与套餐（本机 ChatGPT 账号下 `gpt-5.1-codex` / `gpt-5.3-codex` 均被拒），交由 codex 自身配置决定更稳。
