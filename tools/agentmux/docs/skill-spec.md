# agentmux Skill 规格

## 1. 目标

`agentmux` 需要配套一个可安装 skill，供 OpenClaw、Codex、Nanobot 一类 Agent 学会正确使用它。

这个 skill 的职责不是解释 tmux 原理，而是提供稳定、节制、可执行的实例控制与任务委派规范。自然语言说明使用中文；命令、参数、JSON 字段、状态、错误码和 harness 名称保持原始英文标识。

skill 的目标：

1. 教会 Agent 何时使用 `agentmux`
2. 教会 Agent 如何选择模板与实例
3. 教会 Agent 用最小循环驱动外部 Agent 实例
4. 强制 Agent 优先使用 `--json`
5. 禁止 Agent 直接操作 tmux
6. 教会 Agent 编写包含目标、上下文、边界和完成定义的首次任务指令
7. 教会 Agent 根据可验证证据追加、纠偏和验收
8. 避免并行实例在共享工作树中产生文件冲突

---

## 2. skill 目录结构

建议目录：

```text
../../../skills/agentmux/
├── SKILL.md
├── agents/
│   └── openai.yaml
└── references/
    └── prompting.md
```

主 `SKILL.md` 只保留每次委派都必须遵守的核心规则。复杂任务、连续纠偏、并行协作和独立审查所需的模板与反例放入 `references/prompting.md`，由 Agent 按需加载。

原因：

1. CLI 控制面较小，命令语义应留在主文件
2. 任务指令模板存在多种场景，不应占用每次调用的上下文
3. references 只增加一个直接链接的文件，避免深层导航和内容重复
4. 当前不需要 `scripts/` 或 `assets/`

---

## 3. 触发条件

以下场景应触发该 skill：

1. 用户明确提到 `agentmux`
2. 用户要求启动、复用、操控一个终端内的 Agent 实例
3. 用户要求管理多个 coding agent / TUI agent
4. 用户要求通过外部 CLI agent 间接完成任务

以下场景不应触发：

1. 普通本地代码修改
2. 不需要外部 Agent 的简单查询
3. 仅讨论 tmux 本身而非 `agentmux`

---

## 4. SKILL.md 要表达的核心规则

SKILL.md 中应明确写出以下规则：

1. 优先使用 `agentmux ... --json`
2. 不直接调用 `tmux`
3. 不假设实例不存在，先 `list`
4. 不假设模板存在，必要时先 `template list`
5. 人类要求实时查看时才使用 `attach`
6. `capture` 默认读取当前可观测输出；TUI 返回屏幕文本，结构化 harness 返回聚合内容
7. 使用 `summon` 时，要明确区分“新建”与“复用”
8. 每条首次任务指令应按需包含工作模式、目标、上下文、范围边界、完成定义和交付说明
9. 追加任务指令应基于 `inspect` 或 `capture` 的具体证据，只描述相对原任务的变化
10. 外部 Agent 的完成声明和自验不能替代编排 Agent 的直接验收
11. 长任务策略应集中写在“等待、输入与中断”章节，不要把同一规则在多个章节重复展开
12. 明确实例隔离不等于工作树隔离；多个写入者必须使用不同 worktree 和 `cwd`

---

## 5. 推荐工作流

skill 应把 `agentmux` 的典型使用流程压缩成固定套路：

### 5.1 查看可用模板

```bash
agentmux template list --json
```

### 5.2 查看现有实例

```bash
agentmux list --json
```

### 5.3 创建或复用实例

```bash
agentmux summon --template claude-code --name 编码助手-A --cwd /path --json
```

首次任务：

```bash
agentmux summon --template codex-cli-execjson --name 登录修复-A --cwd /path --prompt "工作模式：实现。修复登录超时后的错误重试；先阅读 AGENTS.md 和 internal/auth/；不要改动公开 API；完成后运行 go test ./internal/auth/... 并报告证据。" --json
```

复用并顺手发送一条消息：

