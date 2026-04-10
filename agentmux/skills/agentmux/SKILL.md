---
name: agentmux
description: Control, reuse, and inspect external terminal AI agent instances through the `agentmux` CLI. Use when you need to start or continue work inside an isolated tmux-backed instance, manage multiple coding agents or TUI agents, capture current screen text, send the next prompt or key, wait for work completion, or attach for human debugging. Trigger this skill when the user explicitly mentions `agentmux`, asks to orchestrate another terminal agent, or needs a reusable external coding/documentation/workflow assistant running in its own terminal session.
---

# Agentmux

Use `agentmux` instead of calling `tmux` directly. Treat `agentmux` as the only control surface for external terminal agent instances.

## Core Rules

1. Prefer `agentmux ... --json` so you can consume stable machine-readable fields.
2. Run `agentmux list --json` before assuming an instance does or does not exist.
3. Run `agentmux template list --json` before choosing a template if the available templates are not already known.
4. Use `summon` to create or reuse an instance. Do not call `tmux new-session` yourself.
5. Use `inspect --json` when you need the current status for one instance.
6. Use `capture` when you need current screen text. Do not read raw tmux output or terminal stdout directly.
7. Use `wait` when you only need to block until the agent appears done and do not need returned content.
8. Use `prompt` to send the next text or special key after you inspect current state.
9. Use `attach` only when a human explicitly asks to watch or debug interactively.
10. Use `version --json` when you need to confirm the installed CLI version or check whether a newer command should exist.
11. Prefer short, task-specific prompts. Do not resend large repeated context if the instance already has it.
12. Do not rely on long `prompt --stdin` payloads for Claude Code. For long instructions, write them to a file first and then send a short `--text` message telling the agent to read that file.
13. Verify deliverables, not just status. `idle` or `wait` success does not prove the target file or artifact is correct.

## Workflow

1. List templates if the correct role template is unclear.
2. List instances if the correct instance is unclear.
3. Summon the target instance by template and optional name.
4. If this is a fresh TUI-style harness launch, confirm it is ready before sending an important first prompt.
5. Send the next instruction or key.
6. If the prompt text appears to be buffered on screen but the agent does not start working, send one explicit `Enter` before assuming it is stuck.
7. Repeat `capture|wait -> decide -> prompt` until the task reaches a stopping point.
8. After the agent reports completion, read the expected output files and verify the deliverable.

Typical loop:

```bash
agentmux list --json
agentmux summon --template 深度编码专家 --name 编码助手-A --json
agentmux capture 编码助手-A --history 120 --json
agentmux prompt 编码助手-A --text "继续修复剩余失败测试" --json
```

First-prompt decision:

1. Use `summon --prompt` when you want one call to create or reuse the instance and the first instruction is safe to send immediately.
2. Use separate `summon -> capture -> prompt` when the harness may still be starting, may show an upgrade prompt, or you need to inspect screen state before sending the real task.

For Claude Code cold starts, check whether the input prompt is actually ready before sending the first important task:

```bash
agentmux summon --template 工作项管理助手 --name wiki审核-A --json
agentmux capture wiki审核-A --history 10 --json
```

If capture shows an upgrade prompt such as `A new version is available ... [Y/n]`, dismiss it first:

```bash
agentmux prompt wiki审核-A --text "n" --json
```

If Claude Code is not yet ready to execute a long first task, prefer a follow-up prompt after readiness is visible in the captured content.

For long instructions to Claude Code, prefer a file-based handoff:

```bash
agentmux prompt wiki审核-A --text "请阅读 /path/to/task.md 并按要求执行" --json
```

Avoid treating long `--stdin` payloads as reliable for Claude Code. They may appear in the input buffer without actually starting execution.

## Command Quick Reference

Use `summon` when you need to create or reuse an instance.

```bash
agentmux summon --template 深度编码专家 --name 编码助手-A --cwd /path --json
```

Use `summon --prompt` when the same call should also send a message.

```bash
agentmux summon --template 深度编码专家 --name 编码助手-A --prompt "先阅读项目并总结结构" --json
```

Use summon overrides when the existing template is close but not exact.

```bash
agentmux summon --template 深度编码专家 --name 编码助手-A --model openai/gpt-5.4 --command 'codex --model $MODEL' --system-prompt "先建上下文，再直接修改" --json
```

Use `inspect` when you need metadata such as template, cwd, model, status, or session identity.

```bash
agentmux inspect 编码助手-A --json
```

Use `list` for a multi-instance status overview, and use `inspect --json` for one instance's current status.

Use `capture` when you need the visible screen text and recent history.

```bash
agentmux capture 编码助手-A --history 120 --json
```

Use `wait` when you only need the agent to appear done and want to avoid returning large text.

```bash
agentmux wait 编码助手-A --stable 1500 --timeout 30s --json
```

Use `prompt` when the instance already exists and you want to separate control from creation.

```bash
agentmux prompt 编码助手-A --text "继续" --json
echo "短的多行补充说明" | agentmux prompt 编码助手-A --stdin --json
agentmux prompt 编码助手-A --key Enter --json
agentmux prompt 编码助手-A --key C-c --json
```

