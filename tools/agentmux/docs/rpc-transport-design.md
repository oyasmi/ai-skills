# pi-rpc Direct Process Transport 实现文档

## 1. 背景与目标

`agentmux` 已经为 Claude Code（`claude-code-ndjson`）和 Codex CLI（`codex-cli-execjson`）接入了结构化 headless 模式。pi coding agent 也提供了一条程序化协议：`pi --mode rpc`，通过 stdin 的 JSONL 命令和 stdout 的 JSONL 事件驱动，无需终端仿真。

本设计新增 `pi-rpc` harness type。该路径不使用 tmux，不读屏幕、不发按键，agentmux 直接启动并管理一个常驻 `pi --mode rpc` 进程：

```text
agentmux CLI -> FIFO(stdin) / JSONL(stdout) -> pi --mode rpc process
```

协议参考 `~/projects/pi/packages/coding-agent/docs/rpc.md` 及 pi 源码（`src/modes/rpc/rpc-mode.ts`、`src/core/agent-session.ts`、`src/main.ts`）。

## 1.1 与两条既有结构化路径的关系

进程模型上，`pi-rpc` 与 `claude-code-ndjson` 同构，而与 `codex-cli-execjson` 不同：

- **claude-code-ndjson / pi-rpc**：一个长驻进程读取 FIFO 命令、向 output.jsonl 持续输出事件，贯穿实例整个生命周期。进程死亡意味着实例消失。
- **codex-cli-execjson**：进程间无常驻体，每个 prompt 派生一个短命 `codex exec` 进程；进程死亡只表示一个 turn 结束。

因此 `pi-rpc` 的 Reconcile / Wait / Halt 语义靠拢 ndjson。但协议细节每一步都不同，所以 `internal/rpcctl` 是**独立实现**，不与 ndjsonctl / execjsonctl 复用代码。

pi 协议相较 claude 的三点差异（均已在实现中利用）：

1. **命令带 `id`、响应带同 `id`**：prompt 的接受/拒绝可精确关联，无需靠 replay 出的 user message 反推。
2. **带内 `abort`**：中断是一条 JSONL 命令，会让当前 run 干净 settle，而不必只靠进程信号。
3. **`agent_settled` 语义明确**：pi 在“无自动重试、无 compaction 重试、无排队 follow-up 残留”时才发出该事件，正好等价于 agentmux 的“at rest”。这比 claude 的 idle 推断更可靠。

## 1.2 关键行为核对（基于 pi 源码）

- `--session-id <id>`（`src/main.ts`）：**存在则按项目 cwd 恢复该会话，不存在则用该 id 新建**。因此新建与 resume 共用同一个 flag，resume 就是“在相同 cwd 用相同 id 再启动一次”。id 只需满足 `^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`，agentmux 生成的 UUIDv4 满足。
- stdin EOF 会触发 pi 关闭（`process.stdin.on("end", ...)`）。因此必须像 ndjson 一样用 `exec 3<>"$FIFO"` 持有一个读写描述符，避免命令间隙 pi 因 EOF 退出。
- prompt 命令的响应在 preflight 成功后立即发出；`streamingBehavior`（`src/core/agent-session.ts` 的 `prompt()`）**仅在 `isStreaming` 时被消费**，idle 时忽略。故 agentmux 恒定发送 `streamingBehavior:"followUp"`：idle 立即执行，streaming 则入队为 follow-up，在同一个 run 内 drain。
- `agent_settled` 在 `_runAgentPrompt` 的 finally 中发出，覆盖重试/compaction/队列 follow-up；`abort` 也会走到该 finally，故中断同样产生 settle。

## 2. 非目标

1. 不复用 tmux 作为进程容器。
2. 不把 `capture` 伪装成抓屏；`pi-rpc` 返回协议视角的消息快照。
3. 不支持 TUI 导航键；除 `C-c` 映射为 interrupt 外，其他 `prompt --key` 不产生协议输入。
4. 不引入常驻 daemon；所有控制命令仍是一次性 CLI。
5. 不实现 pi RPC 的全部命令面（fork/clone/compact/get_tree 等）。只实现 summon/prompt/wait/capture/interrupt/halt/attach/resume 所需子集。

## 3. 传输目录布局

`StateDir/rpc/<sessionID>/`：

```text
input.fifo     # 写入 pi 的 JSONL 命令（prompt / abort / extension_ui_response）
output.jsonl   # pi 的 stdout 事件流，增量解析
stderr.log     # pi 的 stderr
state.json     # agentmux 维护的协议状态机（flock 保护）
process.json   # pid/pgid/指纹
run.sh         # 保活 FIFO 并 exec pi 的启动脚本
```

一个传输目录对应一段进程生命。resume 时在**新目录**重建，保持“一个目录 = 一段进程生命”。

## 4. 启动命令

`buildPiCommand` 在模板命令后追加：

```text
--mode rpc --session-id '<uuid>' [--append-system-prompt '<sp>']
```