```bash
agentmux summon --template codex-cli-execjson --name 登录修复-A --prompt "观察到：go test ./internal/auth/... 中 TestRetryTimeout 仍失败。需要修正：超时后只重试一次。保持公开 API 不变，并重新运行该测试。" --json
```

### 5.4 获取实例详情

```bash
agentmux inspect 编码助手-A --json
```

### 5.5 立即抓屏并判断

```bash
agentmux capture 编码助手-A --history 120 --json
```

### 5.6 继续发送指令

```bash
agentmux prompt 编码助手-A --text "观察到：TestRetryTimeout 仍失败。请修复该偏差，保持公开 API 不变，并重新运行聚焦测试。" --json
```

### 5.7 中断

```bash
agentmux prompt 编码助手-A --key C-c --json
```

---

## 5.8 等待策略

SKILL.md 应在“等待、输入与中断”章节集中说明耐心策略：

这个章节至少应包含：

1. 等待节奏：`1m, 1m, 3m, 5m, 1m, 1m, 3m, 5m, ...`
2. 单次等待上限：`5m`
3. `capture_timeout` 表示超时时仍在活动，不等于失败
4. 例外条件：用户明确要求、明确阻塞、明显无限循环或崩溃、任务约束要求立即纠偏
5. 打断升级路径：先一次 `C-c`，等 `10-15s` 验证，再决定是否 `halt`
6. 慢或仍为 `busy` 本身不是中断理由

---

## 6. Agent 决策准则

skill 应要求使用它的 Agent 遵守以下准则：

### 6.1 先观测再行动

在继续给实例发 prompt 之前，优先：

1. `inspect`
2. `capture`

避免盲发消息。

### 6.2 复用优先

如果用户提到已有实例名，优先复用该实例，而不是创建新的。

### 6.3 把任务指令写成紧凑契约

发给实例的消息应：

1. 明确工作模式和可观察目标
2. 提供会改变结果的路径、证据和已有决策
3. 说明修改边界、非目标和需要额外授权的动作
4. 给出可判断真假的完成定义和验证方法
5. 要求报告修改文件、检查结果、风险和阻塞项
6. 避免长篇重复上下文和无法验证的人设或泛化要求

复杂任务先调查或规划，再明确授权实现。追加任务指令先观察当前结果，只发送新的证据、所需修正、保持项和重新验证方法；不要只发送“继续”“修一下”或“再试试”。

### 6.4 遇到异常先观察，不要急于中断

长任务等待与中断策略应统一引用 `Patience & Polling Strategy`，不要在决策准则里再写一份细节版。

这里保留高层原则即可：

1. 慢不等于卡住
2. 先观察，再决定是否介入
3. 真要中断时，要有明确触发条件和升级路径

---

## 7. 最小编排循环

SKILL.md 应给出一个明确、可复用的循环：

1. `agentmux list --json`
2. 若实例不存在，则 `agentmux summon ... --json`
3. 根据目标、上下文、范围边界和完成定义组装任务指令
4. 用 `summon --prompt` 或 `prompt` 发送
5. 对长任务优先 `agentmux wait <instance> --timeout 1m --json`
6. 再按 `1m`、`3m`、`5m`，然后回到 `1m` 的循环节奏继续等待或穿插 `capture`
7. 分析 `status` 与 `content`，并直接检查文件、diff 和验证结果
8. 若有具体偏差，发送证据化追加指令；否则接受结果
9. 回到等待和验收

这就是第一版 `agentmux` 的标准 orchestrator loop。

补充：

1. 当调用方已经明确知道实例名，并希望“复用且发送一条消息”时，可以直接使用 `summon --prompt`
2. 当调用方需要把“实例管理”和“交互输入”明确分开时，使用 `prompt`
3. 当调用方需要等待工作完成时，应在 `capture` 前先执行 `wait`
4. 同一目标且历史仍相关时复用实例；无关任务或连续纠偏失败时使用新实例
5. 多个写入者并行时使用独立 worktree 和不同 `--cwd`

