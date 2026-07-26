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

1. `claude-code`
2. `codex-cli`
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
        "name": "claude-code",
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
        "template": "claude-code",
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
    "template": "claude-code",
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
    "template": "claude-code",
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
3. `--json`

约束：

1. `--text` 与 `--key` 至少提供一个
2. 第一版 `--key` 一次只接受一个键
3. `--text` 发送后自动提交

TUI harness 的启动确认：

1. 对有 `pane_title` 状态信号的 harness（`claude-code`、`codex-cli`、`gemini-cli`），发送文本后 `prompt` 会等待 `pane_title` 切换到 busy 标记，最多 `defaults.status.prompt_ack_ms`
2. 观察到切换即返回，正常只增加数百毫秒
3. 这一步是 `wait` 可靠的前提：在 harness 反应过来之前，`pane_title` 仍然描述上一轮，直接相信它会把未开始的工作判成已完成
4. 未观察到切换时不报错（消息已经送达），但本轮的 idle 信号会退回保守判定

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
agentmux prompt 编码助手-A --text "继续修复测试"
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
    "sent_key": ""
  }
}
```

---

## 4.6 `capture`

用途：

立即读取实例当前可观测输出。

语法：

```bash
agentmux capture <instance-name> [--scope current|session] [--history <limit>] [--json]
```

参数：

1. `--scope current|session`，默认 `current`
2. `--history <limit>`
3. `--raw`
4. `--json`

行为：

1. 默认 `--scope current`
2. TUI harness 下，`current` 表示当前屏幕，`--history` 表示向上抓取的历史行数
3. 结构化 harness 下，`current` 表示当前或最近 turn，`session` 表示整段已记录会话，`--history` 表示归一化消息数量限制
4. 调用后立即返回当前可解析内容，不承担等待职责
5. 结构化 harness 未指定 `--history` 时，默认只返回最近 `20` 条消息；`--history 0` 表示不限制
6. 默认不返回协议原始事件：`messages[].raw` 被移除，过长的 `text` 与 `input` 会被截断
7. `--raw` 恢复完整保真度，用于调试；完整事件流始终保存在实例的 `output.jsonl`
8. 结构化 harness 不返回 `cursor_x`、`cursor_y`、`width`、`height`、`pane_title` 这些屏幕字段，改为返回 `messages_limit`

体积约束：

结构化 harness 每个协议事件对应一条消息。不做上述限制时，一个进行中的 turn 可以产出上百 KB JSON，而真正有用的 `data.content` 往往只有几十字节。只读输出时优先使用不带 `--json` 的 `capture`（只输出 `content`）。

说明：

1. `capture` 的职责是读取输出
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
    "scope": "current",
    "harness_type": "claude-code",
    "history_lines": 120,
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
5. **超时不是错误**：到 `--timeout` 仍未完成时返回 `ok: true`、退出码 `0`、`status: busy`、`data.timed_out: true`
6. 只有实例损坏、丢失或进程异常才返回 `ok: false`

完成判定的可信度：

1. 若 `prompt` 已确认 harness 开始工作（见 4.5），idle 信号立即可信
2. 否则在 `--stable` 指定的静默窗口内不接受 idle 信号，避免把上一轮遗留的 idle 标题当作本轮完成
3. `data.saw_busy` 表示本次等待期间确实观察到 harness 在工作
4. `--stable 0` 关闭该保护，只在明确不需要时使用

返回字段：

1. `timed_out`：是否因超时返回
2. `saw_busy`：等待期间是否观察到 busy
3. `elapsed_ms`：本次等待实际耗时
4. `stable_for_ms`：通用启发式下的屏幕静止时长
5. TUI harness 额外返回 `cursor_x`、`cursor_y`、`width`、`height`、`history_lines`、`pane_title`

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
    "timed_out": false,
    "saw_busy": true,
    "elapsed_ms": 8421,
    "stable_for_ms": 0,
    "cursor_x": 0,
    "cursor_y": 23,
    "width": 120,
    "height": 24,
    "history_lines": 120,
    "pane_title": "✳ Task complete"
  }
}
```

