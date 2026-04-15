# agentmux Skill 规格

## 1. 目标

`agentmux` 需要配套一个可安装 skill，供 OpenClaw、Codex、Nanobot 一类 Agent 学会正确使用它。

这个 skill 的职责不是解释 tmux 原理，而是提供稳定、节制、可执行的操作规范。

skill 的目标：

1. 教会 Agent 何时使用 `agentmux`
2. 教会 Agent 如何选择模板与实例
3. 教会 Agent 用最小循环驱动外部 Agent 实例
4. 强制 Agent 优先使用 `--json`
5. 禁止 Agent 直接操作 tmux

---

## 2. skill 目录结构

建议目录：

```text
skills/agentmux/
├── SKILL.md
└── agents/
    └── openai.yaml
```

第一版不需要额外脚本与参考文件。

原因：

1. 当前 CLI 面较小
2. skill 重点是行为约束，不是复杂资源分发

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
6. `capture` 默认拿纯文本，不要求 ANSI
7. 使用 `summon` 时，要明确区分“新建”与“复用”
8. 长任务策略应集中写在单独的 `Patience & Polling Strategy` 章节，不要把同一规则在多个章节重复展开

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
agentmux summon --template claude-code --name 编码助手-A --cwd /path --prompt "先阅读项目并总结结构" --json
```

复用并顺手发送一条消息：

```bash
agentmux summon --template claude-code --name 编码助手-A --prompt "继续修复剩余失败测试" --json
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
agentmux prompt 编码助手-A --text "继续修复剩余失败测试" --json
```

### 5.7 中断

```bash
agentmux prompt 编码助手-A --key C-c --json
```

---

## 5.8 Patience & Polling Strategy

SKILL.md 应新增一个明确章节：`Patience & Polling Strategy`

这个章节至少应包含：

1. 等待节奏：`1m, 1m, 3m, 5m, 1m, 1m, 3m, 5m, ...`
2. 单次等待上限：`5m`
3. `2h` 内不要主动中断
4. 例外条件：用户明确要求、`capture` 显示崩溃、明显无限循环
5. 打断升级路径：先一次 `C-c`，等 `10-15s` 验证，再决定是否 `halt`
6. 心态校准语句，强调耐心而不是频繁打断

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

### 6.3 少量、明确的 prompt

发给实例的消息应：

1. 目标明确
2. 单轮信息适中
3. 避免长篇重复上下文

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
3. 对长任务优先 `agentmux wait <instance> --timeout 1m --json`
4. 再按 `1m`、`3m`、`5m`，然后回到 `1m` 的循环节奏继续等待或穿插 `capture`
5. 分析 `status` 与 `content`
6. 若需要推进，则 `agentmux prompt <instance> --text ... --json`
7. 回到等待或抓屏

这就是第一版 `agentmux` 的标准 orchestrator loop。

补充：

1. 当调用方已经明确知道实例名，并希望“复用且发送一条消息”时，可以直接使用 `summon --prompt`
2. 当调用方需要把“实例管理”和“交互输入”明确分开时，使用 `prompt`
3. 当调用方需要等待工作完成时，应在 `capture` 前先执行 `wait`

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

---

## 10. SKILL.md 内容建议

SKILL.md 应包含这些部分：

1. Skill 触发场景
2. 关键规则
3. 标准命令清单
4. 最小编排循环
5. 常见错误处理

SKILL.md 不应包含：

1. 过多 tmux 原理说明
2. 大量与本工具无关的示例
3. 复杂变体说明

原因：

1. 这个 skill 应该短、硬、可执行
2. 上下文窗口应保留给实际任务

---

## 11. `agents/openai.yaml` 建议

建议元数据语义：

1. `display_name`: `AgentMux`
2. `short_description`: 面向 AI Agent 的 tmux 实例控制技能
3. `default_prompt`: 指导 Agent 使用 `agentmux` 管理和驱动终端 Agent 实例

这一文件应与 `SKILL.md` 保持一致，不要额外扩展无关承诺。

---

## 12. 示例 SKILL 行为描述

下面这类文字适合放入 SKILL.md：

1. 当你需要启动、复用或操控一个外部终端 Agent 实例时，使用 `agentmux`。
2. 先用 `agentmux list --json` 查看现有实例，再决定是否 `summon`。
3. 发消息前先 `capture` 当前屏幕，避免重复发送。
4. 优先使用 `--json`，不要解析人类表格输出。
5. 不要直接调用 `tmux`，除非用户明确要求调试底层问题。

---

## 13. 后续扩展

如果未来 CLI 增加了新命令，skill 的扩展顺序建议是：

1. 先补最小规则
2. 再补示例
3. 最后再考虑是否需要脚本或 references

当前第一版不需要：

1. references/
2. scripts/
3. assets/

因为它们会增加维护负担，而当前信息密度还不足以证明值得引入。
