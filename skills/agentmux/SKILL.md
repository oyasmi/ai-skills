---
name: agentmux
description: 通过 `agentmux` CLI 委派和管理外部 AI coding agent：选择 harness、创建或复用实例、编写首次与追加任务指令、等待和读取输出、纠偏、验证交付物以及停止实例。覆盖基于 tmux 的 TUI harness 和 `claude-code-ndjson`、`codex-cli-execjson`、`pi-rpc` 等结构化 harness。用户提到 `agentmux`、需要调用其他 CLI coding agent，或需要复用外部 Agent 完成任务时使用。
---

# Agentmux

## 速查

单个任务，一条命令就够：

```bash
agentmux run --template <模板> --cwd <路径> --prompt "<任务契约>" --timeout 10m --json
```

`run` = summon + prompt + wait + capture。默认从它开始；只有需要分步控制（并行、中途观察、纠偏）时才拆开用：

```bash
agentmux template list --json                                   # 有哪些模板和 harness
agentmux list --json                                            # 有哪些实例、分别什么状态（--all 含已停止的墓碑）
agentmux summon --template <模板> --name <名称> --cwd <路径> --prompt "..." --json  # 发出去但不等待
agentmux prompt <名称> --text "..." --json                       # 追加指令
agentmux wait <名称> --timeout 3m --json                         # 等待；timed_out=true 表示仍在工作
agentmux wait <A> <B> <C> --mode any --timeout 5m --json         # 并行：谁先完成就返回谁
agentmux capture <名称>                                          # 读输出：纯文本，最省 token
agentmux capture <名称> --json --since <cursor>                  # 只读新增部分
agentmux halt <名称> --json                                      # 停止实例
```

五条硬规则：

1. 只通过 `agentmux` 管理外部 Agent 实例。不要直接调用 `tmux`，也不要读取 harness 原始日志，除非用户明确要求调试底层实现。
2. 单路任务用 `run`，不要手工拼 summon/prompt/wait/capture。并行时不要用 `run`——它会阻塞到自己那一路结束。
3. 读输出优先用**不带 `--json`** 的 `capture`；`--json` 只在需要 `usage`、`thread_id`、`turn_state`、`last_error` 时使用，并始终配合 `--history` 或 `--since`。
4. `wait` 超时（`timed_out: true`）是「还在工作」，不是失败；继续等即可。
5. `idle`、`wait` 成功、`capture` 中自信的完成声明，都不能证明交付正确。必须自己读文件、diff、跑验证。

## 安装依赖

这个 skill 需要 `agentmux` CLI；skill 本身不包含二进制文件。使用前先安装
`agentmux`，并确认它在 `PATH` 中：

### 从本仓库安装

```bash
cd /path/to/ai-skills/tools/agentmux
./scripts/install.sh
```

脚本会把 CLI 安装到 `~/.local/bin/agentmux`，并在需要时写入默认配置
`~/.config/agentmux/config.yaml`。也可以只构建到本地目录：

```bash
cd /path/to/ai-skills/tools/agentmux
go build -o ./bin/agentmux ./cmd/agentmux
```

### 使用发布包安装

从项目 Release 下载与当前系统匹配的 `agentmux_<version>_<os>_<arch>.tar.gz`，
解压后将其中的 `agentmux` 放入 `PATH`，例如：

```bash
mkdir -p ~/.local/bin
install -m 0755 ./agentmux ~/.local/bin/agentmux
```

运行 `agentmux version --json` 验证安装。运行时还需要按所选模板安装对应的
外部 CLI；基于 tmux 的模板需要 `tmux`，结构化模板还需要 `claude`、`codex`
或 `pi`。具体模板命令可通过 `agentmux template list --json` 查看。

## Harness 模型

先从 `template list --json`、`list --json` 或 `inspect --json` 读取 `harness_type`，再判断实例的输入和完成语义。

选择 harness：

| 场景 | 选择 | 理由 |
| --- | --- | --- |
| 默认：委派可验证的编码任务 | 结构化 harness（`claude-code-ndjson`、`codex-cli-execjson`、`pi-rpc`） | 无终端界面竞态，不需要补 `Enter`，输出可结构化读取 |
| 多轮追问、需要共享上下文 | `claude-code-ndjson` 或 `pi-rpc` | 长驻进程，busy 时任务指令排队 |
| 独立 turn、易于并行分片 | `codex-cli-execjson` | 每个 turn 一个进程，turn 之间无进程 |
| 用户要旁观、接管或调试终端 | `claude-code`、`codex-cli`、`gemini-cli` | 可 `attach`；代价是启动提示、按键和状态推断都更脆弱 |

