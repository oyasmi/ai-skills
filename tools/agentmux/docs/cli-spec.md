# agentmux CLI 规格

## 1. 目标

本规格定义 `agentmux` 的命令行接口，目标是：

1. 子命令少
2. 参数少
3. 语义稳定
4. 输出适合 AI Agent 消费

命令集：

1. `template list`
2. `list`
3. `summon`
4. `run`
5. `inspect`
6. `prompt`
7. `capture`
8. `wait`
9. `attach`
10. `halt`
11. `version`

`run` 是编排的默认入口：一次调用完成 summon、prompt、wait 和 capture。其余命令用于需要分步控制的场景。

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
agentmux list [--all] [--json]
```

文本输出建议列：

1. `NAME`
2. `TEMPLATE`
3. `STATUS`
4. `MODEL`
5. `CWD`
6. `UPDATED`
7. `ENDED`

说明：

1. `list` 是多实例状态查询接口
2. 当调用方还不确定实例名，或想批量扫描状态时，优先使用 `list`
3. 默认只返回存活实例；`--all` 额外返回墓碑

### 4.2.1 墓碑

实例停止后不会从注册表消失，而是保留为墓碑，让调用方能区分「名字写错了」和「我的 worker 死了，原因是什么」。

墓碑字段：

1. `status`：`exited` 或 `lost`
2. `end_reason`：`halted`、`process_exited` 或 `session_lost`
3. `ended_at`：停止时间
4. `last_error`：harness 自己给出的解释，例如崩溃 turn 的 stderr 摘要

规则：

1. `list` 默认隐藏墓碑，`list --all` 显示
2. `inspect` 对墓碑正常返回，这正是排查时需要的
3. `prompt`、`capture`、`wait` 对墓碑返回 `process_not_running`，错误信息里带上 `end_reason` 和 `last_error`
4. `halt` 对已停止实例是幂等的，不报错
5. 墓碑不占用 `max_instances` 配额，同名 `summon` 可以直接回收该名字
6. 超过 `defaults.status.tombstone_ttl_ms`（默认 24 小时）后自动清除

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

若目标 `--cwd` 已有其他存活实例，返回的 `data.warnings` 会包含 `cwd_shared:<那个实例名>`。这不会阻止本次 `summon`：agentmux 隔离的是 Agent 进程，不是文件系统，两个写入型实例共享同一个工作目录会在 Git 状态和构建产物上互相竞争，值得提醒但不必强行禁止（例如只读审查者与写入者共享 `cwd` 就是合理用法）。

返回字段：

1. `instance`
2. `reused`
3. `status`
4. `data.template`
5. `data.model`
6. `data.cwd`
7. `data.warnings`（仅当存在时出现）

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

## 4.3.1 `run`

用途：

一次调用完成一次委派：创建或复用实例、发送任务指令、等待完成、读回结果。

语法：

```bash
agentmux run --template <template-name> (--prompt <text> | --prompt-file <path> | --stdin) [flags]
```

参数：

1. `--template <name>`（必填）
2. `--name <instance-name>`
3. `--cwd <path>`
4. `--model <provider/model>`
5. `--command <shell-command>`
6. `--system-prompt <text>`
7. `--prompt <text>` / `--prompt-file <path>` / `--stdin`（三选一，必填）
8. `--timeout <duration-or-ms>`，默认 `5m`
9. `--history <limit>`（结构化 harness 上隐含 `--trace`）
10. `--trace`
11. `--raw`（隐含 `--trace`）
12. `--detach`：发送 prompt 后立即返回，不等待也不读取输出；不能与 `--history`/`--trace`/`--raw` 同时使用
13. `--json`

行为：

1. 等价于「`summon`（不带 prompt）→ 若复用到忙实例则在 `--timeout` 预算内等待 → `prompt` → `wait` → `capture`」，但只有一次调用、一个退出码、一份载荷
2. 同名实例存在时复用，因此重复 `run` 会在同一会话里继续
3. 复用到的实例仍在忙时，不会立刻报错或把新任务和旧任务的输出混在一起：先在 `--timeout` 预算内等旧任务收尾，花掉的时间通过 `data.queued_ms` 报告；预算耗尽仍未空闲则返回 `instance_busy`
4. prompt 发送之后再超时不是失败：agent 继续工作，返回 `data.timed_out: true` 和当前已产出的内容，调用方可以再 `wait`
5. 结束后实例保留，便于检查和追加指令；需要停止时显式 `halt`
6. `--prompt-file` 适合较长的任务契约，避免超长命令行和 TUI 粘贴问题
7. `--detach` 仍会先按第 3 条等待忙实例，但发送 prompt 后立即返回，用于并行分片；配合 `wait --mode any --collect` 使用

返回字段：

1. `instance`、`reused`、`status`
2. `data.elapsed_ms`、`data.queued_ms`
3. `data.template`、`data.harness_type`、`data.cwd`
4. 非 `--detach`：`data.content`（agent 的产出）、`data.timed_out`；结构化 harness 默认只含状态字段（`usage`、`turn_state`、`last_error`、`next_cursor` 等），`messages` 需要 `--trace`/`--raw`/`--history`
5. `--detach`：`data.detached: true`，不产生 `data.content`/`data.timed_out`
6. 若目标 `cwd` 已有其他存活实例，`data.warnings` 含 `cwd_shared:<name>`

JSON 示例：

```json
{
  "ok": true,
  "command": "run",
  "instance": "登录修复-A",
  "reused": false,
  "status": "idle",
  "data": {
    "template": "codex-cli-execjson",
    "harness_type": "codex-cli-execjson",
    "cwd": "/Users/me/work/project",
    "content": "已修复重试逻辑，go test ./internal/auth/... 通过。",
    "timed_out": false,
    "elapsed_ms": 42137,
    "queued_ms": 0,
    "turn_state": "completed",
    "last_error": "",
    "next_cursor": "18422"
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
3. `--stdin`
4. `--wait-if-busy <duration-or-ms>`，默认不等待（保持发送前立即尝试的历史行为）
5. `--json`

约束：

1. `--text`、`--stdin`、`--key` 至少提供一个；`--text` 与 `--stdin` 互斥
2. 第一版 `--key` 一次只接受一个键
3. `--text`/`--stdin` 发送后自动提交

`--wait-if-busy`：

1. 若实例当前 `busy`，发送前先在给定预算内等待它把当前工作收尾，跨 `claude-code-ndjson`（原本会排队）、`codex-cli-execjson`（原本会报 `execjson_instance_busy`）、`pi-rpc`（原本会排队）三种结构化 harness 统一为同一种行为
2. `data.queued_ms` 报告这次等待实际花费的时间
3. 预算耗尽仍未空闲则返回 `instance_busy`，不发送
4. 不传或传 `0` 时完全跳过这一步，等价于历史行为（立即尝试发送，busy 时的结果由各 harness 自己的语义决定）

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
2. `--history <limit>`（结构化 harness 上隐含 `--trace`）
3. `--since <cursor>`（结构化 harness上隐含 `--trace`，与 `--new` 互斥）
4. `--new`（效果同 `--since`，但游标由 agentmux 记在实例上并自动前移；与 `--since` 互斥）
5. `--trace`（结构化 harness 上请求 `messages` 逐条消息，默认不带）
6. `--raw`（隐含 `--trace`）
7. `--json`

行为：

1. 默认 `--scope current`
2. TUI harness 下，`current` 表示当前屏幕，`--history` 表示向上抓取的历史行数
3. 结构化 harness 下，`current` 表示当前或最近 turn，`session` 表示整段已记录会话，`--history` 表示归一化消息数量限制
4. 调用后立即返回当前可解析内容，不承担等待职责
5. 结构化 harness 默认**不**返回 `messages`：`data.content` 已经是答案，逐事件轨迹只在明确请求时才构建。`--trace`、`--raw`、显式 `--history`、`--since`、`--new` 中任意一个都会带回 `messages`，未指定条数时限 `20` 条，`--history 0` 表示不限制
6. `messages` 默认不返回协议原始事件：`messages[].raw` 被移除，过长的 `text` 与 `input` 会被截断
7. `--raw` 恢复完整保真度，用于调试；完整事件流始终保存在实例的 `output.jsonl`
8. 结构化 harness 不返回 `cursor_x`、`cursor_y`、`width`、`height`、`pane_title` 这些屏幕字段；只有在 `messages` 被返回时才附带 `messages_limit`

增量读取：

1. 每次结构化 `capture` 都返回 `data.next_cursor`
2. 把它作为下次的 `--since`，只会返回此后新产生的内容，用于低成本地观察长任务；或者直接用 `--new`，让 agentmux 自己记账，不用来回传递游标
3. `--since`/`--new` 覆盖 `--scope`，且不受默认消息条数上限约束，因为增量本身就是有界的
4. 没有新内容时返回空 `messages` 且 `next_cursor` 不变
5. `--since` 的 cursor 对调用方不透明，只能原样回传工具给出的值；`--new` 完全不需要调用方持有 cursor
6. TUI harness 没有可回放的事件流，使用 `--since`/`--new` 会返回 `invalid_arguments`

体积约束：

结构化 harness 每个协议事件对应一条消息。默认不构建 `messages` 正是为了避免这一点：一个进行中的 turn 可以产出上百 KB JSON，而真正有用的 `data.content` 往往只有几十字节。只读输出时优先使用不带 `--json` 的 `capture`（只输出 `content`）；需要 `--json` 时默认响应也已经是精简过的。

说明：

1. `capture` 的职责是读取输出
2. 它不是等待接口，也不是专门的状态查询接口
3. 若需要等待 agent 完成工作，应使用 `wait`

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

等待 agent 看起来完成了当前工作；加 `--collect` 时顺带读回已完成实例产出的内容。

语法：

```bash
agentmux wait <instance-name>... [--mode all|any] [--stable <duration-or-ms>] [--timeout <duration-or-ms>] [--collect] [--json]
```

参数：

1. 一个或多个实例名，必须写在所有 flag 之前
2. `--mode all|any`，默认 `all`
3. `--stable <duration-or-ms>`
4. `--timeout <duration-or-ms>`
5. `--collect`：对每个已完成（`done`）的实例，额外做一次精简 `capture` 并把 `content` 等字段附带在结果里；仍 pending 的实例不读取
6. `--json`

多实例：

1. `--mode all`：全部完成或超时才返回
2. `--mode any`：任意一个完成即返回，其余等待被取消并报告为 pending。并行分片靠它才可用：先处理最先完成的分片，而不是被最慢的挡住
3. 单个实例名时返回原有单实例结构；多个实例名时返回 `data.instances` 数组
4. 多实例下某个实例失败只记在该实例上（`error_code`），不影响其他实例的结果；`--collect` 对某个已完成实例读取失败时，同样只记在该实例上，不影响其他实例
5. `data.satisfied` 表示是否满足了 `--mode`；`data.done`、`data.pending`、`data.failed` 给出分组
6. 并行分片的推荐形态：`run --detach` × N（各自独立 `--cwd`），再一次 `wait --mode any --collect`，不必对每个分片单独 `capture`

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
6. `--collect` 且已完成时，额外返回 `content` 及该 harness 的精简 `capture` 状态字段（结构化 harness 默认不含 `messages`，规则同 `capture`）

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

多实例示例：

```json
{
  "ok": true,
  "command": "wait",
  "data": {
    "mode": "any",
    "satisfied": true,
    "timed_out": false,
    "done": ["分片-B"],
    "pending": ["分片-A"],
    "failed": [],
    "instances": [
      {"instance": "分片-A", "done": false, "status": "busy", "elapsed_ms": 0, "saw_busy": false, "pane_title": "⠋ Working"},
      {"instance": "分片-B", "done": true, "status": "idle", "elapsed_ms": 1054, "saw_busy": true, "pane_title": "✳ Ready"}
    ]
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
5. 结束后实例记录保留为墓碑（`end_reason: halted`），见 4.2.1
6. 对已停止的实例是幂等的，不报错

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

## 4.10 `version`

用途：

返回版本，以及这个二进制支持什么。

语法：

```bash
agentmux version [--json]
```

行为：

1. 文本模式只输出版本号
2. `--json` 额外返回 `build_time`、`binary_path` 和能力清单，调用方据此判断特性是否可用、以及这次调用实际跑的是哪个二进制，而不是靠试命令看报错
3. 该命令不加载配置，配置损坏时仍可用
4. `version` 是 `"dev"` 通常意味着这个二进制不是通过 `scripts/install.sh`/`scripts/release.sh` 打的版本戳；配合 `doctor` 检查 PATH 上是否有另一个副本在遮蔽它

JSON 示例：

```json
{
  "ok": true,
  "command": "version",
  "data": {
    "version": "v0.4.0",
    "build_time": "2026-04-01T08:00:00Z",
    "binary_path": "/home/me/.local/bin/agentmux",
    "commands": ["template list", "list", "summon", "run", "inspect", "prompt", "capture", "wait", "attach", "halt", "doctor", "version"],
    "harness_types": ["claude-code", "codex-cli", "gemini-cli", "claude-code-ndjson", "codex-cli-execjson", "pi-rpc"],
    "features": ["run", "doctor", "version-provenance", "run-wait-if-busy", "wait-multi", "wait-timeout-ok", "wait-observability", "prompt-ack", "capture-since", "capture-raw", "lean-capture", "capture-new-cursor", "run-detach", "wait-collect", "cwd-shared-warning", "tombstones"]
  }
}
```

---

## 4.11 `doctor`

用途：

一次性检查 agentmux 依赖的整个环境，而不是让调用方逐条试探命令、再从报错反推问题。

语法：

```bash
agentmux doctor [--json]
```

行为：

1. 依次检查：`binary`（这次调用实际运行的版本/构建时间/路径）、`path`（PATH 上是否有另一份 `agentmux` 在遮蔽当前运行的这个）、`paths`（解析出的配置和状态目录）、`state_dir`（可写性）、`registry`（注册表锁可获取）、`config`（配置文件可解析且合法）、每个模板的 `template:<name>`（解析后的命令首个词是否在 PATH 上）、`tmux`（仅当存在需要它的模板时才检查）
2. 每项结果是 `ok`、`warn`、`fail` 或 `skip`；只有 `fail` 影响整体结果和退出码
3. 遇到会阻断后续检查的失败（例如配置解析失败）时提前结束，已收集的结果仍会全部返回
4. 环境异常导致的疑难问题应先跑 `doctor` 而不是先怀疑是 agentmux 的 bug——多数"文档说的行为和实际不一致"最终会定位到 PATH 上的旧二进制或缺失的外部 CLI，这两者 `doctor` 都能直接指出来

JSON 示例：

```json
{
  "ok": true,
  "command": "doctor",
  "data": {
    "checks": [
      {"name": "binary", "status": "ok", "detail": "version=v0.4.2 build_time=2026-04-01T08:00:00Z path=/home/me/.local/bin/agentmux"},
      {"name": "path", "status": "ok", "detail": "PATH resolves to the running binary: /home/me/.local/bin/agentmux"},
      {"name": "paths", "status": "ok", "detail": "config=/home/me/.config/agentmux/config.yaml state=/home/me/.local/state/agentmux"},
      {"name": "state_dir", "status": "ok", "detail": "/home/me/.local/state/agentmux"},
      {"name": "registry", "status": "ok", "detail": "/home/me/.local/state/agentmux/instances.json"},
      {"name": "config", "status": "ok", "detail": "4 templates from /home/me/.config/agentmux/config.yaml"},
      {"name": "template:codex-cli-execjson", "status": "ok", "detail": "codex (harness=codex-cli-execjson)"},
      {"name": "tmux", "status": "skip", "detail": "no TUI templates configured"}
    ]
  }
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
| `process_not_running` | 实例已停止（错误信息带 `end_reason` 和 `last_error`） | `inspect --json` 读墓碑详情，必要时用同名 `summon` 重建 |
| `session_not_found` | 运行时会话缺失 | 同上 |
| `instance_busy` | `run` 或 `prompt --wait-if-busy` 在预算内等到实例仍然忙 | 加大超时预算，或换一个实例名 |
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

### 6.0 一次性委派（默认入口）

```bash
agentmux run --template codex-cli-execjson --cwd /path/to/repo --prompt-file ./task.md --timeout 10m --json
```

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

### 6.4 并行分片

单路阻塞式的 `run` 不适合并行：它会阻塞到自己那一路结束。用 `run --detach` 把每个分片的任务发出去（各自独立 `--cwd`），再统一 `wait --mode any --collect`，不用再对每个分片单独 `capture`。

```bash
agentmux run --template codex-cli-execjson --name 分片-A --cwd /wt/a --prompt "..." --detach --json
agentmux run --template codex-cli-execjson --name 分片-B --cwd /wt/b --prompt "..." --detach --json
agentmux wait 分片-A 分片-B --mode any --timeout 5m --collect --json
```

### 6.5 观察长任务的增量输出

```bash
agentmux wait 编码助手-A --timeout 3m --json
agentmux capture 编码助手-A --new --json   # 游标由 agentmux 自己记账并前移，不用手动传递
agentmux wait 编码助手-A --timeout 3m --json
agentmux capture 编码助手-A --new --json   # 只返回上一次 --new 之后新产生的内容
```

### 6.6 中断当前任务

```bash
agentmux prompt 编码助手-A --key C-c --json
```
