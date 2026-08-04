---
name: agentmux
description: 通过 `agentmux` CLI 委派和管理外部 AI coding agent：按角色选模板（planner/builder/reviewer 等）、创建或复用实例、编写首次与追加任务指令、等待和读取输出、纠偏、验证交付物以及停止实例。模板声明 model 与 effort（thinking level）档位，agentmux 翻译到 `claude-code-ndjson`、`codex-cli-execjson`、`pi-rpc` 等 headless 结构化 harness 以及基于 tmux 的 TUI harness。用户提到 `agentmux`、需要调用其他 CLI coding agent，或需要复用外部 Agent 完成任务时使用。
---

# Agentmux

## 速查

单个任务，一条命令就够：

```bash
agentmux run --template <角色> --cwd <路径> --prompt "<任务契约>" --timeout 10m --json
```

`run` = summon + prompt + wait + capture，复用到忙实例时还会先在预算内等它收尾。默认从它开始；只有需要分步控制（中途观察、纠偏）时才拆开用：

```bash
agentmux doctor --json                                           # 环境自检：二进制、PATH、配置、模板命令是否都就绪
agentmux template list --json                                    # 有哪些角色：读 description 判断该不该用它
agentmux list --json                                             # 有哪些实例、分别什么状态（--all 含已停止的墓碑）
agentmux summon --template <角色> --name <名称> --cwd <路径> --prompt "..." --json  # 发出去但不等待
agentmux prompt <名称> --text "..." --json                        # 追加指令
agentmux wait <名称> --timeout 3m --json                          # 等待；timed_out=true 表示仍在工作
agentmux wait <A> <B> <C> --mode any --timeout 5m --collect --json  # 并行：谁先完成就带回谁的内容
agentmux capture <名称>                                           # 读输出：纯文本，最省 token
agentmux capture <名称> --new --json                              # 只读新增部分；游标由 agentmux 自己记账
agentmux halt <名称> --json                                       # 停止实例
```

六条硬规则：

1. 只通过 `agentmux` 管理外部 Agent 实例。不要直接调用 `tmux`，也不要读取 harness 原始日志，除非用户明确要求调试底层实现。
2. **按角色选模板，不要按 harness 选**。先 `template list --json` 读 `description`（它写明了什么场景用、怎么用才对），再决定用谁。
3. `run` 是默认入口：单路任务直接 `run`；并行时用 `run --detach` 逐个发出（各自独立 `--cwd`），再统一 `wait --mode any --collect`。
4. `capture --json` 默认已经精简（不含逐条消息轨迹）；只要目的是"看外部 Agent 说了什么"，优先用不带 `--json` 的 `capture`；需要消息轨迹时才加 `--trace`。
5. `wait` 超时（`timed_out: true`）是「还在工作」，不是失败；继续等即可。
6. `idle`、`wait` 成功、`capture` 中自信的完成声明，都不能证明交付正确。必须自己读文件、diff、跑验证。

## 安装依赖

命令不存在时先看[安装参考](references/install.md)。命令存在但行为可疑时，先跑 `agentmux doctor --json` 而不是怀疑 agentmux 有 bug——多数"文档说的和实际不一致"最终会定位到 PATH 上的旧二进制或缺失的外部 CLI，`doctor` 直接指出来。

## 选角色

模板是**角色**，不是 harness。选人只看两件事：这个角色适合当前场景吗，用它的正确姿势是什么。两者都写在 `description` 里，因此选人前必须读它：

```bash
agentmux template list --json     # description 是多行的，文本表格只显示第一行，选人要用 --json
```

角色由 `model` + `effort` 定强度档位（`effort` 由弱到强：`off`/`minimal`/`low`/`medium`/`high`/`xhigh`/`max`）。一份典型的角色配置长这样，但**以本机 `template list --json` 的实际内容为准，不要假设某个角色一定存在**：

