---
name: agentmux
description: 通过 `agentmux` CLI 委派和管理外部 AI coding agent：选择 harness、创建或复用实例、编写首次与追加任务指令、等待和读取输出、纠偏、验证交付物以及停止实例。覆盖基于 tmux 的 TUI harness 和 `claude-code-ndjson`、`codex-cli-execjson`、`pi-rpc` 等结构化 harness。用户提到 `agentmux`、需要调用其他 CLI coding agent，或需要复用外部 Agent 完成任务时使用。
---

# Agentmux

只通过 `agentmux` 管理外部 Agent 实例。不要直接调用 `tmux`，也不要读取 harness 原始日志，除非用户明确要求调试底层实现。

## Harness 模型

先从 `template list --json`、`list --json` 或 `inspect --json` 读取 `harness_type`，再判断实例的输入和完成语义。

TUI harness（`claude-code`、`codex-cli`、`gemini-cli`）在 tmux 中运行交互式终端界面。允许启动耗时；检查升级、确认或权限提示；只有文本已粘贴但未提交时才补发 `Enter`。

结构化 harness 没有终端屏幕，永远不需要补发 `Enter`：

1. `claude-code-ndjson`：一个长驻 Claude Code 进程处理多个 turn；busy 时发送的任务指令会排队。
2. `codex-cli-execjson`：每条任务指令启动一个 `codex exec --json` turn 进程，多轮连续性由 `resume <thread_id>` 保持。
3. `pi-rpc`：一个长驻 `pi --mode rpc` 进程通过 JSONL 协议处理多个 turn；busy 时发送的任务指令会排队，在当前运行结束后作为 follow-up 交付；`agent_settled` 表示完成。

`codex-cli-execjson` 在两个 turn 之间显示 `idle` 且 `process_id: 0` 属于正常状态。运行中的实例不接受新任务指令；`execjson_instance_busy` 表示什么也没有发出，必须等待后重发。

## 标准编排循环

默认使用 JSON 模式：

```bash
agentmux template list --json
agentmux list --json
agentmux summon --template <template> --name <name> --json
agentmux inspect <name> --json
agentmux prompt <name> --text "..." --json
agentmux wait <name> --timeout 180s --json
agentmux capture <name> --json
```

按意图选择命令：

1. `template list`：发现可用模板和角色。
2. `list`：查找或扫描现有实例。
3. `summon`：创建或复用实例。
4. `inspect`：低成本读取单个实例的状态和元数据。
5. `prompt`：发送文本、标准输入或支持的按键。
6. `wait`：等待当前工作看起来已经完成，不返回内容。
7. `capture`：立即读取当前可观察输出，不等待完成。
8. `halt`：停止实例。
9. `attach`：只用于人工交互式调试。
10. `version`：确认已安装的命令面。

遵循以下最小循环：

1. 先 `list --json`；必要时再 `template list --json`。
2. 复用合适的既有实例，否则用明确的名称和 `cwd` 创建实例。
3. 根据任务目标、上下文、边界和完成定义编写任务指令。
4. 发送任务指令并耐心 `wait`；需要了解细节时再 `capture`。
5. 读取状态和输出，直接检查实际文件、diff、测试或其他交付物。
6. 接受结果，或根据具体证据发送追加指令；然后回到等待。

`idle`、`wait` 成功或 `capture` 中自信的完成声明都不能证明交付正确。

## 编写任务指令

把每条任务指令当作任务契约，而不是随意的聊天消息。只提供会影响结果的信息；不要堆入无关历史，也不要让外部 Agent 重新调查编排 Agent 已掌握的事实。

首次任务指令按需包含：

1. **工作模式与目标**：明确要求调查、规划、实现还是审查，并描述可观察的结果。
2. **上下文**：指出适用的仓库指令、相关路径或符号、复现步骤、错误信息、已有决策和事实来源。
3. **范围与边界**：说明允许修改的范围、必须保持的行为、非目标，以及需要额外授权的动作。
4. **完成定义（DoD）**：列出可判断真假的验收条件；已知时给出准确的验证命令。
5. **交付说明**：要求报告修改文件、实际执行的检查及结果、剩余风险和阻塞项。

只填写对当前任务有意义的部分。优先提供准确的文件路径、符号名、错误文本和复现输入；通过路径引用仓库中已有的规范，不要复制整份文档。

首次任务指令可使用以下紧凑格式：