没有特殊理由时优先结构化 harness。只有需要人工 attach，或环境只提供 TUI 时才用 TUI harness。

TUI harness（`claude-code`、`codex-cli`、`gemini-cli`）在 tmux 中运行交互式终端界面。允许启动耗时；检查升级、确认或权限提示；只有文本已粘贴但未提交时才补发 `Enter`。发送文本后 `agentmux` 会先确认 harness 真的开始工作（默认最多 5 秒），因此 `prompt` 本身可能多花几百毫秒；这是后续 `wait` 和 `inspect` 可信的前提。

结构化 harness 没有终端屏幕，永远不需要补发 `Enter`：

1. `claude-code-ndjson`：一个长驻 Claude Code 进程处理多个 turn；busy 时发送的任务指令会排队。
2. `codex-cli-execjson`：每条任务指令启动一个 `codex exec --json` turn 进程，多轮连续性由 `resume <thread_id>` 保持。
3. `pi-rpc`：一个长驻 `pi --mode rpc` 进程通过 JSONL 协议处理多个 turn；busy 时发送的任务指令会排队，在当前运行结束后作为 follow-up 交付；`agent_settled` 表示完成。

`codex-cli-execjson` 在两个 turn 之间显示 `idle` 且 `process_id: 0` 属于正常状态。运行中的实例不接受新任务指令；`execjson_instance_busy` 表示什么也没有发出，必须等待后重发。

## 标准编排循环

单路任务的完整流程就是一条 `run`：

```bash
agentmux run --template codex-cli-execjson --name 登录修复-A --cwd /path/to/repo \
  --prompt-file /abs/path/task.md --timeout 10m --json
```

读 `data.content` 拿结果，`data.timed_out` 判断是否还在跑，然后**自己**验证交付物。实例保留，可继续追加指令。

需要分步控制时，控制类命令用 JSON 模式，读输出用文本模式：

```bash
agentmux template list --json
agentmux list --json
agentmux summon --template <template> --name <name> --json
agentmux inspect <name> --json
agentmux prompt <name> --text "..." --json
agentmux wait <name> --timeout 180s --json
agentmux capture <name>
```

按意图选择命令：

1. `run`：一次性委派一个任务并拿回结果（默认入口）。
2. `template list`：发现可用模板和角色。
3. `list`：查找或扫描现有实例；`--all` 包含已停止的墓碑。
4. `summon`：创建或复用实例；带 `--prompt` 时发出任务但不等待，适合并行分片。
5. `inspect`：低成本读取单个实例的状态和元数据，对墓碑同样有效。
6. `prompt`：发送文本、标准输入或支持的按键。
7. `wait`：等待一个或多个实例完成，不返回内容。
8. `capture`：立即读取当前可观察输出，不等待完成。
9. `halt`：停止实例。
10. `attach`：只用于人工交互式调试。
11. `version --json`：读取 `features` 判断本机 agentmux 是否支持某个能力，不要靠试命令看报错。

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

单路任务用 `run`，一次调用拿到结果：

```bash
agentmux run --template codex-cli-execjson --name 登录修复-A --cwd /path/to/repo --prompt "工作模式：实现。修复登录超时后错误重试的问题；先阅读 AGENTS.md 和 internal/auth/；完成后运行 go test ./internal/auth/... 并报告证据。不要改动公开 API。" --timeout 10m --json
```

较长的任务契约写入文件，用 `--prompt-file` 传入，不要塞进命令行：

```bash
agentmux run --template claude-code-ndjson --name 审查-A --cwd /path/to/repo --prompt-file /abs/path/task.md --timeout 15m --json
```

需要发出任务但先不阻塞（并行分片、或先去做别的事），用 `summon --prompt`：

```bash
agentmux summon --template codex-cli-execjson --name 登录修复-A --cwd /path/to/repo --prompt "..." --json
agentmux wait 登录修复-A --timeout 180s --json
agentmux capture 登录修复-A
```

对新建的 TUI harness，尤其是 Claude Code，优先分开执行 `summon -> capture/inspect -> prompt`，避免启动页或升级提示截获任务指令：

