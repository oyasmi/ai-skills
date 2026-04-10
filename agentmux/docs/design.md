# agentmux 设计说明书

## 1. 目标

`agentmux` 是一个面向 AI Agent 的命令行控制器。它使用独立的 `tmux` 作为终端运行时，让上层编排器可以稳定地创建、复用、观察和驱动 TUI/CLI Agent 实例。

核心目标：

1. 让 OpenClaw、Codex、Nanobot 一类编排型 Agent 能稳定操控 `codex`、`claude code`、`opencode`、`vim` 等终端程序。
2. 提供高层的“实例”抽象，而不是要求上层直接操作 `tmux session/window/pane`。
3. 保证输出格式简明、平坦、稳定，便于 Agent 消费。
4. 控制命令是一次性的 CLI，不引入常驻 daemon。
5. 即使控制命令退出，实例也继续在 `tmux` 中存活。

非目标：

1. 不实现终端仿真器。
2. 不接管各 Agent CLI 的内部协议。
3. 不追求覆盖所有 tmux 能力，只做 Agent 编排真正需要的核心集。

---

## 2. 总体架构

采用三层结构：

1. `CLI 层`
   - 参数解析
   - 文本/JSON 输出
   - 一次性命令入口
2. `Control 层`
   - 配置加载
   - 模板合并
   - 实例复用与生命周期控制
   - prompt 注入、抓屏、等待完成
3. `Runtime 层`
   - `tmux` 调用封装
   - 本地 registry 持久化
   - 状态修复与最小 reconcile

建议目录：

```text
agentmux/
├── cmd/agentmux/
├── internal/
│   ├── app/
│   ├── config/
│   ├── template/
│   ├── instance/
│   ├── tmuxctl/
│   ├── capture/
│   ├── naming/
│   └── output/
└── docs/
```

---

## 3. 关键约束

### 3.1 tmux 运行边界

`agentmux` 必须使用独立 tmux socket，不与用户自己的 tmux 环境混用。

固定约束：

1. socket 文件路径：`/tmp/agentmux.sock`
2. 调用 tmux 时统一显式传 `-S /tmp/agentmux.sock`
3. 默认不加载用户 `tmux.conf`，但允许通过配置显式开启
4. 不依赖用户已有 tmux server

原因：

1. 避免污染或误接管用户自己的 tmux session。
2. 避免用户配置中的 status bar、hooks、plugins、prefix 改动影响 TUI 行为。
3. 避免启动时加载复杂配置造成延迟和不确定性。

实现建议：

1. 启动 server 时显式指定 socket。
2. 当未开启用户配置加载时，使用 `tmux -f /dev/null -S ...`；当开启时，只传 `-S ...`。
3. 所有内部 tmux 调用都经由 `tmuxctl` 封装，禁止散落 shell 拼接。

### 3.2 实例与 tmux 的映射

固定为：

1. `1 instance = 1 tmux session`
2. 主交互 pane 固定为 `session:0.0`

第一版不支持多 pane 编排。

### 3.3 配置与状态路径

采用 XDG 路径：

1. 配置文件：`~/.config/agentmux/config.yaml`
2. 状态目录：`~/.local/state/agentmux/`
3. registry 文件：`~/.local/state/agentmux/instances.json`

---

## 4. 模板语义

这里的 `template` 不是 harness 模板，而是“具体 Agent 角色模板”。

例如：

1. `工作项管理助手`
2. `深度编码专家`
3. `文档专家`
4. `代码审查助手`

每个模板描述的是“一类 agent instance 的默认角色配置”，其中会引用具体 harness 命令，如 `codex`、`claude`、`opencode`。

因此模板包含两类信息：

1. 角色配置
   - 模板名
   - 默认 system prompt
   - 默认首次 prompt
   - 默认工作目录策略
2. harness 配置
   - 启动命令
   - model
   - shell
   - env

这比把模板直接命名成 `codex` 或 `claude` 更符合使用场景。

---

## 5. 名称设计

模板名和实例名都必须支持中文、英文、数字，以及常见连接符。

要求：

1. 配置中的 `template name` 允许中文。
2. CLI 中 `--template`、`--name` 接受 UTF-8 名称。
3. registry 以原始名称保存，不做强制英文化。
4. tmux session name 需要安全编码，不能直接等于原始名称。

