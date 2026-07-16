---
name: agentmux
description: Manage isolated AI agent instances via `agentmux` CLI: summon, prompt, wait, capture, inspect, and halt. Covers tmux-backed TUI harnesses and structured harnesses (claude-code-ndjson, codex-cli-execjson, pi-rpc). Trigger when user mentions `agentmux` or needs a reusable external coding agent.
---

# Agentmux

Use `agentmux` as the only control surface for external agent instances. Do not call `tmux` directly and do not read raw harness logs unless the user explicitly asks for debugging internals.

## Harness Model

Always read `harness_type` from `template list --json`, `list --json`, or `inspect --json` before assuming how an instance behaves.

TUI harnesses (`claude-code`, `codex-cli`, `gemini-cli`) run an interactive terminal UI inside tmux. They may need startup time, may show upgrade or confirmation prompts, and may need an explicit `Enter` if text is pasted but not submitted.

Structured harnesses have no terminal screen and never need `Enter`:

1. `claude-code-ndjson`: one long-lived Claude Code process handles multiple turns. Prompting while busy is allowed; messages queue.
2. `codex-cli-execjson`: each prompt launches one `codex exec --json` turn process. Multi-turn continuity uses `resume <thread_id>`.
3. `pi-rpc`: one long-lived `pi --mode rpc` process handles multiple turns over an in-band JSONL command/event protocol. Prompting while busy is allowed; messages queue as follow-ups delivered after the current run. Completion is the `agent_settled` event.

For `codex-cli-execjson`, an `idle` instance with `process_id: 0` is healthy between turns. Prompting while a turn is running fails with `execjson_instance_busy`; wait before sending the next prompt.

## Default Loop

Use JSON mode by default.

```bash
agentmux template list --json
agentmux list --json
agentmux summon --template <template> --name <name> --json
agentmux inspect <name> --json
agentmux prompt <name> --text "..." --json
agentmux wait <name> --timeout 180s --json
agentmux capture <name> --json
```

Choose commands by intent:

1. `template list`: discover available roles.
2. `list`: find or scan existing instances.
3. `summon`: create or reuse an instance.
4. `inspect`: cheap status and metadata for one instance.
5. `prompt`: send text, stdin, or a supported key.
6. `wait`: block until the current work appears done; returns no content.
7. `capture`: read current observable output immediately; does not wait.
8. `halt`: stop an instance.
9. `attach`: human interactive debugging only.
10. `version`: confirm the installed command surface.

Verify deliverables directly after the agent reports completion. `idle`, `wait` success, or a confident message in `capture` does not prove files or artifacts are correct.

## Starting Work

Prefer `summon --prompt` when the harness is structured, already known-ready, or the first instruction is cheap to retry.

```bash
agentmux summon --template codex-cli-execjson --name 重构-A --prompt "阅读 src/ 并总结模块边界" --json
agentmux wait 重构-A --timeout 180s --json
agentmux capture 重构-A --json
```

Prefer separate `summon -> inspect/capture -> prompt` for fresh TUI harnesses, especially Claude Code, because startup screens and upgrade prompts can intercept input.

```bash
agentmux summon --template claude-code --name wiki审核-A --json
agentmux capture wiki审核-A --history 10 --json
agentmux prompt wiki审核-A --text "请阅读 /path/to/task.md 并按要求执行" --json
```

If a TUI capture shows an upgrade prompt such as `A new version is available ... [Y/n]`, answer it directly before sending the real task.

For long instructions to TUI harnesses, write the task to a file and prompt the agent to read it. `prompt --stdin` is best for short to medium multi-line text. Structured harnesses are more reliable for larger payloads, but very large tasks should still use file references.

## Command Reference

Create or reuse:

```bash
agentmux summon --template claude-code --name 编码助手-A --cwd /path --json
agentmux summon --template claude-code --name 编码助手-A --prompt "继续修复测试" --json
```

Inspect status:

```bash
agentmux inspect 编码助手-A --json
agentmux list --json
```

Read output:

```bash
agentmux capture 编码助手-A --history 120 --json
agentmux capture 重构-A --scope session --history 40 --json
```

`capture` defaults to `--scope current`. For TUI harnesses, current means current screen plus optional history lines. For structured harnesses, current means the active or most recent turn; `--scope session` intentionally reads the recorded conversation. On TUI harnesses `--history` counts screen lines. On structured harnesses it limits normalized messages.

Structured `capture --json` adds protocol fields. `claude-code-ndjson` returns `messages`, `usage`, `claude_session_id`, and `turns`. `codex-cli-execjson` returns `messages`, `usage`, `thread_id`, `turns`, `turn_state`, and `last_error`. `pi-rpc` returns `messages`, `usage`, `pi_session_id`, `turns`, and `last_error`.

Wait without content:

```bash
agentmux wait 编码助手-A --stable 1500 --timeout 30s --json
agentmux wait 重构-A --timeout 180s --json
```

`--stable` matters only for generic/TUI stability detection. Structured harnesses detect completion from protocol events or turn process exit.

Send input:

```bash
agentmux prompt 编码助手-A --text "继续" --json
printf '%s\n' "补充说明第一行" "补充说明第二行" | agentmux prompt 编码助手-A --stdin --json
agentmux prompt 编码助手-A --key Enter --json
agentmux prompt 编码助手-A --key C-c --json
```

Supported keys are `Enter`, `C-c`, `Escape`, `Up`, `Down`, and `Tab`. On structured harnesses only `C-c` has an effect; other keys are accepted as no-ops. On `codex-cli-execjson`, `C-c` kills the running turn process, marks that turn cancelled, and leaves the instance usable. On `pi-rpc`, `C-c` sends an in-band `abort` that stops the running turn while keeping the long-lived process alive; the instance stays usable.

Stop:

```bash
agentmux halt 编码助手-A --json
agentmux halt 编码助手-A --timeout 8s --json
agentmux halt 编码助手-A --immediately --json
```

Use plain `halt` or `halt --timeout` for graceful stopping. Use `--immediately` only when the user wants a hard stop or graceful interruption is no longer useful.

## Output Handling

Read top-level JSON first:

1. `ok`
2. `command`
3. `instance`
4. `reused`
5. `status`
6. `error_code`
7. `error`

For `capture`, read `data.content` as the primary output. Check `data.scope` before assuming whether structured messages came from one turn or the full session. For `codex-cli-execjson`, read `data.turn_state` and `data.last_error`; a failed turn still satisfies `wait`, and the instance remains usable.

For status decisions, prefer `status` from `inspect --json`, `list --json`, or `wait`. Do not rely on `pane_title`; it is observational metadata and structured harnesses leave it empty.

Treat statuses as follows:

1. `idle`: ready for the next instruction. On `codex-cli-execjson`, `process_id: 0` while idle is normal.
2. `busy`: current or recent work is in progress. On TUI harnesses this may degrade after the configured TTL; on structured harnesses it reflects protocol/process state.
3. `exited`: deliberately stopped.
4. `lost`: runtime state is missing or broken; inspect/list before deciding whether to summon fresh.

## Waiting And Interruption

Use `wait` when the only goal is completion detection. Use `capture` when you need output details.

For long-running work, prefer a patient cycle such as:

```text
1m, 1m, 3m, 5m, then repeat
```

Avoid single waits longer than `5m`; repeat the cycle instead. Treat `capture_timeout` from `wait` as "still active when the timeout expired", not as a failure. Follow with `capture` to inspect progress.

Do not interrupt only because work is slow or still `busy`. Interrupt only when the user asks, there is a clear blocker, there is an obvious loop/crash, or the task constraints require redirecting.

If interruption is needed:

1. Send one `C-c`.
2. Wait `10-15s`.
3. Check `inspect --json` or `capture --json`.
4. Use `halt` only if the instance remains unresponsive or should be stopped.

If `capture` shows a direct blocker such as `Y/n`, a permission prompt, or pasted text waiting for submission, answer the blocker directly. Structured harnesses do not present terminal blockers; `codex-cli-execjson` permissions are decided by the template command, usually through `--sandbox`.

## Recovery

When `template_not_found` appears:

```bash
agentmux template list --json
```

When `instance_not_found` appears:

```bash
agentmux list --json
```

When `execjson_instance_busy` appears, nothing was sent. Wait, then resend the same prompt.

```bash
agentmux wait <instance-name> --timeout 180s --json
agentmux prompt <instance-name> --text "..." --json
```

Do not immediately retry and do not `halt` just to unblock it.

When `config_invalid` appears for a `codex-cli-execjson` template, the command must be a plain `codex exec` prefix with only supported parent-level flags such as `--sandbox`, `--cd`, `--add-dir`, `--color`, `--skip-git-repo-check`, or `--model`. Remove `--json`, `-o`, `resume`, `review`, `--ask-for-approval`, `--ephemeral`, positional prompts, pipes, and redirection; agentmux supplies turn-specific arguments.

When `summon --model` fails on `codex-cli-execjson`, the template command likely does not contain `$MODEL`. Either rely on Codex's configured default model or change the template command to include `--model $MODEL`.

When `invalid_key` appears, retry with one supported key: `Enter`, `C-c`, `Escape`, `Up`, `Down`, or `Tab`.

When a command seems missing:

```bash
agentmux version --json
agentmux help <command>
```

## Working Patterns

Use descriptive instance names, not generic names like `worker1`. For parallel work, use a shared prefix and a shard suffix, such as `wiki审核-Q1to5` and `wiki审核-Q6to10`.

Parallel independent work:

```bash
agentmux summon --template claude-code --name task-A --json
agentmux summon --template claude-code --name task-B --json
agentmux list --json
```

Long-running monitoring:

```bash
agentmux inspect wiki审核-A --json
agentmux capture wiki审核-A --history 20 --json
agentmux wait wiki审核-A --timeout 3m --json
```

Deliverable verification:

1. Use `wait` or `capture` to learn what the external agent thinks happened.
2. Read the expected files or artifacts directly.
3. Decide whether to accept, ask for fixes, or redirect the instance.
