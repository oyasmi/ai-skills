# agentmux 配置规格

## 1. 路径与格式

主配置文件路径：

`~/.config/agentmux/config.yaml`

格式选择：

1. 第一版只支持 YAML
2. 文件编码为 UTF-8
3. 允许中文模板名、中文提示词、中文路径注释文本

原因：

1. 模板本质上是角色模板，人工维护会很多
2. YAML 对多行 prompt 与中文内容更友好

---

## 2. 顶层结构

顶层字段：

1. `version`
2. `defaults`
3. `templates`

示例：

```yaml
version: 1

defaults:
  tmux:
    socket: /tmp/agentmux.sock
    load_user_config: false
  shell: /bin/bash -lc
  cwd: .
  harness_type: ""
  env:
    TERM: xterm-256color
  capture:
    history: 120
    stable_ms: 1500
    poll_ms: 250
  max_instances: 12

templates:
  builder:
    description: |
      实现者。用于边界清楚、可验证的常规编码任务：同一轮要求实现、测试和自查。
      正确用法：给出目标、相关路径、必须保持的行为和可判真假的完成标准。
    command: claude --dangerously-skip-permissions
    model: sonnet
    effort: medium
    harness_type: claude-code-ndjson
    system_prompt: ""
    prompt: ""
    cwd: .

  reviewer:
    description: |
      独立审查者。用于高风险改动的验收，刻意与 builder 用不同的模型家族。
      正确用法：给出原始完成标准和变更范围，只报告影响正确性和安全性的缺口。
    command: codex exec --sandbox read-only --skip-git-repo-check
    model: ""
    effort: xhigh
    harness_type: codex-cli-execjson
    system_prompt: ""
    prompt: ""
    cwd: .
```

---

## 3. `version`

类型：

```yaml
version: 1
```

规则：

1. 必填
2. 第一版固定为整数 `1`

---

## 4. `defaults`

`defaults` 定义全局默认值。

支持字段：

1. `shell`
2. `cwd`
3. `env`
4. `tmux`
5. `status`
6. `capture`
7. `max_instances`
8. `harness_type`

### 4.1 `defaults.tmux`

类型：

```yaml
tmux:
  socket: /tmp/agentmux.sock
  load_user_config: false
```

字段：

1. `socket`
2. `load_user_config`

规则：

1. `socket` 默认为 `/tmp/agentmux.sock`
2. `load_user_config` 默认为 `false`
3. 当 `load_user_config=false` 时，`agentmux` 会以 `tmux -f /dev/null -S ...` 形式启动和控制 tmux
4. 当 `load_user_config=true` 时，`agentmux` 会读取用户默认的 tmux 配置文件

### 4.2 `defaults.status`

类型：

```yaml
status:
  busy_ttl_ms: 30000
  prompt_ack_ms: 5000
  tombstone_ttl_ms: 86400000
```

字段：

1. `busy_ttl_ms`
2. `prompt_ack_ms`
3. `tombstone_ttl_ms`

`busy_ttl_ms` 规则：

1. 可选，默认为 `30000`（30 秒）
2. 实例在 `busy` 状态超过此时间后自动退化为 `idle`
3. 设为 `0` 表示禁用自动退化

`prompt_ack_ms` 规则：

1. 可选，默认为 `5000`（5 秒）
2. 只作用于有 `pane_title` 状态信号的 TUI harness（`claude-code`、`codex-cli`、`gemini-cli`）
3. 发送文本 prompt 后，`agentmux` 最多等这么久，确认 harness 的 `pane_title` 真的切换到 busy 标记
4. 观察到切换即立即返回，因此正常情况下额外开销只有数百毫秒
5. 观察到切换后，本轮后续的 idle 信号立即可信；未观察到时回退到 `capture.stable_ms` 的保守静默窗口
6. 设为 `0` 表示不做确认（不推荐：`wait` 可能把上一轮遗留的 idle 标题误判为本轮完成）

`tombstone_ttl_ms` 规则：

