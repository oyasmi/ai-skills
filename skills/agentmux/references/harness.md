# Harness 内部差异参考

只在需要理解某个 harness 的具体行为差异时读这里。日常选型看 SKILL.md 的选择表就够。

## 三种结构化 harness

三者都没有终端屏幕、永远不需要补发 `Enter`，但进程模型和排队语义完全不同：

| | `claude-code-ndjson` | `codex-cli-execjson` | `pi-rpc` |
| --- | --- | --- | --- |
| 进程模型 | 1 实例 = 1 长驻进程 = N turns | 1 实例 = N 个短命进程，每 turn 一个 | 1 实例 = 1 长驻进程 = N turns |
| 多轮机制 | 同一进程内连续 turn | `codex exec resume <thread_id>` | 同一进程内连续 turn（JSONL 协议） |
| 实例存活 | 等同于进程存活 | 与进程无关；turn 之间没有进程 | 等同于进程存活 |
| 直接 `prompt`（不带 `--wait-if-busy`）撞上 busy | 入队，当前 turn 结束后作为 follow-up 交付 | 报错 `execjson_instance_busy`，任务指令没有发出 | 入队，当前 run 结束后交付 |
| 完成判定 | `result`、`session_state_changed=idle` 等协议事件 | turn 进程退出并解析 `turn.completed`/`turn.failed` | `agent_settled` 协议事件，重试、compaction 和排队的 follow-up 都会先跑完 |
| `C-c` | 尝试中断进程 | 中断当前 turn 进程（进程直接结束），实例保持可复用 | 协议内 `abort`，保留长驻进程 |
| 成本字段 | `total_cost_usd` | codex 不提供，恒为 `0` | 见 `usage` |

`codex-cli-execjson` 在两个 turn 之间显示 `idle` 且 `process_id: 0` 属于正常状态，不代表实例已退出。

`run` 和 `prompt --wait-if-busy` 已经把这三种差异抹平了：都是先等当前工作收尾，再发送新任务。只有绕开这两者、直接用不带 `--wait-if-busy` 的 `prompt` 时，才会撞上上表这行的原始行为。

## TUI harness（`claude-code`、`codex-cli`、`gemini-cli`）

在 tmux 中运行交互式终端界面，只有需要人工 attach、或环境只提供 TUI 时才用。

- 允许启动耗时；检查升级、确认或权限提示；只有文本已粘贴但未提交时才补发 `Enter`。
- 发送文本后 `agentmux` 会先确认 harness 真的开始工作（默认最多 5 秒，`defaults.status.prompt_ack_ms` 可调），因此 `prompt` 本身可能多花几百毫秒；这是后续 `wait` 和 `inspect` 可信的前提。
- 对新建的 TUI harness，尤其是 Claude Code，优先分开执行 `summon -> capture/inspect -> prompt`，避免启动页或升级提示截获任务指令：

  ```bash
  agentmux summon --template claude-code --name wiki审核-A --cwd /path/to/repo --json
  agentmux capture wiki审核-A --history 10
  agentmux prompt wiki审核-A --text "请阅读 /absolute/path/to/task.md 并按其中的范围和完成标准执行" --json
  ```

- 如果 TUI 输出显示 `A new version is available ... [Y/n]` 一类直接阻塞，先处理提示，再发送真实任务。
- 对较长的 TUI 指令，写入文件后让外部 Agent 读取。短到中等的多行文本可用 `prompt --stdin`。结构化 harness 更适合较大载荷，但非常长的任务仍应引用文件。

## 结构化 capture 的协议字段

`capture --json`（配合 `--trace`/`--raw`/`--history`/`--since`/`--new` 之一）额外返回：

- `claude-code-ndjson`：`messages`、`usage`、`claude_session_id`、`turns`、`last_error`。
- `codex-cli-execjson`：`messages`、`usage`、`thread_id`、`turns`、`turn_state`、`last_error`。
- `pi-rpc`：`messages`、`usage`、`pi_session_id`、`turns`、`last_error`。

对 `codex-cli-execjson`，即使 turn 失败，`wait` 仍表示已经等到结束；从 `turn_state` 和 `last_error` 读取失败原因，实例之后仍可使用。