因此需要区分两个字段：

1. `name`
   - 面向用户与 Agent 的显示名
   - 可中文
2. `session_id`
   - 面向 tmux 的安全标识
   - ASCII only
   - 内部生成，例如 `i_3f8ab2c1`

结论：

1. instance 的真实业务标识是 `name`
2. tmux session 使用内部 `session_id`
3. registry 维护 `name -> session_id` 映射

这样可以同时满足易用性与底层兼容性。

---

## 6. 核心对象模型

### 6.1 AgentTemplate

字段：

- `name`
- `description`
- `command`
- `model`
- `system_prompt`
- `prompt`
- `cwd`
- `shell`
- `env`

说明：

1. `command` 是 harness 启动命令，例如 `codex --model $MODEL`
2. `model` 是 `<provider>/<model_name>`
3. `system_prompt` 是首次消息前缀
4. `prompt` 是 `summon --prompt` 未显式覆盖时使用的首次任务文本
5. `cwd` 是默认工作目录

### 6.2 AgentInstance

字段：

- `name`
- `template`
- `session_id`
- `model`
- `system_prompt`
- `cwd`
- `command`
- `shell`
- `env`
- `status`
- `created_at`
- `updated_at`
- `last_activity_at`
- `first_prompt_sent`

状态值建议：

- `starting`
- `idle`
- `busy`
- `exited`
- `lost`

### 6.3 CaptureSnapshot

字段：

- `instance`
- `status`
- `cursor_x`
- `cursor_y`
- `width`
- `height`
- `history_lines`
- `content`
- `digest`
- `captured_at`
- `stable_for_ms`

---

## 7. 配置合并规则

优先级从低到高：

1. 内建默认值
2. `defaults`
3. `template`
4. CLI 参数

规则：

1. 标量字段直接覆盖。
2. `env` 做浅合并。
3. CLI 传入空字符串视为显式覆盖为空。
4. `cwd` 最终解析为绝对路径。

---

## 8. CLI 设计原则

你要求子命令尽量少、参数尽量少，因此第一版收敛为核心命令集：

1. `template list`
2. `list`
3. `summon`
4. `inspect`
5. `prompt`
6. `capture`
7. `attach`
8. `halt`

说明：

1. 去掉 `show`，统一用 `inspect`
2. 去掉 `watch`、`doctor`、`remove`、`rename`、`bootstrap`、`paste`、`logs`
3. 去掉独立 `keys` 子命令，特殊键能力并入 `prompt`
4. `capture` 与 `wait` 职责分离，抓屏与等待分别建模

这样命令面更小，更符合“先把核心做好”。

---

## 9. 子命令设计

### 9.1 `template list`

用途：

列出可用模板。

示例：

```bash
agentmux template list
agentmux template list --json
```

### 9.2 `list`

用途：

列出当前实例。

示例：

```bash
agentmux list
agentmux list --json
```

### 9.3 `summon`

用途：

创建或复用一个实例。

核心语义：

1. 同名实例存在时默认复用
2. 同名实例不存在时创建
3. `--prompt` 就是首次 prompt，若提供则应在实例启动后立即下发
4. 不再保留 `pending_prompt`
5. 不提供 `--prompt` 时，仅启动实例，不发送任何业务消息

示例：

```bash
agentmux summon --template 深度编码专家
agentmux summon --template 深度编码专家 --cwd ~/work/agentmux
agentmux summon --template 深度编码专家 --name 编码助手-A --prompt "先阅读项目并总结结构"
```

### 9.4 `inspect`

用途：

获取单个实例的详细信息。

示例：

```bash
agentmux inspect 编码助手-A
agentmux inspect 编码助手-A --json
```

### 9.5 `prompt`

用途：

向实例发送文本或特殊键。

设计约束：

1. 保留一个 `prompt` 命令，不再拆 `keys`
2. 同时支持文本与特殊键
3. 参数保持少量

示例：

```bash
agentmux prompt 编码助手-A --text "继续"
agentmux prompt 编码助手-A --key Enter
agentmux prompt 编码助手-A --key C-c
agentmux prompt 编码助手-A --text "继续修复测试"
```