1. 可选，默认为 `86400000`（24 小时）
2. 实例停止后保留为墓碑，供 `inspect` 和 `list --all` 排查
3. 墓碑不占用 `max_instances` 配额
4. 超过该时间后由下一次 `list` 或 `summon` 清除
5. 设为 `0` 表示永久保留

### 4.3 `defaults.shell`

类型：

```yaml
shell: /bin/bash -lc
```

规则：

1. 默认值建议为 `/bin/bash -lc`
2. 用于启动模板命令

### 4.4 `defaults.cwd`

类型：

```yaml
cwd: .
```

规则：

1. 可为相对路径或绝对路径
2. 实际创建实例时应解析为绝对路径

### 4.5 `defaults.env`

类型：

```yaml
env:
  TERM: xterm-256color
```

规则：

1. 键值均为字符串
2. 第一版建议至少显式设置 `TERM=xterm-256color`

### 4.6 `defaults.harness_type`

类型：

```yaml
harness_type: claude-code
```

规则：

1. 可选，默认空字符串
2. 用于声明模板对应的 agent harness 类型
3. 当前内建识别 `claude-code`、`codex-cli`、`gemini-cli`
4. 这三类 harness 会启用基于 tmux `pane_title` 的精确 idle 检测
5. 其他值或空值不会报错，而是回退到通用的内容稳定性与 TTL 路径

### 4.7 `defaults.capture`

类型：

```yaml
capture:
  history: 120
  stable_ms: 1500
  poll_ms: 250
```

字段：

1. `history`
2. `stable_ms`
3. `poll_ms`

规则：

1. `history` 是默认向上抓取历史行数
2. `stable_ms` 是默认稳定判定窗口
3. `poll_ms` 是轮询间隔

### 4.8 `defaults.max_instances`

类型：

```yaml
max_instances: 12
```

规则：

1. 可选，默认 `12`
2. 限制同时存活的实例数；超限时 `summon`/`run` 报 `config_invalid`（`max_instances exceeded`），
   而不是排队等待
3. 只统计存活实例。墓碑（已 `halt` 或已退出的实例）不占配额
4. `0` 表示不限制；负数在配置加载阶段报 `config_invalid`

---

## 5. `templates`

`templates` 是角色模板集合。

语法：

```yaml
templates:
  <template-name>:
    ...
```

其中 `<template-name>` 支持 UTF-8 和 kebab-case。模板名应该是角色名，例如：

1. `planner`
2. `builder`
3. `reviewer`
4. `文档专家`

---

## 6. Template 字段

每个模板支持以下字段：

1. `description`
2. `command`
3. `model`
4. `effort`
5. `system_prompt`
6. `prompt`
7. `cwd`
8. `shell`
9. `env`
10. `harness_type`

模板是「角色」而不是 harness 清单：`description`、`model`、`effort` 描述这个角色是谁、
多强、什么场景用它；`harness_type` 和 `command` 是实现它的技术细节。

### 6.1 `description`

类型：

```yaml
description: 面向复杂编码与调试任务的通用专家
```

规则：

1. 可选但强烈建议提供
2. 用于 `template list` 展示：文本表格只显示第一行，`template list --json` 返回完整内容
3. 内容应回答两个问题：什么场景用这个角色、用它时正确的姿势是什么。这是调用方在
   选人这一步唯一能读到的依据，因此支持多行（YAML `|` 块）

### 6.2 `command`

类型：

```yaml
command: claude --dangerously-skip-permissions
```

规则：

1. 必填
2. 这是启动 harness 的 shell 命令
3. 常规情况下只写 harness 本身和它的权限/沙箱选项：`model`/`effort` 由 agentmux 追加，
   协议参数（NDJSON flag、`resume`、`--session-id` 等）也由 agentmux 注入
4. 支持白名单变量替换，用于 agentmux 的注入规则不适用时自己安排参数位置

支持变量：

1. `$MODEL`
2. `$EFFORT`
3. `$CWD`
4. `$INSTANCE`
5. `$TEMPLATE`

说明：

1. 不做任意环境变量插值
2. 只做上述白名单替换
3. `$EFFORT` 展开为当前 harness 自己的档位写法（见 6.4），不是 agentmux 的原始取值
4. 命令里出现 `$MODEL`/`$EFFORT` 时，agentmux 不再自动追加对应 flag：占位符表示
   「我自己安排位置」，它优先于注入