超时（仍在工作）示例：

```json
{
  "ok": true,
  "command": "wait",
  "instance": "编码助手-A",
  "status": "busy",
  "data": {
    "timed_out": true,
    "saw_busy": true,
    "elapsed_ms": 180003,
    "stable_for_ms": 0,
    "pane_title": "⠋ Working"
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

错误码稳定，调用方可以按码分支。

### 5.1 通用

| 错误码 | 含义 | 建议动作 |
| --- | --- | --- |
| `invalid_arguments` | 参数缺失、冲突或位置错误 | 按提示修正；实例名必须写在 flag 之前 |
| `invalid_key` | `--key` 不在白名单内 | 改用 `Enter`、`C-c`、`Escape`、`Up`、`Down`、`Tab` |
| `template_not_found` | 模板不存在 | `template list --json` |
| `instance_not_found` | 实例不存在或已被清理 | `list --json`，必要时 `summon` |
| `instance_template_mismatch` | 同名实例属于其他模板 | 换一个实例名 |
| `instance_changed` | 发送期间实例被其他进程替换 | 重新 `inspect` 后再决定 |
| `process_not_running` | 实例进程已不在运行 | `inspect --json`，必要时重新 `summon` |
| `session_not_found` | 运行时会话缺失 | 同上 |
| `tmux_unavailable` | tmux 不可用或命令失败 | 检查 tmux 安装、socket 路径与权限 |
| `capture_timeout` | 内部超时信号 | `wait` 不再向调用方返回该码，超时表现为 `ok: true` + `timed_out: true` |
| `input_too_large` | `--stdin` 输入超过上限 | 改为写文件并让外部 Agent 读取 |
| `input_read_error` | 读取标准输入失败 | 检查管道来源 |
| `internal_error` | 未归类错误 | 带 `AGENTMUX_LOG_LEVEL=debug` 重跑并上报 |

### 5.2 配置与注册表

| 错误码 | 含义 |
| --- | --- |
| `config_invalid` | 配置或模板命令不合法 |
| `config_parse_error` | 配置文件 YAML 解析失败 |
| `config_io_error` | 配置目录或文件读写失败 |
| `registry_io_error` | `instances.json` 读写失败 |
| `registry_parse_error` | `instances.json` 内容损坏 |
| `registry_lock_error` | 注册表文件锁获取失败 |

### 5.3 `claude-code-ndjson`

1. `ndjson_fifo_broken`
2. `ndjson_parse_error`
3. `ndjson_process_error`
4. `ndjson_state_error`

### 5.4 `codex-cli-execjson`

1. `execjson_parse_error`
2. `execjson_process_error`
3. `execjson_state_error`
4. `execjson_instance_busy` — 实例正在执行一个 turn 时再次 `prompt`。codex 无法向执行中的 turn 追加输入，调用方应先 `wait` 再原样重发。

### 5.5 `pi-rpc`

1. `rpc_fifo_broken`
2. `rpc_parse_error`
3. `rpc_process_error`
4. `rpc_state_error`

`pi-rpc` 在运行中再次 `prompt` 不会报错：消息经 `streamingBehavior:"followUp"` 排入 pi 的原生队列，在当前 run 结束后交付。

---

## 6. 典型调用序列

### 6.1 创建新实例并启动首次任务

```bash
agentmux summon --template claude-code --name 编码助手-A --cwd ~/work/project --prompt "先阅读项目并总结结构" --json
agentmux capture 编码助手-A --json
```

### 6.2 复用实例继续工作

```bash
agentmux summon --template claude-code --name 编码助手-A --json
agentmux capture 编码助手-A --history 120
agentmux prompt 编码助手-A --text "继续修复剩余测试" --json
```

### 6.3 等待长任务

```bash
agentmux wait 编码助手-A --timeout 3m --json    # timed_out=true 表示仍在工作，继续等
agentmux wait 编码助手-A --timeout 5m --json
agentmux capture 编码助手-A                      # 读结果用纯文本，避免协议噪音
```

### 6.4 中断当前任务

```bash
agentmux prompt 编码助手-A --key C-c --json
```