```text
工作模式：实现
目标：<要实现的可观察结果>

上下文：
- 先阅读当前目录适用的仓库指令。
- 相关文件或符号：<路径或名称>
- 当前行为或证据：<错误、复现步骤或已有决策>

范围与边界：
- <必须保持的行为和非目标>
- 未经授权，不执行外部写入、破坏性操作或明显扩大任务范围。

完成标准：
- <行为验收条件>
- 成功运行 `<验证命令>`。

如果关键信息无法从当前工作区获得，报告准确的阻塞原因和所需的最小补充，不要猜测。完成后报告修改文件、验证证据以及剩余风险。
```

对复杂、模糊或高风险任务，先发送只允许调查和规划的任务指令，要求返回实现方案、影响范围、假设、风险和验证方法；审查方案后再明确授权实现。对边界清楚、易于验证的常规任务，在同一轮要求实现、测试和自查。除非执行过程本身是需求，否则描述结果和约束，不要过度规定具体步骤。

发送追加任务指令前，先用 `inspect` 或 `capture` 了解当前结果。只发送相对原任务的变化：

```text
观察到：<具体偏差、失败输出或新证据>
需要修正：<期望的具体变化>
保持不变：<不得回退的行为或文件>
重新验证：<命令或可观察检查>
```

不要只发送“继续”“修一下”或“再试试”，除非紧邻的输出已经唯一确定下一步。合并相关反馈，并在发送前遵守对应 harness 的 busy 和排队规则。

遇到复杂或模糊任务、连续纠偏、并行协作或独立审查时，阅读[任务指令参考](references/prompting.md)。

## 启动任务

对结构化 harness、已确认就绪的实例或易于重试的首条指令，优先使用 `summon --prompt`：

```bash
agentmux summon --template codex-cli-execjson --name 登录修复-A --cwd /path/to/repo --prompt "工作模式：实现。修复登录超时后错误重试的问题；先阅读 AGENTS.md 和 internal/auth/；完成后运行 go test ./internal/auth/... 并报告证据。不要改动公开 API。" --json
agentmux wait 登录修复-A --timeout 180s --json
agentmux capture 登录修复-A --json
```

对新建的 TUI harness，尤其是 Claude Code，优先分开执行 `summon -> capture/inspect -> prompt`，避免启动页或升级提示截获任务指令：

```bash
agentmux summon --template claude-code --name wiki审核-A --cwd /path/to/repo --json
agentmux capture wiki审核-A --history 10 --json
agentmux prompt wiki审核-A --text "请阅读 /absolute/path/to/task.md 并按其中的范围和完成标准执行" --json
```

如果 TUI 输出显示 `A new version is available ... [Y/n]` 一类直接阻塞，先处理提示，再发送真实任务。

对较长的 TUI 指令，写入文件后让外部 Agent 读取。短到中等的多行文本可用 `prompt --stdin`。结构化 harness 更适合较大载荷，但非常长的任务仍应引用文件。

## 读取状态和输出

先读取 JSON 顶层字段：`ok`、`command`、`instance`、`reused`、`status`、`error_code`、`error`。

使用以下命令：

```bash
agentmux inspect 编码助手-A --json
agentmux list --json
agentmux capture 编码助手-A --history 120 --json
agentmux capture 编码助手-A --scope session --history 40 --json
```

把 `data.content` 作为 `capture` 的主要输出。先检查 `data.scope`：

- 默认 `--scope current`。TUI harness 返回当前屏幕和可选历史行；结构化 harness 返回当前或最近 turn。
- `--scope session` 用于读取结构化 harness 的已记录会话；TUI harness 仍按屏幕和历史行处理。
- TUI harness 的 `--history` 计算屏幕行；结构化 harness 的 `--history` 限制归一化消息数。

结构化 `capture --json` 还提供协议字段：

- `claude-code-ndjson`：`messages`、`usage`、`claude_session_id`、`turns`。
- `codex-cli-execjson`：`messages`、`usage`、`thread_id`、`turns`、`turn_state`、`last_error`。
- `pi-rpc`：`messages`、`usage`、`pi_session_id`、`turns`、`last_error`。

对 `codex-cli-execjson`，即使 turn 失败，`wait` 仍表示已经等到结束；从 `turn_state` 和 `last_error` 读取失败原因，实例之后仍可使用。

判断状态时使用 `inspect --json`、`list --json` 或 `wait` 返回的 `status`，不要依赖仅供观察的 `pane_title`：

- `idle`：可接收下一条任务指令；`codex-cli-execjson` 此时 `process_id: 0` 正常。
- `busy`：当前或最近的工作仍在进行；TUI 状态可能在 TTL 后退化，结构化状态来自协议或进程。
- `exited`：实例已被停止。
- `lost`：运行时状态缺失或损坏；先检查再决定是否重新创建。

## 等待、输入与中断

只关心完成状态时用 `wait`；需要输出细节时才用 `capture`：

```bash
agentmux wait 编码助手-A --stable 1500 --timeout 30s --json
agentmux wait 登录修复-A --timeout 180s --json
```