If pasted text is visible in the harness input area but execution does not begin, send one explicit `Enter` with `agentmux prompt <name> --key Enter --json`.

Use `halt` when the instance should stop. By default it attempts graceful interruption first.

```bash
agentmux halt 编码助手-A --json
agentmux halt 编码助手-A --timeout 8s --json
agentmux halt 编码助手-A --immediately --json
```

Use `version` when you need to confirm which CLI surface is available.

```bash
agentmux version --json
```

## Decision Rules

Prefer `inspect --json` before sending another message. Use `capture` only when you also need screen text. Avoid blind prompting.

Prefer `inspect --json` when the question is "what is the current status of this one instance?".

Prefer `list --json` when the question is "what is the status across multiple instances?".

Prefer `wait` only when the question is "block until this work seems finished".

Prefer reusing a named instance when the user is clearly continuing previous work in the same external agent.

Prefer `summon --prompt` only when the harness is already known-ready or the first instruction is cheap to retry.

Prefer separate `summon -> capture -> prompt` for fresh Claude Code sessions and other TUI harnesses that may need startup time or may ask an initial question before accepting the real task.

Prefer `prompt --stdin` for short to medium multi-line text only. For long task descriptions, especially with Claude Code, prefer the file-based pattern instead of pushing the whole payload through stdin.

If the text prompt seems to have landed in the input box but no work starts, prefer sending a single `Enter` before retrying or interrupting the instance.

Prefer `wait` over `capture` when the only goal is to block until the instance appears done.

Treat `capture_timeout` from `wait` as "still active when the timeout expired", not automatically as a failure. Follow with `capture` to inspect actual progress before interrupting work.

Send `C-c` before anything else when the instance is clearly stuck, waiting on the wrong action, or running an unwanted command.

Prefer plain `halt` when you want the agent to stop cleanly. It now sends `C-c`, waits briefly, and only escalates if needed.

Prefer `halt --timeout <duration>` when the instance may need a little time to exit after interruption.

Prefer `halt --immediately` only when the user explicitly wants a hard stop or graceful interruption is no longer useful.

Treat `--stable` and `--timeout` as accepting either plain millisecond integers such as `1500` or Go duration strings such as `1500ms` and `1.5s`.

## Output Handling

Read these top-level JSON fields first:

1. `ok`
2. `command`
3. `instance`
4. `reused`
5. `status`
6. `error_code`
7. `error`

Read `data.content` as the primary screen text for `capture`.

For `wait`, read `status` first. `data.stable_for_ms` is auxiliary wait metadata; no content is returned.

Treat `reused: true` as confirmation that `summon` attached to an existing instance instead of creating a new one.

Treat `status: exited` as a deliberate stopped instance.

Treat `status: lost` as a broken or missing runtime that may require a fresh `summon`.

Treat `status: busy` as a recent-activity hint, not a permanent lock. It may automatically degrade back to `idle` if the configured busy TTL expires.

Treat `status` from `inspect --json`, `list --json`, or `wait` as the authoritative state signal.

Do not depend on `pane_title` to determine state. It is optional observational metadata and may be useful for debugging, but state decisions should be based on `status`.

For lightweight single-instance status checks, prefer reading `status` from `inspect --json` instead of using `capture`.

Use `capture` when you need current screen text.

## Recovery

When `template_not_found` appears, run:

```bash
agentmux template list --json
```

When `instance_not_found` appears, run:

```bash
agentmux list --json
```

Then decide whether to `summon` a new instance.

Use `wait` when you need blocking completion detection; use `capture` when you need the current screen immediately.

When `wait` returns `error_code: capture_timeout`, assume the work may still be running and inspect with:

```bash
agentmux capture <instance-name> --history 20 --json
```

When `process_not_running` or `status: exited` appears, decide whether the user wants a fresh instance or whether work should stop.

When `invalid_key` appears, retry with a supported key such as `Enter`, `C-c`, `Escape`, `Up`, `Down`, or `Tab`.

When `halt` does not stop the instance fast enough, retry with a longer `--timeout` or escalate to `--immediately` if the user wants a hard kill.

When a command seems unexpectedly missing, run:

```bash
agentmux version --json
agentmux help <command>
```

## Working Patterns

Naming convention:

1. Use descriptive names with task context instead of generic names like `worker1`.
2. For parallel workers, use a shared prefix plus a shard or scope suffix such as `wiki审核-Q1to5` and `wiki审核-Q6to10`.
3. For long-lived instances, include purpose or date if it helps avoid reuse mistakes.

Parallel independent work:

```bash
agentmux summon --template 深度编码专家 --name task-A --json
agentmux summon --template 深度编码专家 --name task-B --json
agentmux list --json
```

Long-running task monitoring:

1. Do not rely only on `wait` for long tasks if you also need progress details.
2. Prefer periodic `capture` with enough history to see the latest reasoning or tool output.
3. Use `list` or `inspect` for cheap status checks between captures.

Example polling loop:

```bash
agentmux inspect wiki审核-A --json
agentmux capture wiki审核-A --history 20 --json
```

Deliverable verification pattern:

1. Use `capture` or `wait` to observe that the agent believes it is finished.
2. Read the expected output file or artifact directly.
3. Decide whether to accept, ask for fixes, or interrupt and redirect.