### 9.6 `capture`

用途：

抓取实例屏幕文本。

设计约束：

1. 默认只返回纯文本
2. 支持向上带历史
3. 立即返回当前内容，不承担等待职责

示例：

```bash
agentmux capture 编码助手-A
agentmux capture 编码助手-A --history 120
agentmux capture 编码助手-A --history 160 --json
```

### 9.7 `attach`

用途：

人类进入 tmux 实时查看。

示例：

```bash
agentmux attach 编码助手-A
agentmux attach
```

无参数时：

1. 若当前是 TTY，则展示实例列表并允许选择
2. 非 TTY 环境直接报错

### 9.8 `halt`

用途：

默认优雅终止实例，必要时再强制结束。

示例：

```bash
agentmux halt 编码助手-A
agentmux halt 编码助手-A --timeout 8s
agentmux halt 编码助手-A --immediately
agentmux halt 编码助手-A --json
```

---

## 10. `summon` 详细流程

执行步骤：

1. 解析参数
2. 加载 config
3. 解析模板
4. 合并默认值、模板值、CLI 值
5. 计算实例名
6. 查询 registry
7. 若存在同名实例，则返回该实例，并在必要时补做状态修复
8. 若不存在，则创建 tmux session
9. 在目标 cwd 启动 command
10. 写入 registry
11. 若提供 `--prompt`，等待最短启动就绪后立即发送该次消息
12. 返回实例信息

同名复用规则：

1. 默认复用
2. 若调用时额外传入 `cwd/model/command/system_prompt` 等覆盖参数，而实例已存在，则不修改现有实例配置
3. 若复用时传入 `--prompt`，也要发送该消息
4. 返回结果中需明确标记 `reused: true|false`

这样可以保持语义简单，避免“复用时隐式改配置”导致不可预测。

---

## 11. 首次消息策略

首次消息只存在两种来源：

1. `summon --prompt`
2. 模板中的默认 `prompt`，且本次 `summon` 未显式覆盖为空

发送规则：

1. `system_prompt` 作为开场文本前缀
2. 新建实例时：
   - 若提供 `prompt`，则作为首次用户消息正文
   - 发送成功后 `first_prompt_sent=true`
3. 复用实例时：
   - 若提供 `prompt`，则只发送该次 `prompt`
   - 不重复发送 `system_prompt`
4. `summon` 完成启动或复用确认后立即发送

拼接格式建议：

```text
[SYSTEM]
<system_prompt>

[USER]
<prompt>
```

说明：

1. 第一版将 `system_prompt` 视为普通文本前缀，不追求不同 harness 的原生 system role 语义。
2. 后续如某些 harness 支持真正的 system 参数，再做增强。

---

## 12. 抓屏设计

### 12.1 抓屏来源

必须通过 `tmux capture-pane`，不读 stdout。

统一调用：

```bash
tmux -S /tmp/agentmux.sock capture-pane -p -J -S -<N> -t <session_id>:0.0
```

### 12.2 纯文本优先

第一版 `capture` 默认返回纯文本，不返回 ANSI。

原因：

1. 纯文本更适合 Agent 消费
2. ANSI 对多数编排决策无帮助
3. 保持返回结构简明

### 12.3 稳定性检测

单独提供 `wait` 子命令，等待职责不并入 `capture`。

定义屏幕稳定：

1. 连续多次抓屏内容摘要不变
2. 安静时间达到阈值，例如 `1500ms`

建议轮询策略：

1. 间隔 `250ms`
2. 根据 `digest` 判断变化
3. 在 `timeout` 内等待

---

## 13. 输入注入设计

### 13.1 文本输入

优先使用：

1. `tmux load-buffer`
2. `tmux paste-buffer`

而不是逐字符 `send-keys`。

### 13.2 特殊键输入

通过 `tmux send-keys` 支持：

1. `Enter`
2. `C-c`
3. `Escape`
4. `Up`
5. `Down`
6. `Tab`

第一版只支持少量白名单键名。

---

## 14. 输出格式

输出要求：

1. 简明
2. 平坦
3. 一致
4. 尽量避免深层嵌套

建议 JSON 统一结构：

```json
{
  "ok": true,
  "command": "inspect",
  "instance": "编码助手-A",
  "reused": false,
  "status": "idle",
  "data": {}
}
```