- system prompt 用 `--append-system-prompt`；若模板命令已含 `--system-prompt` / `--append-system-prompt` 则不追加。
- run.sh 保活 FIFO：

```sh
exec 3<>"$FIFO"
exec pi ... --mode rpc --session-id '...' < "$FIFO" >> "$OUT" 2>> "$ERR"
```

## 5. 协议状态机（state.json）

### 5.1 prompt 生命周期

每个 prompt 生成一个 UUID，写入 `{"id","type":"prompt","message","streamingBehavior":"followUp"}`，并入 `pending_prompts`：

```text
sent      -> 已写 FIFO，等待 response
accepted  -> 收到 response(prompt, success)（运行中或已入队）
done      -> agent_settled 后（该 prompt 所在 run 已完全 drain）
failed    -> 收到 response(prompt, success:false)，记录 error
cancelled -> interrupt 中止
```

### 5.2 事件应用（`applyEvents`）

| 事件 | 处理 |
|---|---|
| `response`(command=prompt) | 按 id 定位 pending，success→accepted，failure→failed + LastError |
| `agent_start` | `AgentRunActive=true`，清 InterruptedAt |
| `agent_settled` | `AgentRunActive=false`，`ResumeAvailable=true`，所有 sent/accepted → done，每个完成的 prompt `TotalTurns++`（一次对话轮次） |
| `turn_end`(assistant) | 累加 usage（tokens + cost.total）；不计 turns——pi 的 turn_end 是 agent-loop step（thinking/tool call/tool result），非对话轮次 |
| `extension_ui_request`(dialog) | 记录待自动取消的对话框 id |

状态派生：`busy` = `AgentRunActive || hasUnfinishedPrompt`，否则 `idle`（`exited` 为终态不覆盖）。

### 5.3 usage / cost

从 `turn_end` 的 assistant `usage` 累加：`input/output/cacheRead/cacheWrite` 及 `cost.total`。由于未完成 prompt 的同步可能从 `startOffset` 重放事件，累计时使用事件结束 offset 做幂等去重。capture 的 usage 来自 state 累计值，与单次窗口无关。

## 6. 各命令语义

- **Start**：建目录/FIFO，写 run.sh，setsid 起进程组，写 process.json/state.json，短暂等待确认进程存活后置 idle。
- **SendPrompt**：写 prompt 命令到 FIFO，入 pending（sent），置 busy。忙碌时不拒绝，靠 `followUp` 入队。
- **Wait**：轮询增量解析，直到 `!hasUnfinishedPrompt && !AgentRunActive`。从未 prompt 的实例立即返回（不挂死）。
- **Capture**：current scope 从最近 prompt 的 startOffset 起，session scope 全量。normalize `message_end`（text/thinking/toolCall）为 messages；Extra 带 `pi_session_id`、`turns`、`usage`、`last_error`、`raw_event_count`。
- **Interrupt**：有在飞时写 `{"type":"abort"}`（带内、干净 settle）；FIFO 写失败退回进程组 SIGINT。无在飞时为 no-op（避免把 idle 实例 wedge 成 busy）。
- **Halt**：SIGTERM→（宽限后）SIGKILL 进程组，删 FIFO，state 置 exited。
- **Reconcile**：进程死→exited（长驻模型）；活着则增量应用事件。
- **CanResume**：`PiSessionID != "" && state.ResumeAvailable`（首个 agent_settled 后置位）。
- **Attach**：`tail -f output.jsonl`。

## 7. 防挂死：扩展对话框自动取消

pi 扩展可能发 `extension_ui_request`（select/confirm/input/editor），dialog 类会阻塞 agent 直到收到 `extension_ui_response`。headless 下无人应答会永久阻塞。

`syncState` 在锁内识别未处理的 dialog 请求，在锁外向 FIFO 写 `{"type":"extension_ui_response","id":...,"cancelled":true}`，成功后再在锁内标记 handled（写失败则下次 sync 重试）。fire-and-forget 方法（notify/setStatus/…）不阻塞，忽略。

## 8. 中断静默兜底

正常情况 abort 会产生 agent_settled。作为极端兜底，`degradeSilentInterrupt`：若 busy、有 InterruptedAt、无在飞 prompt，且自中断/最后事件起静默超过 `interruptSilenceGrace`（5s），强制回 idle，防止永久 busy。

## 9. resume

死亡的 `pi-rpc` 实例在 `summon` 同名同模板且 `CanResume` 为真时，于新传输目录、保留 `PiSessionID` 重建。相同 cwd + 相同 session id 使 pi 打开既有会话。

## 10. 独立实现说明

`internal/rpcctl` 不与 ndjsonctl / execjsonctl 共享代码。FIFO、进程、状态锁等机制虽形似，但错误码前缀（`rpc_*`）、协议解析、状态字段均为 pi 专用，独立实现避免耦合三套不同协议。
