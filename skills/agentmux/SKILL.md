---
name: agentmux
description: 使用 `agentmux` CLI 把任务委派给一个或多个外部 AI coding agent，并负责选角、分派、并行协调、持续跟进和交付验收。用户提到 `agentmux`、agent 委托或分派、智能体团队（agent team）、多智能体协作、按角色编排、调用或复用其他 CLI coding agent，或要求让另一个 Agent 调查、规划、实现、评审、验证、写文档时使用。也用于创建、修改和排查 agentmux 的角色模板、团队配置及运行实例。
---

# Agentmux

使用 `agentmux` 调用和管理外部 AI coding agent。你仍是编排者：负责理解用户目标、选择角色、提供任务契约、处理反馈，并独立验收最终结果。

## 核心规则

1. 只通过 `agentmux` 管理外部 Agent 实例。不要直接调用外部 coding CLI、`tmux` 或读取 harness 原始日志，除非用户明确要求调试底层实现；人类观察 headless 对话使用 `logs`。
2. 按任务职责选择模板，不要按模型或 harness 名称选。先用 `agentmux template list --json` 读取本机模板及其完整 `description`。
3. 只委派可与当前工作分离、且能独立验收的任务。默认只用一个最小足够角色；只有任务确实需要独立判断、并行分片或多阶段交付时才组合多个角色。
4. 单路一次性任务优先使用 `run`；需要保留新会话继续工作时显式加 `--keep`。只有需要异步并行或分步观察、纠偏时才拆成 `summon`、`prompt`、`wait` 和 `capture`。
5. 等待超时表示实例仍在工作，不表示失败。不要仅因耗时长或状态仍为 `busy` 就中断。
6. 多实例 `wait` 中，真正超时仍是 `ok: true`；但任一实例失败会使顶层 `ok: false` 和退出码非零，不能把失败当成超时处理。
7. 外部 Agent 的完成声明不是验收证据。直接检查文件、diff 和产物，并亲自运行与风险相称的验证。

## 标准流程

### 1. 检查环境和可用角色

首次使用、命令行为异常或不清楚本机配置时运行：

```bash
agentmux doctor --json
agentmux template list --json
agentmux list --json
```

- `doctor` 检查当前二进制、PATH、配置、模板命令和外部依赖。
- `template list --json` 返回完整角色说明；不要假设某个模板一定存在。
- `template list` 在配置文件尚不存在时也能读取内置模板，且不会为这次只读查询创建配置或 state 文件。
- `list --json` 返回稳定的精简实例摘要，不包含 prompt、环境变量或 transport 内部字段；`--all` 还显示已停止的墓碑。
- `inspect --json` 返回诊断所需的稳定字段，但不暴露 `system_prompt` 和 `env`。
- JSON 和详细文本中的时间使用运行机器的本地时区；面向 Agent 时优先解析 JSON，不要假设时间是 UTC。

命令不存在时读取[安装与环境自检](references/install.md)。

### 2. 选择最小足够角色

优先以本机模板的 `description` 为准。常见角色语义如下：

| 角色 | 适用任务 |
| --- | --- |
| `planner` | 收敛需求、调查现状、设计方案、定义验收和拆解任务 |
| `builder` | 实现边界清楚的代码改动和单元测试；通常是开发任务的默认角色 |
| `reviewer` | 独立审查稳定的方案、diff、commit 或文档并给出证据化发现 |
| `verifier` | 复现问题、实际运行、冒烟、回归和分析运行数据 |
| `documenter` | 更新文档、变更记录、提交和最终交付摘要 |
| `worker` | 数据处理、对比分析、批量转换和专项报告等通用工作 |
| `scout` | 低成本、快速的单点事实查询 |

选择原则：

- 需求未收敛时先用 `planner`；边界清楚时直接用 `builder`。
- `reviewer` 负责独立挑错，`verifier` 负责用真实运行建立证据；二者不能互相替代。
- 审查高风险产物时，尽量让 `reviewer` 使用与产出者不同的模型家族。
- 只在需要同步权威文档、变更记录、commit 或交付摘要时加入 `documenter`。
- 某次任务更难时优先用 `--model` 或 `--effort` 临时升档，不要仅为强度差异新增角色模板。

