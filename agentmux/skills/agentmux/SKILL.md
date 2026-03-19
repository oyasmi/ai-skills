---
name: agentmux
description: Control, reuse, and inspect external terminal AI agent instances through the `agentmux` CLI. Use when you need to start or continue work inside an isolated tmux-backed instance, manage multiple coding agents or TUI agents, capture stable screen text, send the next prompt or key, or attach for human debugging. Trigger this skill when the user explicitly mentions `agentmux`, asks to orchestrate another terminal agent, or needs a reusable external coding/documentation/workflow assistant running in its own terminal session.
---

# Agentmux

Use `agentmux` instead of calling `tmux` directly. Treat `agentmux` as the only control surface for external terminal agent instances.

## Core Rules

1. Prefer `agentmux ... --json` so you can consume stable machine-readable fields.
2. Run `agentmux list --json` before assuming an instance does or does not exist.
3. Run `agentmux template list --json` before choosing a template if the available templates are not already known.
4. Use `summon` to create or reuse an instance. Do not call `tmux new-session` yourself.
5. Use `capture` to read screen state. Do not read raw tmux output or terminal stdout directly.
6. Use `prompt` to send the next text or special key after you inspect current state.
7. Use `attach` only when a human explicitly asks to watch or debug interactively.

## Standard Workflow

1. List templates if the correct role template is unclear.
2. List instances if the correct instance is unclear.
3. Summon the target instance by template and optional name.
4. Capture stable screen text before deciding the next action.
5. Prompt the instance with the next instruction or key.
6. Repeat `capture -> decide -> prompt` until the task reaches a stopping point.

Typical loop:

```bash
agentmux list --json
agentmux summon --template 深度编码专家 --name 编码助手-A --json
agentmux capture 编码助手-A --history 120 --stable 1500 --json
agentmux prompt 编码助手-A --text "继续修复剩余失败测试" --enter --json
```

## Command Selection

Use `summon` when you need to create or reuse an instance.

```bash
agentmux summon --template 深度编码专家 --name 编码助手-A --cwd /path --json
```

Use `summon --prompt` when the same call should also send a message.

```bash
agentmux summon --template 深度编码专家 --name 编码助手-A --prompt "先阅读项目并总结结构" --json
```

Use `inspect` when you need metadata such as template, cwd, model, status, or session identity.

```bash
agentmux inspect 编码助手-A --json
```

Use `capture` when you need the visible screen text and recent history.

```bash
agentmux capture 编码助手-A --history 120 --stable 1500 --timeout 30s --json
```

Use `prompt` when the instance already exists and you want to separate control from creation.

```bash
agentmux prompt 编码助手-A --text "继续" --enter --json
agentmux prompt 编码助手-A --key C-c --json
```

Use `halt` when the instance should stop.

```bash
agentmux halt 编码助手-A --json
```

## Decision Rules

Prefer `inspect` and `capture` before sending another message. Avoid blind prompting.

Prefer reusing a named instance when the user is clearly continuing previous work in the same external agent.

Prefer short, task-specific prompts. Do not resend large repeated context if the instance already has it.

Send `C-c` before anything else when the instance is clearly stuck, waiting on the wrong action, or running an unwanted command.

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

Treat `reused: true` as confirmation that `summon` attached to an existing instance instead of creating a new one.

Treat `status: exited` as a deliberate stopped instance.

Treat `status: lost` as a broken or missing runtime that may require a fresh `summon`.

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

When `capture_timeout` appears, reduce the wait or capture immediately without `--stable`.

When `process_not_running` or `status: exited` appears, decide whether the user wants a fresh instance or whether work should stop.

When `invalid_key` appears, retry with a supported key such as `Enter`, `C-c`, `Escape`, `Up`, `Down`, or `Tab`.