```bash
agentmux summon --template claude-code --name wiki审核-A --cwd /path/to/repo --json
agentmux capture wiki审核-A --history 10
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
agentmux capture 编码助手-A
agentmux capture 编码助手-A --history 120
agentmux capture 编码助手-A --json --history 20
agentmux capture 编码助手-A --scope session --history 40 --json
```

读输出的成本纪律：

- **默认用不带 `--json` 的 `capture`**，它只打印聚合后的内容，通常几十到几百字节。
- 结构化 harness 每个协议事件对应一条消息。`--json` 默认只返回最近 20 条并去掉原始事件；`--history 0` 和 `--raw` 会恢复完整输出，只在调试协议时使用。
- TUI harness 的 `--history` 是屏幕行数，按需给值，不要习惯性写 120。
- **反复观察同一个长任务时用 `--since`**：每次结构化 `capture --json` 都会返回 `data.next_cursor`，把它作为下次的 `--since`，只会拿到新增内容。没有新增时返回空 `messages`，`next_cursor` 不变。TUI harness 不支持 `--since`。

把 `data.content` 作为 `capture --json` 的主要输出。先检查 `data.scope`：

- 默认 `--scope current`。TUI harness 返回当前屏幕和可选历史行；结构化 harness 返回当前或最近 turn。
- `--scope session` 用于读取结构化 harness 的已记录会话；TUI harness 仍按屏幕和历史行处理。
- TUI harness 的 `--history` 计算屏幕行；结构化 harness 的 `--history` 限制归一化消息数。

结构化 `capture --json` 还提供协议字段：

- `claude-code-ndjson`：`messages`、`usage`、`claude_session_id`、`turns`、`last_error`。
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

读 `wait` 的返回：

- `ok: true` + `data.timed_out: false` + `status: idle`：本轮工作已经结束。
- `ok: true` + `data.timed_out: true` + `status: busy`：**仍在工作**，继续等，不要当作失败，也不要因此中断。
- `ok: false`：实例真的出问题了（丢失、退出、进程异常），按错误码处理。
- `data.saw_busy` 表示本次等待确实观察到 harness 在工作，`data.elapsed_ms` 是实际等待时长。

并行等待多个实例：

```bash
agentmux wait 分片-A 分片-B 分片-C --mode any --timeout 5m --json
agentmux wait 分片-A 分片-B 分片-C --mode all --timeout 5m --json
```

- `--mode any`：任意一个完成就返回，用于「先处理最先完成的分片」，不被最慢的挡住。
- `--mode all`（默认）：全部完成或超时才返回。
- 多实例时读 `data.done`、`data.pending`、`data.failed` 和 `data.instances`；某个实例失败只记在它自己身上，不影响其他结果。
- 实例名必须全部写在 flag 之前。

`--stable` 对 TUI harness 有两个作用：通用稳定性检测的窗口，以及信任 idle 信号前的最小观察窗口；结构化 harness 根据协议事件或 turn 进程退出判断完成。除非明确知道后果，不要设 `--stable 0`。

长任务采用耐心循环：`1m, 1m, 3m, 5m`，然后重复。单次等待不要超过 `5m`。每次超时后可以用 `capture` 检查进展，再继续等待。

不要因为任务耗时或仍为 `busy` 就中断。只在用户要求、出现明确阻塞、明显循环或崩溃，或者任务约束要求立即纠偏时中断。

如果 `capture` 显示 `Y/n`、权限确认或已粘贴但尚未提交的文本，直接处理该阻塞。结构化 harness 不会出现终端交互阻塞；`codex-cli-execjson` 的权限通常由模板命令中的 `--sandbox` 决定。

需要中断时：

1. 发送一次 `C-c`。
2. 等待 `10-15s`。
3. 用 `inspect --json` 查看状态，或 `capture` 查看输出。
4. 仅在仍无响应或确实应该停止时使用 `halt`。

```bash
agentmux prompt 编码助手-A --key C-c --json
agentmux halt 编码助手-A --timeout 8s --json
agentmux halt 编码助手-A --immediately --json
```

优先使用普通 `halt` 或 `halt --timeout` 做优雅停止。只在用户要求硬停止或优雅中断已无意义时使用 `--immediately`。