### 6.3 `model`

类型：

```yaml
model: sonnet
```

规则：

1. 可选；留空表示交给目标 CLI 自己的配置决定
2. 取值格式由 harness 决定，不做统一约定：
   - `claude-code` / `claude-code-ndjson`：`claude --model` 的取值，例如 `opus`、`sonnet`、
     `haiku`，或完整名 `claude-sonnet-4-5`
   - `codex-cli` / `codex-cli-execjson`：`codex --model` 的取值，例如 `gpt-5.6-luna`
   - `pi-rpc`：`pi --model` 的 `provider/id` 形式，例如 `zai-coding-cn/glm-4.7`；
     `pi --list-models` 列出本机可用组合
   - `gemini-cli`：`gemini --model` 的取值
3. agentmux 按 harness 追加对应的 `--model`。命令里已经写了 `--model`/`-m`，
   或写了 `$MODEL` 占位符时不再注入
4. 为一个没有已知 model flag 的 `harness_type` 设置 `model`，会在 `summon` 和
   `doctor` 阶段报错，而不是被静默忽略；此时改用 `$MODEL` 占位符自己安排位置

### 6.4 `effort`

类型：

```yaml
effort: high
```

规则：

1. 可选
2. 取值（由弱到强）：`off`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max`；
   其他取值在配置加载阶段就报 `config_invalid`
3. 与 `model` 一起构成角色的强度档位：规划和审查用最强模型配最高 effort，
   常规实现用中档，事实查询用低档
4. agentmux 把它翻译成各 harness 自己的写法，目标 CLI 词表更窄时向下夹取：

   | agentmux | `claude --effort` | `pi --thinking` | `codex -c model_reasoning_effort=` |
   | --- | --- | --- | --- |
   | `off` | `low`（夹取） | `off` | `none` |
   | `minimal` | `low`（夹取） | `minimal` | `low`（夹取） |
   | `low` | `low` | `low` | `low` |
   | `medium` | `medium` | `medium` | `medium` |
   | `high` | `high` | `high` | `high` |
   | `xhigh` | `xhigh` | `xhigh` | `xhigh` |
   | `max` | `max` | `max` | `max` |

5. 夹取原因：`claude --effort` 只接受 `low..max`，没有更弱的档；`codex` 虽然有
   `minimal`，但哪些模型支持 `minimal` 是逐模型的，不支持时直接返回 400，
   因此统一夹到 `low`。想强行使用时在 `command` 里直接写
   `-c model_reasoning_effort=minimal`，注入会自动让位
6. `gemini-cli` 没有 thinking 开关：给它设 `effort` 会在 `summon`/`doctor` 阶段报错
7. 命令里已经写了 `--effort`/`--thinking`/`-c model_reasoning_effort=`，或 pi 的
   model 形如 `<id>:<thinking>`，或写了 `$EFFORT` 占位符时不再注入
8. `agentmux doctor` 会标出发生夹取的模板；`agentmux inspect <名称>` 的 `command`
   显示实际启动的命令，`effort` 显示角色声明的原始档位

### 6.5 `system_prompt`

类型：

```yaml
system_prompt: |
  你是编程专家，优先阅读上下文、定位根因、直接给出可执行修改。
