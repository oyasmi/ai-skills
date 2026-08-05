# 错误码参考

只在 SKILL.md 速查表之外撞到陌生错误码时查这里。先运行 `agentmux doctor --json`：多数「文档说的行为和实际不一致」最终会定位到 PATH 上的旧二进制或缺失的外部 CLI，`doctor` 直接指出来，比对着错误文本猜快得多。

| `error_code` | 含义 | 下一步 |
| --- | --- | --- |
| `template_not_found` | 模板名不存在 | `agentmux template list --json` |
| `instance_not_found` | 实例不存在或已被清理 | `agentmux list --json`，再决定是否 `summon` |
| `instance_template_mismatch` | 同名实例属于其他模板 | 换一个描述性实例名 |
| `process_not_running` | 实例已停止 | 错误信息里带 `end_reason`；用 `inspect --json` 读墓碑的 `end_reason`、`ended_at`、`last_error`，判断是被 halt 还是崩溃，再决定是否用同名 `summon` 重建 |
| `session_not_found` | 运行时会话缺失 | 同上 |
| `instance_busy` | `run` 或 `prompt --wait-if-busy` 在给定预算内等到实例仍然忙 | 加大 `--timeout`/`--wait-if-busy`，或换一个实例名；不要立即重试 |
| `execjson_instance_busy` | 直接用 `prompt`（不带 `--wait-if-busy`）撞上正在跑的 turn，任务指令没有发出 | 用 `prompt --wait-if-busy <duration>` 重发，或先 `wait` 再原样重发；不要用 `halt` 解锁 |
| `invalid_key` | 按键不在白名单 | 改用 `Enter`、`C-c`、`Escape`、`Up`、`Down`、`Tab` |
| `invalid_arguments` | 参数错误 | 按提示修正；实例名和 flag 可以混排；`attach` 不支持 `--json`；`--new` 与 `--since` 互斥；`--detach` 不能与 `--history`/`--trace`/`--raw` 同时使用 |
| `tmux_unavailable` | tmux 不可用或命令失败 | 报告环境问题；先跑 `agentmux doctor --json` 确认 |
| `config_invalid` / `config_parse_error` / `config_io_error` | 配置问题 | 见下面的模板命令修复说明；必要时请用户检查配置 |
| `registry_io_error` / `registry_parse_error` / `registry_lock_error` | agentmux 自身状态文件异常 | 重试一次；仍失败则报告给用户，不要反复重试 |
| `ndjson_*` / `execjson_*` / `rpc_*` | 对应结构化 harness 的传输或状态错误 | `inspect --json` 确认状态，必要时新建实例继续 |
| `instance_changed` | 发送期间实例被其他进程替换 | 重新 `inspect --json` 后再决定 |
| `internal_error` | 未归类错误 | 报告给用户，不要静默重试 |

`wait` 真正到达 deadline 时不表现为错误：它返回 `ok: true` 和 `data.timed_out: true`，按"仍在工作"处理。多实例 `wait` 中，失败项仍会和其他实例结果一起返回，但顶层 `ok` 为 `false`、退出码非零；只有真正到达 deadline 才设置 `data.timed_out: true`。

实例停止后不会从记录中消失，而是保留为墓碑：`instance_not_found` 表示这个名字从来不存在（多半是名字写错），`process_not_running` 表示它确实存在过但已经停止，`inspect` 仍能读到停止原因。排查外部 Agent "消失"时先 `inspect --json`，再看 `list --all --json`。

命令疑似不存在时：运行 `agentmux version --json` 和 `agentmux help <command>`；`version --json` 的 `features` 列出这个二进制实际支持什么，不要靠试命令看报错来猜。

`codex-cli-execjson` 出现 `config_invalid` 时，把模板命令改成只带受支持父级 flag 的普通 `codex exec` 前缀，例如 `--sandbox`、`--cd`、`--add-dir`、`--color`、`--skip-git-repo-check`、`--model` 或 `-c key=value`。移除 `--json`、`-o`、`resume`、`review`、`--ask-for-approval`、`--ephemeral`、位置参数、管道和重定向；turn 参数由 agentmux 注入。

`config_invalid` 提到 effort 时，是模板里的 `effort` 不在 `off`/`minimal`/`low`/`medium`/`high`/`xhigh`/`max` 之内——这在配置加载阶段就会失败，所有命令都会受影响，必须先改配置。

`invalid_arguments` 提到 model 或 effort 时，说明这个角色的强度设置落不到它的 harness 上：

- `harness "gemini-cli" cannot be told how hard to think`：`gemini-cli` 没有 thinking 开关，去掉这个模板的 `effort`。
- `harness "<x>" has no known model flag`（或 effort 同类信息）：这个 `harness_type` 不在 agentmux 的 flag 映射表里。要么改成已支持的 harness，要么在模板命令里自己写 `$MODEL`/`$EFFORT` 占位符指定位置。
- `invalid value for --effort`：取值拼错了，按提示里列出的档位改。

档位在目标 CLI 词表更窄时会被向下夹取（例如 claude 的 `off`/`minimal` 都变成 `low`），这不是错误：`agentmux doctor` 会标出发生夹取的模板，`agentmux inspect <名称>` 的 `command` 显示实际启动的命令。