设计或调整角色、团队模板时读取[角色与团队模板](references/team-templates.md)，并按需复用 [`development-team.yaml`](assets/development-team.yaml)。

### 3. 编写任务契约

把首次指令写成自足、可验收的任务契约。描述终点、约束和证据，不要替外部 Agent 规定每一步；只提供会改变结果的信息：

```text
工作模式：<调查 | 规划 | 实现 | 审查 | 验证 | 文档>
目标：<可观察的最终结果>

上下文：
- 先阅读当前目录适用的仓库指令。
- 相关路径或符号：<精确位置>
- 当前行为或证据：<复现、错误、已有决策>

范围与权限：
- <允许修改、必须保持和明确非目标>
- <需要额外授权的外部写入、破坏性或高影响动作>

完成标准与证据：
- <可判断真假的验收条件>
- 成功运行 `<验证命令>`。
- 重要产物写入 `<目标工作区中的路径>`，不要只留在 stdout 或回复文本中。

停止条件：
- 证据不足时报告最小缺口，不猜测。
- 满足完成标准后停止，不为润色继续搜索或扩大范围。

交付：报告修改文件、实际检查及结果、未验证内容、剩余风险和阻塞项；不要输出冗长的思考过程。
```

只保留适用于当前任务的字段，并让目标、范围、完成标准和停止条件彼此一致。优先给出准确路径、符号名、完整错误文本、复现输入和验证命令；删除泛化人设、重复警告、无效示例和过细流程。同一边界只写一次。较长的规格或任务契约写入文件，再用 `--prompt-file` 传递。

复杂或模糊任务、连续纠偏、独立审查、并行协作时读取[任务指令参考](references/prompting.md)。

### 4. 委派任务

委派前确认并保留实例名、工作目录、预期产物、验收方法和失败后的恢复方案；这些信息用于后续判断是继续等待、纠偏、复用还是新建实例。

单路一次性任务使用：

```bash
agentmux run --template <角色> --name <描述性名称> --cwd <工作目录> \
  --prompt-file <任务文件> --timeout 10m --json
```

短任务可改用 `--prompt "<任务契约>"`。`run` 会创建或复用实例、等待既有任务收尾、发送本次任务、等待并读取结果；新建实例成功完成后默认清理并留下墓碑。需要继续复用新实例时加 `--keep`；复用已有实例时 `run` 不会替调用方关闭它。

需要分步控制时使用：

```bash
agentmux summon --template <角色> --name <名称> --cwd <工作目录> --json
agentmux prompt <名称> --text "<任务契约>" --json
agentmux wait <名称> --timeout 3m --json
agentmux capture <名称>
```

需要并行分片时，先全部发出，再统一等待：

```bash
agentmux run --template builder --name 分片-A --cwd /worktree/a --prompt-file /tmp/task-a.md --detach --json
agentmux run --template builder --name 分片-B --cwd /worktree/b --prompt-file /tmp/task-b.md --detach --json
agentmux wait 分片-A 分片-B --mode any --timeout 5m --collect --json
```

用 `--mode any` 先验收最先完成的分片；需要等全部完成时用 `--mode all`。`wait` 的实例名和 flags 可以混排。

### 5. 观察、等待和纠偏

```bash
agentmux inspect <名称> --json        # 状态和元数据
agentmux wait <名称> --timeout 3m --json
agentmux capture <名称>               # 聚合文本，默认首选
agentmux capture <名称> --new --json  # 只读结构化 harness 的新增消息
agentmux logs <名称>                  # 结构化 harness 的完整可读对话
agentmux logs <名称> --follow         # 先读历史，再跟随新增事件
```

`wait` 的实例名和 flags 可以按常见命令行习惯混排，例如
`agentmux wait --timeout 3m <名称> --json`。`attach` 是交互式命令：TUI
实例连接 tmux；headless 对话使用 `logs --follow`，原始事件调试才使用 attach。

读取 `wait` 结果时：

