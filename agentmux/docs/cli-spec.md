# agentmux CLI 规格

## 1. 目标

本规格定义 `agentmux` 的命令行接口，目标是：

1. 子命令少
2. 参数少
3. 语义稳定
4. 输出适合 AI Agent 消费

第一版命令集固定为：

1. `template list`
2. `list`
3. `summon`
4. `inspect`
5. `prompt`
6. `capture`
7. `wait`
8. `attach`
9. `halt`

---

## 2. 全局约定

### 2.1 可执行文件

```bash
agentmux <subcommand> [flags]
```

### 2.2 全局参数

所有命令都支持：

```bash
--json
```

语义：

1. 输出 JSON 到 stdout
2. 错误也输出 JSON
3. 非 JSON 模式下输出简洁文本或表格

### 2.3 名称约定

以下名称均支持 UTF-8：

1. 模板名
2. 实例名

例如：

1. `深度编码专家`
2. `工作项管理助手`
3. `编码助手-A`

---

## 3. 输出约定

### 3.1 成功输出

统一顶层字段：

```json
{
  "ok": true,
  "command": "list",
  "instance": "",
  "reused": false,
  "status": "",
  "data": {}
}
```

字段规则：

1. `ok` 必填
2. `command` 必填
3. `instance` 适用于单实例命令，无则为空字符串或省略
4. `reused` 仅 `summon` 使用，其他命令可省略
5. `status` 适用于单实例命令
6. `data` 放命令专有内容

### 3.2 错误输出

```json
{
  "ok": false,
  "command": "capture",
  "instance": "编码助手-A",
  "error_code": "instance_not_found",
  "error": "instance '编码助手-A' not found"
}
```

---

## 4. 命令规格

## 4.1 `template list`

用途：

列出配置文件中的模板。

语法：

```bash
agentmux template list [--json]
```

文本输出建议列：

1. `NAME`
2. `MODEL`
3. `CWD`
4. `DESCRIPTION`

JSON 示例：

```json
{
  "ok": true,
  "command": "template list",
  "data": {
    "templates": [
      {
        "name": "深度编码专家",
        "model": "openai/gpt-5.4",
        "cwd": ".",
        "description": "面向复杂编码与调试任务的通用专家"
      }
    ]
  }
}
```

---

## 4.2 `list`

用途：

列出当前实例。

语法：

```bash
agentmux list [--json]
```

文本输出建议列：

1. `NAME`
2. `TEMPLATE`
3. `STATUS`
4. `MODEL`
5. `CWD`
6. `UPDATED`

说明：

1. `list` 是多实例状态查询接口
2. 当调用方还不确定实例名，或想批量扫描状态时，优先使用 `list`

JSON 示例：

```json
{
  "ok": true,
  "command": "list",
  "data": {
    "instances": [
      {
        "name": "编码助手-A",
        "template": "深度编码专家",
        "status": "idle",
        "model": "openai/gpt-5.4",
        "cwd": "/Users/me/work/project",
        "updated_at": "2026-03-20T10:00:00+08:00"
      }
    ]
  }
}
```

---

## 4.3 `summon`

用途：

创建或复用实例。

语法：

```bash
agentmux summon --template <template-name> [flags]
```

参数：

1. `--template <name>`
2. `--name <instance-name>`
3. `--cwd <path>`
4. `--model <provider/model>`
5. `--command <shell-command>`
6. `--system-prompt <text>`
7. `--prompt <text>`
8. `--json`

参数原则：

1. 只保留真正必要的覆盖项
2. 不提供 `--reuse`，因为默认就是复用
3. 不提供 `--bootstrap`

行为：

1. 实例名未提供时自动生成
2. 若同名实例已存在，则直接复用
3. 复用时不修改既有实例配置
4. 若本次指定了 `--prompt` 且实例是新建的，则发送首次消息
5. 若本次指定了 `--prompt` 且实例是复用的，则也发送该次消息
6. 复用实例时不重复发送既有 `system_prompt`

这一条的语义是：

1. `summon --prompt` 表示“本次调用要发送一条消息”
2. 新建时它是首次 prompt
3. 复用时它是继续对既有实例发一条 prompt

返回字段：

1. `instance`
2. `reused`
3. `status`
4. `data.template`
5. `data.model`
6. `data.cwd`

JSON 示例：

```json
{
  "ok": true,
  "command": "summon",
  "instance": "编码助手-A",
  "reused": false,
  "status": "idle",
  "data": {
    "template": "深度编码专家",
    "model": "openai/gpt-5.4",
    "cwd": "/Users/me/work/project"
  }
}
```

---

## 4.4 `inspect`

用途：

查看实例详情。

语法：

```bash
agentmux inspect <instance-name> [--json]
```

输出字段：

1. `name`
2. `template`
3. `status`
4. `model`
5. `cwd`
6. `command`
7. `session_id`
8. `created_at`
9. `updated_at`
10. `last_activity_at`
11. `first_prompt_sent`

说明：

1. `inspect` 是单实例状态查询接口
2. 若调用方只想知道当前 `idle/busy/exited/lost` 与相关元数据，优先使用 `inspect`

JSON 示例：

```json
{
  "ok": true,
  "command": "inspect",
  "instance": "编码助手-A",
  "status": "idle",
  "data": {
    "name": "编码助手-A",
    "template": "深度编码专家",
    "model": "openai/gpt-5.4",
    "cwd": "/Users/me/work/project",
    "command": "codex --model openai/gpt-5.4",
    "session_id": "i_3f8ab2c1",
    "first_prompt_sent": true,
    "created_at": "2026-03-20T09:58:00+08:00",
    "updated_at": "2026-03-20T10:00:00+08:00",
    "last_activity_at": "2026-03-20T10:00:00+08:00"
  }
}
```