```

规则：

1. 可为空；支持多行（YAML `|` 块）
2. **它永远是追加，不会替换 harness 自己的系统提示。** 具体注入方式按 `harness_type` 分两类：

   | harness_type | 注入方式 | 生效范围 |
   | --- | --- | --- |
   | `claude-code-ndjson` | 原生 `claude --append-system-prompt` | 整个会话，含 `resume` 后的所有 turn |
   | `pi-rpc` | 原生 `pi --append-system-prompt` | 整个会话 |
   | `codex-cli-execjson` | 文本前缀 `[SYSTEM]\n…\n\n[USER]\n…` | **只拼在首条 prompt 前面**，后续 turn 不再重复 |
   | `claude-code` / `codex-cli` / `gemini-cli`（TUI） | 同上，文本前缀 | **只拼在首条 prompt 前面** |

3. 因此给走文本前缀的 harness 写 `system_prompt` 时，正文要自己声明「对本轮及之后每一轮
   指令持续生效」；否则模型容易把它当成只约束第一条任务的一次性说明
4. `command` 里如果已经自己写了系统提示 flag，agentmux 不再注入：
   `claude-code-ndjson` 检查 `--system-prompt`、`--system-prompt-file`、`--append-system-prompt`、
   `--append-system-prompt-file`；`pi-rpc` 检查 `--system-prompt`、`--append-system-prompt`
5. 内容应该只写这个角色跨任务不变的东西：工作方式、硬边界、报告格式。具体任务的目标、
   路径和完成标准属于 `prompt`，不要写进这里
6. 已存在的实例保留 `summon` 时解析出的值：改配置只影响新建实例，`inspect --json` 的
   `system_prompt` 显示该实例实际生效的内容

### 6.6 `prompt`

类型：

```yaml
prompt: 先阅读项目并总结结构。
```

规则：

1. 可为空
2. 表示模板默认首次 prompt
3. 若 `summon --prompt` 提供了值，则覆盖模板值
4. 若模板值不为空且本次 `summon` 未显式给空，则新建实例时自动下发
5. 复用实例时，模板中的 `prompt` 不会自动再次发送；只有本次显式传入 `summon --prompt` 才发送

### 6.7 `cwd`

类型：

```yaml
cwd: .
```

规则：

1. 可选
2. 作为模板默认工作目录

### 6.8 `shell`

类型：

```yaml
shell: /bin/bash -lc
```

规则：

1. 可选
2. 若模板未设置，则继承 `defaults.shell`

### 6.9 `env`

类型：

```yaml
env:
  TERM: xterm-256color
```

规则：

1. 可选
2. 与 `defaults.env` 做浅合并

### 6.10 `harness_type`

类型：

```yaml
harness_type: claude-code
```

规则：

1. 可选
2. 若模板未设置，则继承 `defaults.harness_type`
3. 当前内建识别 `claude-code`、`codex-cli`、`gemini-cli` 三类 TUI harness
4. 这三类 harness 启用基于 tmux `pane_title` 的精确 idle 检测
5. 对 `wait` 命令，这三类 harness 可走轻量 pane 元信息轮询，不必反复 `capture-pane`
6. 另识别三类结构化 harness，它们不使用 tmux：
   - `claude-code-ndjson`：一个长驻的 Claude Code `stream-json` 进程承载所有 turn
   - `codex-cli-execjson`：每个 turn 拉起一个 `codex exec --json` 进程，多轮靠 `resume <thread_id>` 串联
   - `pi-rpc`：一个长驻的 `pi --mode rpc` 进程承载所有 turn，靠带内 JSONL 命令/事件驱动，`agent_settled` 事件作为 idle 信号
7. `codex-cli-execjson` 的 `command` 必须是只带父级 flag 的 `codex exec` 前缀；`resume`、`--json`、`-` 由 agentmux 注入，`--ask-for-approval`、`--ephemeral`、`--json`、`-o`、管道与重定向会在 `summon` 阶段被拒绝
8. `pi-rpc` 的 `command` 为 `pi` 前缀（可含 `--model $MODEL`）；`--mode rpc`、`--session-id`、`--append-system-prompt` 由 agentmux 注入。`--session-id` 兼作新建与 resume（相同 cwd + 相同 id 即恢复既有会话）
9. 未知值保留在实例元数据中，但行为回退到通用模式

---

## 7. 合并规则

优先级从低到高：

1. 内建默认值
2. `defaults`
3. 模板字段
4. CLI 显式参数

逐字段规则：

1. `command` 直接覆盖
2. `model` 直接覆盖
3. `effort` 直接覆盖
4. `system_prompt` 直接覆盖
5. `prompt` 直接覆盖
6. `cwd` 直接覆盖
7. `shell` 直接覆盖
8. `harness_type` 直接覆盖
9. `env` 浅合并

空字符串规则：

1. 空字符串是合法值
2. CLI 显式传空值表示覆盖为空，而不是“忽略”

---

## 8. 名称与内部标识

### 8.1 模板名

模板名允许中文。

例如：

```yaml
templates:
  claude-code:
    ...