支持的按键为 `Enter`、`C-c`、`Escape`、`Up`、`Down`、`Tab`。结构化 harness 只有 `C-c` 有效果，其他按键是 no-op。`codex-cli-execjson` 的 `C-c` 杀死当前 turn 进程并保持实例可复用；`pi-rpc` 的 `C-c` 发送协议内 `abort` 并保留长驻进程。

## 故障恢复

先读 `error_code`，按下表执行；不要凭错误文本猜测。

| `error_code` | 含义 | 下一步 |
| --- | --- | --- |
| `template_not_found` | 模板名不存在 | `agentmux template list --json` |
| `instance_not_found` | 实例不存在或已被清理 | `agentmux list --json`，再决定是否 `summon` |
| `instance_template_mismatch` | 同名实例属于其他模板 | 换一个描述性实例名 |
| `process_not_running` | 实例已停止 | 错误信息里带 `end_reason`；用 `inspect --json` 读墓碑的 `end_reason`、`ended_at`、`last_error`，判断是被 halt 还是崩溃，再决定是否用同名 `summon` 重建 |
| `session_not_found` | 运行时会话缺失 | 同上 |
| `execjson_instance_busy` | 任务指令没有发出，turn 正在跑 | 先 `wait`，再原样重发；不要立即重试，也不要用 `halt` 解锁 |
| `invalid_key` | 按键不在白名单 | 改用 `Enter`、`C-c`、`Escape`、`Up`、`Down`、`Tab` |
| `invalid_arguments` | 参数错误 | 按提示修正；注意实例名必须写在所有 flag 之前 |
| `tmux_unavailable` | tmux 不可用或命令失败 | 报告环境问题；这不是外部 Agent 的错误 |
| `config_invalid` / `config_parse_error` / `config_io_error` | 配置问题 | 见下面的模板命令修复说明；必要时请用户检查配置 |
| `registry_io_error` / `registry_parse_error` / `registry_lock_error` | agentmux 自身状态文件异常 | 重试一次；仍失败则报告给用户，不要反复重试 |
| `ndjson_*` / `execjson_*` / `rpc_*` | 对应结构化 harness 的传输或状态错误 | `inspect --json` 确认状态，必要时新建实例继续 |
| `instance_changed` | 发送期间实例被其他进程替换 | 重新 `inspect --json` 后再决定 |
| `internal_error` | 未归类错误 | 报告给用户，不要静默重试 |

`wait` 超时不再表现为错误：它返回 `ok: true` 和 `data.timed_out: true`，按“仍在工作”处理。

实例停止后不会从记录中消失，而是保留为墓碑：`instance_not_found` 表示这个名字从来不存在（多半是名字写错），`process_not_running` 表示它确实存在过但已经停止，`inspect` 仍能读到停止原因。排查外部 Agent “消失”时先 `inspect --json`，再看 `list --all --json`。

命令疑似不存在时：运行 `agentmux version --json` 和 `agentmux help <command>`。

`codex-cli-execjson` 出现 `config_invalid` 时，把模板命令改成只带受支持父级 flag 的普通 `codex exec` 前缀，例如 `--sandbox`、`--cd`、`--add-dir`、`--color`、`--skip-git-repo-check` 或 `--model`。移除 `--json`、`-o`、`resume`、`review`、`--ask-for-approval`、`--ephemeral`、位置参数、管道和重定向；turn 参数由 agentmux 注入。

`summon --model` 在 `codex-cli-execjson` 上失败时，检查模板命令是否包含 `$MODEL`。没有占位符时，使用 Codex 的默认模型，或修改模板命令加入 `--model $MODEL`。

## 复用、并行与审查

使用描述性实例名，不要使用 `worker1` 一类泛化名称。并行分片使用共同前缀和范围后缀，例如 `wiki审核-Q1to5`、`wiki审核-Q6to10`。

并行的标准形态是「先全部发出，再统一等待」：

```bash
agentmux summon --template codex-cli-execjson --name wiki审核-Q1to5 --cwd /wt/a --prompt "..." --json
agentmux summon --template codex-cli-execjson --name wiki审核-Q6to10 --cwd /wt/b --prompt "..." --json
agentmux wait wiki审核-Q1to5 wiki审核-Q6to10 --mode any --timeout 5m --json
```

用 `--mode any` 先拿到最先完成的分片并开始验收，剩下的继续等；不要对每个分片串行 `wait`，也不要用 `run` 做并行（它会阻塞到自己那一路结束）。

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