| 角色 | 典型场景 | 典型档位 |
| --- | --- | --- |
| `planner` | 复杂、模糊或高风险改动：先出方案再动手 | 最强模型 + `max`，只读 sandbox |
| `builder` | 边界清楚、可验证的常规编码任务（默认入口） | 中档模型 + `medium` |
| `builder-hard` | `builder` 连续纠偏失败、或跨模块的复杂约束 | 最强模型 + `xhigh` |
| `reviewer` | 高风险改动或长时间自主执行之后的独立验收 | 与 builder 不同的模型家族 + `xhigh`，只读 sandbox |
| `scout` | 便宜快速的单点事实查询 | 廉价模型 + `low` |
| `documenter` | 需求梳理、设计说明、使用文档 | 中档模型 + `medium` |

选人原则：

1. **默认 `builder`**。任务边界清楚、可验证时不要升档，`xhigh`/`max` 只是更慢更贵。
2. **先规划再实现**：任务复杂、模糊或高风险时先用 `planner` 拿方案，审查通过后把结论作为 `builder` 的任务契约。不要让 `planner` 直接改代码。
3. **审查要换家族**：`reviewer` 刻意与 `builder` 用不同的模型家族，避免同源盲区。只读角色可以和写入型角色共享 `cwd`。
4. **纠偏两次仍失败就升档**，而不是继续在同一个实例上重试：换 `builder-hard`，并把已确认的证据和约束带过去。
5. 只有"人要旁观、接管或调试终端"时才用可 `attach` 的 TUI 角色（通常叫 `claude-code-tui` 之类）；它启动提示多、按键和状态推断都更脆弱。

临时调档不用改配置，也不要为此新建模板：

```bash
agentmux run --template builder --effort xhigh --model opus --prompt-file ./task.md --timeout 20m --json
```

`--model`/`--effort` 只在**新建**实例时生效；复用到既有实例时不会改它的配置，要换档位就换一个实例名。

需要判断某个实例的输入和完成语义时，再从 `template list --json`、`list --json` 或 `inspect --json` 读 `harness_type`：结构化 harness（`claude-code-ndjson`、`codex-cli-execjson`、`pi-rpc`）没有终端界面竞态，不需要补 `Enter`；TUI harness（`claude-code`、`codex-cli`、`gemini-cli`）可以 `attach`。它们各自的进程模型、busy 语义、协议字段差异，以及 `model`/`effort` 具体翻译成哪个 flag，见[harness 内部差异参考](references/harness.md)。日常委派不需要展开这一层。

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

不要只发送"继续""修一下"或"再试试"，除非紧邻的输出已经唯一确定下一步。合并相关反馈。

遇到复杂或模糊任务、连续纠偏、并行协作或独立审查时，阅读[任务指令参考](references/prompting.md)。

## 启动与控制

按意图选择命令：

1. `run`：一次性委派一个任务并拿回结果（默认入口）；`--detach` 让它发出即返回，用于并行分片。
2. `template list --json`：发现可用角色，并从 `description` 判断该用谁、怎么用。
3. `list`：查找或扫描现有实例；`--all` 包含已停止的墓碑。
4. `summon`：创建或复用实例；带 `--prompt` 时发出任务但不等待。
5. `inspect`：低成本读取单个实例的状态和元数据，对墓碑同样有效。
6. `prompt`：发送文本、标准输入或支持的按键；`--wait-if-busy <duration>` 让它在发送前先等实例把当前工作收尾。
7. `wait`：等待一个或多个实例完成；`--collect` 顺带读回已完成实例的内容。
8. `capture`：立即读取当前可观测输出，不等待完成。
9. `halt`：停止实例。
10. `attach`：只用于人工交互式调试。
11. `doctor --json`：环境不对劲时先跑这个，而不是先怀疑命令用错了。
12. `version --json`：读取 `features` 判断本机 agentmux 是否支持某个能力，不要靠试命令看报错。

较长的任务契约写入文件，用 `--prompt-file` 传入，不要塞进命令行：

```bash
agentmux run --template reviewer --name 审查-A --cwd /path/to/repo --prompt-file /abs/path/task.md --timeout 15m --json
```

遵循以下最小循环：

