# agentmux

`agentmux` 是一个面向 AI Agent 的命令行控制器。它用隔离的 `tmux` session 运行外部终端 Agent，并提供适合编排器使用的实例管理、抓屏、输入注入和结构化输出。

当前目标平台：

1. macOS
2. Linux

Windows 不是首要目标。

## 特性

1. 使用独立 tmux socket `/tmp/agentmux.sock`
2. 不加载用户 `tmux.conf`
3. `1 instance = 1 tmux session`
4. 模板名和实例名支持中文
5. 关键命令支持 `--json`
6. `summon` 默认同名复用
7. `capture` 默认返回纯文本

## 依赖

运行时依赖：

1. `tmux >= 3.x`

构建依赖：

1. `Go >= 1.26`

## 构建

```bash
cd /path/to/agentmux
go build -o ./bin/agentmux ./cmd/agentmux
```

如果当前环境对 Go 默认缓存目录有限制，可以把缓存切到项目内：

```bash
GOCACHE=$PWD/.cache/go-build \
GOPATH=$PWD/.cache/go-path \
GOMODCACHE=$PWD/.cache/go-mod \
go build -o ./bin/agentmux ./cmd/agentmux
```

## 安装

最直接的安装方式：

```bash
cd /path/to/agentmux
./scripts/install.sh
```

这个脚本会做三件事：

1. 编译并安装二进制到 `~/.local/bin/agentmux`
2. 在不存在配置时安装默认配置到 `~/.config/agentmux/config.yaml`
3. 安装配套 skill 到 `$CODEX_HOME/skills/agentmux`，如果 `CODEX_HOME` 未设置则使用 `~/.codex/skills/agentmux`

可选环境变量：

1. `BIN_DIR=/custom/bin`
2. `INSTALL_SKILL=0`
3. `OVERWRITE_CONFIG=1`
4. `SKILL_TARGET_DIR=/custom/skills/agentmux`

示例：

```bash
BIN_DIR=$HOME/bin OVERWRITE_CONFIG=1 ./scripts/install.sh
INSTALL_SKILL=0 ./scripts/install.sh
```

安装完成后，建议先确认：

```bash
agentmux template list --json
```

## 发布

发布脚本：

```bash
./scripts/release.sh
```

默认会构建这些目标：

1. `darwin/amd64`
2. `darwin/arm64`
3. `linux/amd64`
4. `linux/arm64`

产物输出到 `dist/`，每个 tarball 包含：

1. `agentmux` 可执行文件
2. `config.yaml` 示例配置
3. `skill-agentmux/` skill 目录
4. `README.md`

可以通过环境变量覆盖版本号和输出目录：

```bash
VERSION=v0.1.0 ./scripts/release.sh
DIST_DIR=$PWD/out VERSION=v0.1.0 ./scripts/release.sh
```

发布完成后会额外生成：

```text
dist/checksums.txt
```

## 配置

主配置文件路径：

`~/.config/agentmux/config.yaml`

可以先复制示例配置：

```bash
mkdir -p ~/.config/agentmux
cp /path/to/agentmux/examples/config.yaml ~/.config/agentmux/config.yaml
```

示例配置文件见 [config.yaml](/Users/oyasmi/projects/ai-skills/agentmux/examples/config.yaml)。

## Skill

配套 skill 目录位于：

1. [SKILL.md](/Users/oyasmi/projects/ai-skills/agentmux/skills/agentmux/SKILL.md)
2. [openai.yaml](/Users/oyasmi/projects/ai-skills/agentmux/skills/agentmux/agents/openai.yaml)

这个 skill 面向上层编排型 Agent，要求优先通过 `agentmux ... --json` 管理外部终端 Agent 实例，而不是直接调用 `tmux`。

## 最小配置示例

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
```

## 常用命令

列出模板：

```bash
agentmux template list
agentmux template list --json
```

创建或复用实例：

```bash
agentmux summon --template 深度编码专家 --name 编码助手-A --cwd ~/work/project
```

创建并发送首条消息：

```bash
agentmux summon --template 深度编码专家 --name 编码助手-A --prompt "先阅读项目并总结结构" --json
```

查看实例详情：

```bash
agentmux inspect 编码助手-A --json
```

抓取稳定后的屏幕文本：

```bash
agentmux capture 编码助手-A --history 120 --stable 1500 --timeout 30s --json
```

继续发送消息：

```bash
agentmux prompt 编码助手-A --text "继续修复剩余失败测试" --enter --json
```

发送特殊键：

```bash
agentmux prompt 编码助手-A --key C-c --json
agentmux prompt 编码助手-A --key Enter --json
```

人工 attach：

```bash
agentmux attach 编码助手-A
```

停止实例：

```bash
agentmux halt 编码助手-A --json
```

## 命令语义

### `summon`

1. 同名实例存在时默认复用
2. 同名实例不存在时创建
3. 新建实例时，若给 `--prompt`，立即发送该消息
4. 复用实例时，若给 `--prompt`，也发送该消息
5. 复用实例时不会隐式修改既有实例配置

### `capture`

1. 始终通过 `tmux capture-pane` 抓纯文本
2. `--history` 控制向上抓取的历史行数
3. `--stable` 表示等待屏幕稳定后再返回

### `prompt`

1. `--text` 发送文本
2. `--key` 发送白名单特殊键
3. `--enter` 为文本发送后额外补一个 `Enter`

## 输出格式

面向 Agent 使用时，优先添加 `--json`。

成功输出示例：

```json
{
  "ok": true,
  "command": "inspect",
  "instance": "编码助手-A",
  "status": "idle",
  "data": {}
}
```

错误输出示例：

```json
{
  "ok": false,
  "command": "capture",
  "instance": "编码助手-A",
  "error_code": "instance_not_found",
  "error": "instance \"编码助手-A\" not found"
}
```

## 项目文档

设计和规格文档位于：

1. [design.md](/Users/oyasmi/projects/ai-skills/agentmux/docs/design.md)
2. [cli-spec.md](/Users/oyasmi/projects/ai-skills/agentmux/docs/cli-spec.md)
3. [config-spec.md](/Users/oyasmi/projects/ai-skills/agentmux/docs/config-spec.md)
4. [skill-spec.md](/Users/oyasmi/projects/ai-skills/agentmux/docs/skill-spec.md)