`--stable` 只影响通用/TUI 的稳定性检测；结构化 harness 根据协议事件或 turn 进程退出判断完成。

长任务采用耐心循环：`1m, 1m, 3m, 5m`，然后重复。单次等待不要超过 `5m`。`wait` 返回 `capture_timeout` 表示超时时仍在活动，不表示任务失败；随后可用 `capture` 检查进展。

不要因为任务耗时或仍为 `busy` 就中断。只在用户要求、出现明确阻塞、明显循环或崩溃，或者任务约束要求立即纠偏时中断。

如果 `capture` 显示 `Y/n`、权限确认或已粘贴但尚未提交的文本，直接处理该阻塞。结构化 harness 不会出现终端交互阻塞；`codex-cli-execjson` 的权限通常由模板命令中的 `--sandbox` 决定。

需要中断时：

1. 发送一次 `C-c`。
2. 等待 `10-15s`。
3. 用 `inspect --json` 或 `capture --json` 检查结果。
4. 仅在仍无响应或确实应该停止时使用 `halt`。

```bash
agentmux prompt 编码助手-A --key C-c --json
agentmux halt 编码助手-A --timeout 8s --json
agentmux halt 编码助手-A --immediately --json
```

优先使用普通 `halt` 或 `halt --timeout` 做优雅停止。只在用户要求硬停止或优雅中断已无意义时使用 `--immediately`。

支持的按键为 `Enter`、`C-c`、`Escape`、`Up`、`Down`、`Tab`。结构化 harness 只有 `C-c` 有效果，其他按键是 no-op。`codex-cli-execjson` 的 `C-c` 杀死当前 turn 进程并保持实例可复用；`pi-rpc` 的 `C-c` 发送协议内 `abort` 并保留长驻进程。

## 故障恢复

- `template_not_found`：运行 `agentmux template list --json`。
- `instance_not_found`：运行 `agentmux list --json`，再决定是否 `summon`。
- `capture_timeout`：按“仍在活动”处理，继续等待并按需读取快照。
- `process_not_running`：先用 `inspect --json` 检查实例，再决定是否重新 `summon`。
- `execjson_instance_busy`：当前任务指令没有发出；先 `wait`，再原样重发。不要立即重试，也不要用 `halt` 解锁。
- `invalid_key`：改用 `Enter`、`C-c`、`Escape`、`Up`、`Down` 或 `Tab`。
- 命令疑似不存在：运行 `agentmux version --json` 和 `agentmux help <command>`。

`codex-cli-execjson` 出现 `config_invalid` 时，把模板命令改成只带受支持父级 flag 的普通 `codex exec` 前缀，例如 `--sandbox`、`--cd`、`--add-dir`、`--color`、`--skip-git-repo-check` 或 `--model`。移除 `--json`、`-o`、`resume`、`review`、`--ask-for-approval`、`--ephemeral`、位置参数、管道和重定向；turn 参数由 agentmux 注入。

`summon --model` 在 `codex-cli-execjson` 上失败时，检查模板命令是否包含 `$MODEL`。没有占位符时，使用 Codex 的默认模型，或修改模板命令加入 `--model $MODEL`。

## 复用、并行与审查

使用描述性实例名，不要使用 `worker1` 一类泛化名称。并行分片使用共同前缀和范围后缀，例如 `wiki审核-Q1to5`、`wiki审核-Q6to10`。

同一目标且历史仍然相关时复用实例。切换到无关任务，或同一问题连续两次纠偏仍失败时，创建新实例，并在新的首次任务指令中吸收已经确认的证据和约束。

Agentmux 隔离的是 Agent 运行实例，不是仓库文件。相同 `cwd` 的实例会共享工作树、Git 状态和构建产物。不要让多个写入型 Agent 同时修改同一个 checkout；并行写入时为每个实例创建独立 worktree，并传入不同的 `--cwd`。无法隔离工作目录时只保留一个写入者，其他实例只基于稳定快照进行调查或审查。

对高风险改动或长时间自主执行的任务，使用新实例独立审查稳定的 diff。向审查者提供原始完成标准和变更范围，要求只报告影响正确性、明确需求或安全性的缺口，不追逐纯风格偏好。

## 交付验收

外部 Agent 报告完成后：

1. 用 `capture` 读取它声称完成的内容、检查命令和阻塞项。
2. 直接读取预期文件、diff 或产物。
3. 亲自运行与风险相称的测试、构建、lint、类型检查或视觉检查。
4. 对照原始完成定义判断接受、发送证据化纠偏，或改用新实例审查。

要求外部 Agent 自验，不能替代编排 Agent 的独立验收。