1. 先 `list --json` 看有没有能复用的实例；要新建就先 `template list --json` 选角色。
2. 复用合适的既有实例，否则用明确的名称和 `cwd` 创建实例。
3. 根据任务目标、上下文、边界和完成定义编写任务指令。
4. 发送任务指令并耐心 `wait`；需要了解细节时再 `capture`。
5. 读取状态和输出，直接检查实际文件、diff、测试或其他交付物。
6. 接受结果，或根据具体证据发送追加指令；然后回到等待。

`idle`、`wait` 成功或 `capture` 中自信的完成声明都不能证明交付正确。

## 读取状态和输出

先读取 JSON 顶层字段：`ok`、`command`、`instance`、`reused`、`status`、`error_code`、`error`；`summon`/`run` 还可能带 `data.warnings`（例如 `cwd_shared:<name>`，见下方"复用、并行与审查"）。

```bash
agentmux inspect 编码助手-A --json
agentmux list --json
agentmux capture 编码助手-A
agentmux capture 编码助手-A --history 120
agentmux capture 编码助手-A --json
agentmux capture 编码助手-A --trace --json
agentmux capture 编码助手-A --new --json
```

- **默认用不带 `--json` 的 `capture`**，它只打印聚合后的内容，通常几十到几百字节。
- **`capture --json` 默认已经精简**：只有 `content`、`usage`、`turns`、`last_error`、`next_cursor` 等状态字段，不含逐条消息轨迹。需要消息轨迹时加 `--trace`；`--raw`、显式 `--history`、`--since`、`--new` 中任意一个也会带出消息轨迹（各自已经是明确的详情请求）。
- TUI harness 的 `--history` 是屏幕行数，按需给值，不要习惯性写 120；结构化 harness 下 `--history` 是消息条数上限，默认 20。
- **反复观察同一个长任务时用 `capture --new`**：游标由 agentmux 记在实例上并自动前移，不用来回传递；等价的显式写法是记下 `data.next_cursor` 再传回 `--since`。没有新增时返回空 `messages`。TUI harness 不支持 `--new`/`--since`。

把 `data.content` 作为 `capture --json` 的主要输出。先检查 `data.scope`：默认 `--scope current`（TUI harness 返回当前屏幕和可选历史行；结构化 harness 返回当前或最近 turn），`--scope session` 读取结构化 harness 的已记录会话（TUI harness 仍按屏幕和历史行处理）。

结构化 harness 各自额外提供的协议字段见[harness 内部差异参考](references/harness.md)。

判断状态时使用 `inspect --json`、`list --json` 或 `wait` 返回的 `status`，不要依赖仅供观察的 `pane_title`：

- `idle`：可接收下一条任务指令；`codex-cli-execjson` 此时 `process_id: 0` 正常。
- `busy`：当前或最近的工作仍在进行；TUI 状态可能在 TTL 后退化，结构化状态来自协议或进程。
- `exited`：实例已被停止。
- `lost`：运行时状态缺失或损坏；先检查再决定是否重新创建。

## 等待、输入与中断

