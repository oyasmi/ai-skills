# agentmux

`agentmux` 是一个面向 AI Agent 的命令行控制器。它用隔离的 `tmux` session 运行外部终端 Agent，并提供适合编排器使用的实例管理、抓屏、输入注入和结构化输出。

当前目标平台：

1. macOS
2. Linux

Windows 不是首要目标。

## 特性

1. 默认使用独立 tmux socket `/tmp/agentmux.sock`，且可通过配置修改
2. 不加载用户 `tmux.conf`
3. `1 instance = 1 tmux session`
4. 模板名和实例名支持中文
5. 关键命令支持 `--json`
6. `summon` 默认同名复用
7. `capture` 默认返回纯文本

## 近期优化

1. `prompt` 新增 `--stdin`，适合长文本与多行文本输入，避免 shell 参数长度限制
2. 新增 `version` 命令，支持纯文本和 `--json` 输出，便于 Agent 判断功能版本
3. 新增 `wait` 命令，只等待屏幕稳定，不返回内容，适合节省 token
4. `capture --stable` 与 `wait --stable` 支持整数毫秒和 Go duration 两种格式，例如 `1500`、`1500ms`、`1.5s`
5. `tmux` socket 路径从硬编码改为配置项 `defaults.tmux.socket`
6. `busy` 状态新增 TTL 自动退化，默认 `10s`，避免发送 prompt 后因缺少后续观测而永久停留在 `busy`
7. `instances.json` 现在使用文件锁和原子替换写入，降低多进程并发编排时的数据丢失和文件损坏风险
8. `capture`/`wait` 内部减少了一次重复的注册表事务，避免不必要的注册表读改写
9. 核心路径测试已补齐到 `capture`、`prompt`、`summon reuse`、`halt` 和 `naming`

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

也可以先通过帮助探索命令：

```bash
agentmux --help
agentmux help summon
agentmux capture --help
```

## 发布

发布脚本：

```bash
./scripts/release.sh
```

GitHub Actions 也已经配置为自动打包：

1. 创建 `v*` tag 时自动构建、上传 artifact，并发布 GitHub Release
2. 支持手动触发 `AgentMux Package` workflow

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

如果该文件不存在，`agentmux` 会在首次执行非帮助命令时自动写入默认配置。

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
  tmux:
    socket: /tmp/agentmux.sock
  status:
    busy_ttl_ms: 10000
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

只等待屏幕稳定，不返回内容：

```bash
agentmux wait 编码助手-A --stable 1500 --timeout 30s --json
```

继续发送消息：

```bash
agentmux prompt 编码助手-A --text "继续修复剩余失败测试" --enter --json
printf '%s\n' "很长的多行文本" "第二行" | agentmux prompt 编码助手-A --stdin --enter --json
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

查看版本：

```bash
agentmux version
agentmux version --json
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

### `wait`

1. 只等待屏幕稳定，不返回屏幕内容
2. 适合上层 Agent 只想阻塞等待、避免传回大段文本时使用

### `prompt`

1. `--text` 发送文本
2. `--stdin` 从标准输入读取完整文本
3. `--key` 发送白名单特殊键
4. `--enter` 为 `--text` 或 `--stdin` 发送后额外补一个 `Enter`

### `busy` 状态

1. `prompt` 后实例会进入 `busy`
2. 若后续执行 `capture --stable` 或 `wait`，状态会正常收敛回 `idle`
3. 若调用方没有继续观测，`busy` 会在 `defaults.status.busy_ttl_ms` 到期后自动退化为 `idle`
4. 若 `busy_ttl_ms: 0`，表示禁用自动退化，实例不会仅因 TTL 到期而自动回到 `idle`

## 并发安全

1. `instances.json` 的读改写现在在文件锁保护下执行
2. 注册表写入使用临时文件加原子替换，避免出现半写入 JSON
3. 这能显著降低多个 `agentmux` 进程并发操作同一注册表时的丢写风险

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