- `ok: true`、`timed_out: false`、`status: idle`：本轮已经结束。
- `ok: true`、`timed_out: true`、`status: busy`：仍在工作；继续等待。
- `ok: false`：实例或运行环境确实失败；按 `error_code` 处理。

长任务按 `1m, 1m, 3m, 5m` 的节奏等待，之后重复；单次等待不超过五分钟。机器需要增量读取时使用 `capture --new`，人类需要完整进度时使用 `logs --follow`。

追加或重试前先用 `inspect` 和 `capture` 检查实例状态、既有输出与预期产物，避免把同一任务重复下发。追加指令只描述相对原任务的变化：

```text
观察到：<具体偏差、失败输出或新证据>
需要修正：<期望的具体变化>
保持不变：<不得回退的行为或文件>
重新验证：<命令或可观察检查>
```

发送时优先让忙实例先完成当前工作：

```bash
agentmux prompt <名称> --wait-if-busy 3m --text "<追加指令>" --json
```

不要只发送“继续”“修一下”或“再试试”，除非紧邻输出已经唯一确定下一步。

### 6. 验收交付

外部 Agent 报告完成后：

1. 确认重要产物已经写入目标工作区，而不只存在于 stdout 或 Agent 的回复中。
2. 用 `capture` 读取它声称完成的内容、检查命令和阻塞项。
3. 直接读取预期文件、diff、commit 或其他产物。
4. 亲自运行与风险相称的测试、构建、lint、类型检查、冒烟或视觉检查。
5. 对照原始完成标准决定接受、发送证据化纠偏，或创建新实例独立审查。

要求外部 Agent 自验是第一层证据，不能替代编排者的独立验收。

## 实例复用和并行隔离

- 同一目标且既有上下文仍有价值时复用实例；切换到无关任务、需要独立审查或旧上下文已被失败路径污染时创建新实例。
- 同一问题连续两次证据化纠偏仍失败时，创建新实例并提高 `--model` 或 `--effort`；把已确认事实、已排除方向和当前文件状态整理成新的任务契约。
- `--model` 和 `--effort` 只影响新建实例。复用同名实例不会改变其配置，因此升档时必须换实例名。
- Agentmux 隔离运行实例，不隔离仓库文件。同一 `cwd` 的实例共享工作树、Git 状态和构建产物。
- 只并行执行输入、文件所有权和验收方法彼此独立的分片。不要让多个写入者并发修改同一个 checkout；并行写入时为每个实例创建独立 worktree，并传入不同的 `--cwd`，无法隔离时只保留一个写入者。
- 角色名不决定是否写入。会落盘方案、报告、脚本或测试产物的 `planner`、`reviewer`、`verifier` 同样按写入者处理。

## 故障和中断

行为与文档不符时先运行 `agentmux doctor --json`，确认 PATH 中没有旧二进制，并检查模板依赖。命令失败时先读结构化返回的 `error_code`，陌生错误或配置问题读取[错误码参考](references/errors.md)。

不要为了“解锁”忙实例而停止它。只在用户要求、出现明确阻塞、明显循环或崩溃，或者必须立即纠偏时中断：

```bash
agentmux prompt <名称> --key C-c --json
agentmux inspect <名称> --json
agentmux halt <名称> --timeout 8s --json
```

发送 `C-c` 后等待 10–15 秒再检查。优先使用普通 `halt` 或 `halt --timeout`；仅在优雅停止已经无意义时使用 `halt --immediately`。

只有需要排查输入、完成判定、协议字段、model/effort 翻译或 TUI 行为时，才读取 [harness 差异参考](references/harness.md)。日常委派不要展开这一层。

## 按需读取的参考

- [安装与环境自检](references/install.md)：命令不存在、PATH 或依赖异常。
- [任务指令参考](references/prompting.md)：复杂任务、规划、评审、纠偏和并行交接。
- [角色与团队模板](references/team-templates.md)：创建或修改角色、团队配置和交接契约。
- [Harness 差异参考](references/harness.md)：调试协议、状态语义或 model/effort 映射。
- [错误码参考](references/errors.md)：处理不熟悉的错误码和配置错误。
