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
  max_instances: 8

templates:
  claude-code:
    description: Claude Code 通用编程智能体
    command: claude --dangerously-skip-permissions --model $MODEL
    model: anthropic/claude-sonnet-4.5
    system_prompt: ""
    prompt: ""
    cwd: .

  文档专家:
    description: 面向需求梳理、设计文档与说明文档的专家
    command: codex --model $MODEL
    model: openai/gpt-5.4
    system_prompt: 你负责生成清晰、可执行、结构稳定的技术文档。
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
```

字段：

1. `busy_ttl_ms`

规则：

1. 可选，默认为 `30000`（30 秒）
2. 实例在 `busy` 状态超过此时间后自动退化为 `idle`
3. 设为 `0` 表示禁用自动退化

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
max_instances: 8
```

规则：

1. 可选
2. 若设置，则用于限制实例总数
3. 第一版可实现，也可先只校验不强制

---

## 5. `templates`

`templates` 是角色模板集合。

语法：

```yaml
templates:
  <template-name>:
    ...
```

其中 `<template-name>` 支持 UTF-8 和 kebab-case，例如：

1. `claude-code`
2. `codex-cli`
3. `文档专家`

---

## 6. Template 字段

每个模板支持以下字段：

1. `description`
2. `command`
3. `model`
4. `system_prompt`
5. `prompt`
6. `cwd`
7. `shell`
8. `env`
9. `harness_type`

### 6.1 `description`

类型：

```yaml
description: 面向复杂编码与调试任务的通用专家
```

规则：

1. 可选但建议提供
2. 用于 `template list` 展示

### 6.2 `command`

类型：

```yaml
command: codex --model $MODEL
```

规则：

1. 必填
2. 这是启动 harness 的 shell 命令
3. 支持白名单变量替换

支持变量：

1. `$MODEL`
2. `$CWD`
3. `$INSTANCE`
4. `$TEMPLATE`

说明：

1. 不做任意环境变量插值
2. 只做上述白名单替换

### 6.3 `model`

类型：

```yaml
model: openai/gpt-5.4
```

规则：

1. 可选
2. 格式建议为 `<provider>/<model_name>`
3. 常见值：
   - `openai/gpt-5.4`
   - `anthropic/claude-sonnet-4.5`

### 6.4 `system_prompt`

类型：

```yaml
system_prompt: 你是编程专家，优先阅读上下文、定位根因、直接给出可执行修改。
```

规则：

1. 可为空
2. 作为首次消息前缀文本
3. 第一版不映射成 harness 的原生 system role

### 6.5 `prompt`

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

### 6.6 `cwd`

类型：

```yaml
cwd: .
```

规则：

1. 可选
2. 作为模板默认工作目录

### 6.7 `shell`

类型：

```yaml
shell: /bin/bash -lc
```

规则：

1. 可选
2. 若模板未设置，则继承 `defaults.shell`

### 6.8 `env`

类型：

```yaml
env:
  TERM: xterm-256color
```

规则：

1. 可选
2. 与 `defaults.env` 做浅合并

### 6.9 `harness_type`

类型：

```yaml
harness_type: claude-code
```

规则：

1. 可选
2. 若模板未设置，则继承 `defaults.harness_type`
3. 当前内建识别 `claude-code`、`codex-cli`、`gemini-cli`
4. 这三类 harness 启用基于 tmux `pane_title` 的精确 idle 检测
5. 对 `wait` 命令，这三类 harness 可走轻量 pane 元信息轮询，不必反复 `capture-pane`
6. 未知值保留在实例元数据中，但行为回退到通用模式

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
3. `system_prompt` 直接覆盖
4. `prompt` 直接覆盖
5. `cwd` 直接覆盖
6. `shell` 直接覆盖
7. `harness_type` 直接覆盖
8. `env` 浅合并

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
5. `harness_type`
6. `system_prompt`
7. `cwd`
8. `command`
9. `shell`
10. `env`
11. `status`
12. `pane_title`
13. `first_prompt_sent`
14. `created_at`
15. `updated_at`
16. `last_activity_at`

---

## 11. 校验规则

加载配置时至少做以下校验：

1. `version` 必填且为 `1`
2. `templates` 必须存在且至少有一个模板
3. 每个模板必须有 `command`
4. `capture.history`、`capture.stable_ms`、`capture.poll_ms` 必须为非负整数
5. `max_instances` 若存在必须为正整数
6. 模板名不能为空
7. `model` 若存在应包含 `/`

---

## 12. 推荐实践

### 12.1 推荐把模板按“角色”命名

推荐：

1. `claude-code`
2. `codex-cli`
3. `文档专家`

不推荐：

1. `codex`
2. `claude`
3. `opencode`

原因：

1. harness 只是实现手段
2. 模板应该表达业务角色与默认行为

### 12.2 推荐把 harness 差异体现在 `command/model/harness_type`

例如：

```yaml
templates:
  codex-cli:
    command: codex --model $MODEL
    model: openai/gpt-5.4

  claude-code:
    command: claude --dangerously-skip-permissions --model $MODEL
    model: anthropic/claude-sonnet-4.5
    harness_type: claude-code
```

如果确实需要区分不同 harness，可以在角色名后追加标识，而不是直接退化成 harness 名称。