```

### 8.2 实例名

实例名允许中文。

例如：

1. `编码助手-A`
2. `文档专家-项目甲`

### 8.3 tmux session 标识

tmux session 不直接使用模板名或实例名。

内部生成：

```text
i_<random-or-hash>
```

例如：

1. `i_3f8ab2c1`
2. `i_52c1f0de`

这样可以避免中文与特殊字符对 tmux 的兼容风险。

---

## 9. tmux 默认运行参数

这些参数属于 `defaults.tmux` 的默认值：

1. socket 文件默认值：`/tmp/agentmux.sock`
2. `load_user_config` 默认值：`false`

理由：

1. 独立 socket 仍然是运行边界，避免和用户自己的 tmux server 混用
2. 默认不读取用户 `tmux.conf`，可以保持 agent 运行环境更稳定、更可预测
3. 同时保留显式开启入口，兼顾用户个性化需求

---

## 10. registry 数据建议

虽然不属于用户配置，但它和配置语义直接相关，因此在这里固定字段。

建议 `instances.json` 中每项至少包含：

1. `name`
2. `template`
3. `session_id`
4. `model`
5. `effort`
6. `harness_type`
7. `system_prompt`
8. `cwd`
9. `command`
10. `shell`
11. `env`
12. `status`
13. `pane_title`
14. `first_prompt_sent`
15. `created_at`
16. `updated_at`
17. `last_activity_at`

---

## 11. 校验规则

加载配置时至少做以下校验：

1. `version` 必填且为 `1`
2. `templates` 必须存在且至少有一个模板
3. 每个模板必须有 `command`
4. `capture.history`、`capture.stable_ms`、`capture.poll_ms` 必须为非负整数
5. `max_instances` 若存在必须为非负整数（`0` 表示不限制）
6. 模板名不能为空
7. `effort` 若存在必须是 `off`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max` 之一
8. `model` 的格式不做统一校验：取值合法性由目标 harness 决定（见 6.3）

---

## 12. 推荐实践

### 12.1 模板名是角色名

推荐：

1. `planner`
2. `builder`
3. `reviewer`
4. `scout`
5. `文档专家`

不推荐：

1. `codex`
2. `claude`
3. `pi-rpc`

原因：

1. harness 只是实现手段，调用方在选人这一步不该关心用哪个 CLI
2. 模板要表达的是「什么场景用它、用它时正确的姿势」，这只有角色能表达
3. 换 harness 是配置改动，不该连带改掉所有调用点的模板名

唯一值得按 harness 命名的例外是「需要人 attach 的终端实例」：那时 TUI 本身就是需求。

### 12.2 用 `model` + `effort` 表达角色强度，而不是复制一堆 harness 模板

例如：

```yaml
templates:
  planner:
    description: |
      规划者。复杂或高风险改动先出方案再动手；只授权调查和规划。
    command: codex exec --sandbox read-only --skip-git-repo-check
    effort: max
    harness_type: codex-cli-execjson

  builder:
    description: |
      实现者。边界清楚、可验证的常规编码任务，同一轮实现 + 测试 + 自查。
    command: claude --dangerously-skip-permissions
    model: sonnet
    effort: medium
    harness_type: claude-code-ndjson

  scout:
    description: |
      侦察兵。便宜快速的事实查询，一次只问一个能被单一事实回答的问题。
    command: pi
    effort: low
    harness_type: pi-rpc
```

三个角色的差别是「多强」和「什么场景用」，不是「用哪个 CLI」。同一个角色换 harness
时只改 `command` 和 `harness_type`，`model`/`effort` 的语义由 agentmux 负责翻译。

### 12.3 只读角色用 harness 自己的硬约束，不要只靠 system_prompt

`planner` 和 `reviewer` 不该改工作树。`codex exec --sandbox read-only` 是进程级约束，
比在 `system_prompt` 里写"不要修改文件"可靠得多；有了它，只读角色可以和写入型角色
共享同一个 `cwd`。