---

## 8. 输出消费约束

skill 应提醒使用它的 Agent：

1. 只依赖 JSON 字段，不依赖人类文本表格
2. 优先读取顶层平坦字段
3. `content` 是主要屏幕文本来源
4. `reused` 可用于判断是否新建
5. `status` 可用于判断当前实例是否可能还在运行

---

## 9. 错误处理约束

当命令失败时，Agent 应优先读取：

1. `error_code`
2. `error`

建议恢复策略：

1. `template_not_found`
   - 先 `template list --json`
2. `instance_not_found`
   - 先 `list --json`
   - 再决定是否 `summon`
3. `capture_timeout`
   - 视为“还在运行”的常见信号，不要急于中断
   - 继续按等待节奏观察，必要时再抓当前快照
4. `process_not_running`
   - `inspect`
   - 必要时重新 `summon`
5. `invalid_key`
   - 改用支持的白名单键名
6. `execjson_instance_busy`
   - 只出现在 `codex-cli-execjson`；该 prompt 未发出
   - 先 `wait`，再原样重发；不要立刻重试，也不要用 `halt` 解锁
7. `config_invalid`
   - 若发生在 `codex-cli-execjson` 的 `summon`，说明模板 `command` 不是一个只带父级 flag 的 `codex exec` 前缀

---

## 10. SKILL.md 内容建议

SKILL.md 应包含这些部分：

1. 基本规则和 harness 模型
2. 标准编排循环
3. 首次与追加任务指令的核心契约
4. 启动、观察、等待和中断规则
5. 常见错误恢复
6. 复用、并行、审查和交付验收

SKILL.md 不应包含：

1. 过多 tmux 原理说明
2. 大量与本工具无关的示例
3. 复杂变体说明
4. 在主文件中重复完整的场景模板

原因：

1. 这个 skill 应该短、硬、可执行
2. 上下文窗口应保留给实际任务
3. 主文件控制在约 300 行以内；详细模板通过一层 reference 渐进加载

---

## 11. `agents/openai.yaml` 建议

`agents/` 只放这一个文件，且它是 **skill 的接口元数据**，不是 harness 或 agent 的注册表。新增 harness type 时不要在这里加文件——没有任何东西会读取它们；应该改的是 `SKILL.md` 的行为规则，以及本文件的描述文案。

建议元数据语义：

1. `display_name`: `AgentMux`
2. `short_description`: 简洁表达“委派并管理外部 CLI coding agent”
3. `default_prompt`: 用一句中文说明选择 harness、创建或复用实例、发送有完成标准的任务指令并直接验收

`default_prompt` 必须明确提及 `$agentmux`。详细协议差异留在 `SKILL.md`，不要把 UI 元数据扩写成第二份操作手册。

---

## 12. 示例 SKILL 行为描述

下面这类文字适合放入 SKILL.md：

1. 当你需要启动、复用或操控一个外部终端 Agent 实例时，使用 `agentmux`。
2. 先用 `agentmux list --json` 查看现有实例，再决定是否 `summon`。
3. 首次任务指令应明确目标、相关上下文、范围边界和可验证的完成标准。
4. 优先使用 `--json`，不要解析人类表格输出。
5. 不要直接调用 `tmux`，除非用户明确要求调试底层问题。
6. 外部 Agent 声称完成后，直接检查实际文件、diff 和验证结果。

---

## 13. 后续扩展

如果未来 CLI 或委派工作流增加新能力，skill 的扩展顺序建议是：

1. 先补最小规则
2. 再补示例
3. 复杂或低频变体放入现有 reference
4. 只有重复出现且需要确定性执行时才增加脚本

当前不需要：

1. `scripts/`
2. `assets/`
3. 更多平行参考文件

`references/prompting.md` 已覆盖复杂任务指令、纠偏、并行和审查场景。只有出现清晰的新信息域时再拆分，避免增加导航和维护负担。