---

## 4.5 `prompt`

用途：

向实例发送文本或特殊键。

语法：

```bash
agentmux prompt <instance-name> [flags]
```

参数：

1. `--text <text>`
2. `--key <key-name>`
3. `--enter`
4. `--json`

约束：

1. `--text` 与 `--key` 至少提供一个
2. 第一版 `--key` 一次只接受一个键
3. `--enter` 只对 `--text` 生效

支持的键名：

1. `Enter`
2. `C-c`
3. `Escape`
4. `Up`
5. `Down`
6. `Tab`

示例：

```bash
agentmux prompt 编码助手-A --text "继续"
agentmux prompt 编码助手-A --text "继续修复测试" --enter
agentmux prompt 编码助手-A --key Enter
agentmux prompt 编码助手-A --key C-c
```

JSON 示例：

```json
{
  "ok": true,
  "command": "prompt",
  "instance": "编码助手-A",
  "status": "busy",
  "data": {
    "sent_text": true,
    "sent_key": "",
    "enter": true
  }
}
```

---

## 4.6 `capture`

用途：

抓取实例当前屏幕文本。

语法：

```bash
agentmux capture <instance-name> [flags]
```

参数：

1. `--history <lines>`
2. `--json`

行为：

1. 默认抓当前屏幕可见文本
2. `--history` 允许向上抓取更多历史行
3. 调用后立即返回当前屏幕内容，不承担等待职责

说明：

1. `capture` 的职责是读取终端输出文本
2. 它不是等待接口，也不是专门的状态查询接口
3. 若需要等待 agent 完成工作，应使用 `wait`

默认值建议：

1. `history=0`

JSON 示例：

```json
{
  "ok": true,
  "command": "capture",
  "instance": "编码助手-A",
  "status": "busy",
  "data": {
    "cursor_x": 0,
    "cursor_y": 23,
    "width": 120,
    "height": 24,
    "history_lines": 120,
    "stable_for_ms": 1750,
    "content": "...\n"
  }
}
```

---

## 4.7 `wait`

用途：

等待 agent 看起来完成了当前工作，但不返回屏幕内容。

语法：

```bash
agentmux wait <instance-name> [--stable <duration-or-ms>] [--timeout <duration-or-ms>] [--json]
```

参数：

1. `--stable <duration-or-ms>`
2. `--timeout <duration-or-ms>`
3. `--json`

行为：

1. `wait` 的语义是等待 agent 看起来完成当前工作
2. `claude-code` 这类 harness 优先使用 `pane_title` 等直接状态信号判断是否完成
3. 其他 harness 回退到基于屏幕静止的通用启发式
4. `wait` 不返回屏幕文本

说明：

1. `wait` 是阻塞命令，不是状态查询命令
2. 若调用方只想看当前状态而不是等待，应使用 `inspect` 或 `list`

JSON 示例：

```json
{
  "ok": true,
  "command": "wait",
  "instance": "编码助手-A",
  "status": "idle",
  "data": {
    "cursor_x": 0,
    "cursor_y": 23,
    "width": 120,
    "height": 24,
    "history_lines": 120,
    "stable_for_ms": 0,
    "pane_title": "✳ Task complete"
  }
}
```

---

## 4.8 `attach`

用途：

让人类 attach 到实例对应的 tmux session。

语法：

```bash
agentmux attach [<instance-name>]
```

行为：

1. 传实例名时直接 attach
2. 未传实例名且当前在 TTY 中，则展示列表让用户选择
3. 非 TTY 环境且未传实例名，则报错

说明：

1. `attach` 主要服务人类调试
2. Agent 编排流程不应依赖它

---

## 4.9 `halt`

用途：

终止实例。

语法：

```bash
agentmux halt <instance-name> [--timeout <duration-or-ms>] [--immediately] [--json]
```

行为：

1. 默认先发送一次 `C-c`
2. 若实例仍在运行，则短暂等待后再发送第二次 `C-c`
3. 若到 `--timeout` 仍未退出，则回退为强制结束 tmux session
4. `--immediately` 跳过优雅停止，直接强制结束 tmux session
5. 结束后从 registry 中删除实例记录

参数：

1. `--timeout <duration-or-ms>`
2. `--immediately`

规则：

1. `--timeout` 默认值为 `5s`
2. `--timeout` 支持整数毫秒或 Go duration
3. `--immediately` 与 `--timeout` 不应同时使用

JSON 示例：

```json
{
  "ok": true,
  "command": "halt",
  "instance": "编码助手-A",
  "status": "exited",
  "data": {}
}
```

---

## 5. 错误码

第一版标准错误码：

1. `config_invalid`
2. `template_not_found`
3. `instance_not_found`
4. `tmux_unavailable`
5. `session_not_found`
6. `process_not_running`
7. `capture_timeout`
8. `invalid_key`
9. `invalid_arguments`

---

## 6. 典型调用序列

### 6.1 创建新实例并启动首次任务

```bash
agentmux summon --template 深度编码专家 --name 编码助手-A --cwd ~/work/project --prompt "先阅读项目并总结结构" --json
agentmux capture 编码助手-A --json
```

### 6.2 复用实例继续工作

```bash
agentmux summon --template 深度编码专家 --name 编码助手-A --json
agentmux capture 编码助手-A --history 120 --json
agentmux prompt 编码助手-A --text "继续修复剩余测试" --enter --json
```

### 6.3 中断当前任务

```bash
agentmux prompt 编码助手-A --key C-c --json
```