错误结构：

```json
{
  "ok": false,
  "command": "capture",
  "instance": "编码助手-A",
  "error_code": "instance_not_found",
  "error": "instance '编码助手-A' not found"
}
```

设计规则：

1. 公共字段尽量提到顶层
2. `data` 只放命令专有字段
3. 错误也走平坦结构

建议错误码：

- `config_invalid`
- `template_not_found`
- `instance_not_found`
- `tmux_unavailable`
- `session_not_found`
- `process_not_running`
- `capture_timeout`
- `invalid_key`

---

## 15. 配置文件草案

```yaml
version: 1

defaults:
  shell: /bin/bash -lc
  cwd: .
  env:
    TERM: xterm-256color
  capture:
    history: 120
    stable_ms: 1500
    poll_ms: 250

templates:
  深度编码专家:
    description: 面向复杂编码与调试任务的通用专家
    command: codex --model $MODEL
    model: openai/gpt-5.4
    system_prompt: 你是深度编码专家，优先阅读上下文、定位根因、直接给出可执行修改。
    prompt: ""
    cwd: .

  文档专家:
    description: 面向需求梳理、设计文档与说明文档的专家
    command: codex --model $MODEL
    model: openai/gpt-5.4
    system_prompt: 你负责生成清晰、可执行、结构稳定的技术文档。
    prompt: ""
    cwd: .

  工作项管理助手:
    description: 面向任务拆分、状态跟踪与交付节奏管理的助手
    command: claude --dangerously-skip-permissions --model $MODEL
    model: anthropic/claude-sonnet-4.5
    system_prompt: 你负责管理工作项，输出要短、准、可执行。
    prompt: ""
    cwd: .
```

---

## 16. 资源与复杂度控制

第一版只做最小必要能力：

1. 不做 daemon
2. 不做多 pane
3. 不做事件订阅
4. 不做原始 ANSI 返回
5. 不做自动修改已复用实例的配置
6. 不做复杂调度器

可保留一个简单并发上限：

1. `defaults.max_instances`
2. 超限时 `summon` 报错

但这项不是第一版阻塞项。

---

## 17. 恢复与一致性

需要处理的主要问题：

1. registry 有记录，但 tmux session 丢失
2. tmux session 存在，但实例进程已退出
3. `summon` 复用时，registry 与 tmux 状态不一致

建议策略：

1. `list` 和 `inspect` 时做轻量 reconcile
2. 若 session 不存在，则状态标记为 `lost`
3. 若 pane 进程已退出，则状态标记为 `exited`
4. `summon` 复用遇到 `lost` 状态时，按“重新创建并覆盖旧 registry”处理

---

## 18. 配套 SKILL 设计

需要提供一个供上层 Agent 安装的 skill，职责是教会它如何使用 `agentmux`。

skill 的重点：

1. 什么时候应该使用 `agentmux`
2. 如何选择模板
3. 如何执行最小编排循环
4. 如何优先使用 `--json`
5. 如何避免直接调用 `tmux`

最小工作流：

1. `agentmux list --json`
2. `agentmux summon --template ... --json`
3. `agentmux capture <instance> --json`
4. `agentmux prompt <instance> --text ... --json`
5. 重复 `capture -> 判断 -> prompt`

---

## 19. 已确认决策

根据当前讨论，以下内容已经冻结：

1. `1 instance = 1 tmux session`
2. 使用 XDG 配置路径
3. `summon --prompt` 就是首次 prompt，并应立即发送
4. 去掉 `pending_prompt`
5. 去掉 `bootstrap`
6. 默认同名复用
7. 使用独立 socket：`/tmp/agentmux.sock`
8. 默认不加载用户 `tmux.conf`，可通过配置显式开启
9. 增加 `inspect`
10. 输出尽量平坦一致
11. 模板是“角色模板”，不是 harness 名称
12. 模板名和实例名支持中文
13. 不做 daemon
14. `capture` 默认返回纯文本
15. 收敛子命令与参数规模

---

## 20. 下一步产出

基于这份设计，继续落三份规格文档：

1. `docs/cli-spec.md`
2. `docs/config-spec.md`
3. `docs/skill-spec.md`