只关心完成状态时用 `wait`；需要输出细节时用 `wait --collect` 或 `capture`：

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
agentmux wait 分片-A 分片-B 分片-C --mode any --timeout 5m --collect --json
agentmux wait 分片-A 分片-B 分片-C --mode all --timeout 5m --json
```

- `--mode any`：任意一个完成就返回，用于「先处理最先完成的分片」，不被最慢的挡住。
- `--mode all`（默认）：全部完成或超时才返回。
- `--collect`：对每个已完成的实例顺带做一次精简 `capture`（单实例、多实例都支持），并行分片因此不用再对每个分片单独调一次 `capture`。
- 多实例时读 `data.done`、`data.pending`、`data.failed` 和 `data.instances`；某个实例失败（含 `--collect` 读取失败）只记在它自己身上，不影响其他结果。
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

支持的按键为 `Enter`、`C-c`、`Escape`、`Up`、`Down`、`Tab`。结构化 harness 只有 `C-c` 有效果，其他按键是 no-op。

## 故障恢复

先读 `error_code`，按下表执行；不要凭错误文本猜测。撞到陌生错误码，或需要处置说明时查[错误码参考](references/errors.md)。

| `error_code` | 含义 | 下一步 |
| --- | --- | --- |
| `instance_not_found` | 实例不存在或已被清理 | `agentmux list --json`，再决定是否 `summon` |
| `process_not_running` / `session_not_found` | 实例已停止或会话缺失 | `inspect --json` 读墓碑的 `end_reason`、`last_error`，再决定是否同名 `summon` 重建 |
| `instance_busy` | `run`/`prompt --wait-if-busy` 在预算内等到实例仍然忙 | 加大超时预算，或换一个实例名 |
| `execjson_instance_busy` | 不带 `--wait-if-busy` 直接 `prompt` 撞上正在跑的 turn | 改用 `run` 或 `prompt --wait-if-busy`；不要立即重试，也不要用 `halt` 解锁 |
| `invalid_arguments` | 参数错误；也包括角色的 `model`/`effort` 与它的 harness 不匹配（例如给 `gemini-cli` 设 effort） | 按提示修正；实例名必须写在所有 flag 之前；`--effort` 只接受 `off`/`minimal`/`low`/`medium`/`high`/`xhigh`/`max` |
| `tmux_unavailable` / `config_*` / `registry_*` | 环境或配置问题 | 先跑 `agentmux doctor --json` 定位 |

## 复用、并行与审查

使用描述性实例名，不要使用 `worker1` 一类泛化名称。并行分片使用共同前缀和范围后缀，例如 `wiki审核-Q1to5`、`wiki审核-Q6to10`。

并行的标准形态是「`run --detach` 逐个发出，再统一 `wait --collect`」：

```bash
agentmux run --template builder --name wiki审核-Q1to5 --cwd /wt/a --prompt "..." --detach --json
agentmux run --template builder --name wiki审核-Q6to10 --cwd /wt/b --prompt "..." --detach --json
agentmux wait wiki审核-Q1to5 wiki审核-Q6to10 --mode any --timeout 5m --collect --json
```

用 `--mode any` 先拿到最先完成的分片并开始验收，剩下的继续等；`--collect` 让完成的分片直接带回内容，不用再单独 `capture`。

同一目标且历史仍然相关时复用实例。切换到无关任务，或同一问题连续两次纠偏仍失败时，创建新实例，并在新的首次任务指令中吸收已经确认的证据和约束。连续纠偏失败往往说明档位不够而不是指令不够：换更强的角色（例如 `builder-hard`），或对同一角色加 `--effort xhigh`/更强的 `--model` 新建实例。注意 `--model`/`--effort` 对复用到的实例无效，必须用新的实例名。

Agentmux 隔离的是 Agent 运行实例，不是仓库文件。相同 `cwd` 的实例会共享工作树、Git 状态和构建产物；`summon`/`run` 在目标 `cwd` 已有其他存活实例时会在 `data.warnings` 里给出 `cwd_shared:<name>`（不阻断，只提醒）。不要让多个写入型 Agent 同时修改同一个 checkout；并行写入时为每个实例创建独立 worktree，并传入不同的 `--cwd`。无法隔离工作目录时只保留一个写入者，其他实例只基于稳定快照进行调查或审查。

对高风险改动或长时间自主执行的任务，用 `reviewer` 角色的新实例独立审查稳定的 diff——它刻意与实现者用不同的模型家族，避免同源盲区。向审查者提供原始完成标准和变更范围，要求只报告影响正确性、明确需求或安全性的缺口，不追逐纯风格偏好。只读角色（`planner`、`reviewer`）通常带 harness 级别的只读约束，因此可以和写入者共享 `cwd`；`cwd_shared` 警告在这种组合下是预期的。

## 交付验收

外部 Agent 报告完成后：

1. 用 `capture` 读取它声称完成的内容、检查命令和阻塞项。
2. 直接读取预期文件、diff 或产物。
3. 亲自运行与风险相称的测试、构建、lint、类型检查或视觉检查。
4. 对照原始完成定义判断接受、发送证据化纠偏，或改用新实例审查。

要求外部 Agent 自验，不能替代编排 Agent 的独立验收。
