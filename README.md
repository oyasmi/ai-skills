# ai-skills

这里是一组可直接安装到 Agent skills 目录的 skills。每个 skill 都是自包含的目录：复制对应目录即可使用，不需要复制整个仓库；skill 依赖的 CLI 或其他工具会在它自己的 `SKILL.md` 中说明安装方法。

## 可用 skills

| Skill | 用途 |
|---|---|
| [`skills/agentmux`](skills/agentmux) | 通过 `agentmux` 委派和管理外部 coding agent。使用前请按 [`SKILL.md`](skills/agentmux/SKILL.md) 安装 `agentmux` CLI。 |
| [`skills/cookbook-forge`](skills/cookbook-forge) | 研究、写作并构建可离线阅读的中文 HTML cookbook。需要 Node.js 来运行模板脚本。 |

## 安装 skill

以 Codex 为例，把 skill 目录复制到 `$CODEX_HOME/skills/`；未设置
`CODEX_HOME` 时通常是 `~/.codex`：

```bash
cp -R skills/agentmux "${CODEX_HOME:-$HOME/.codex}/skills/agentmux"
cp -R skills/cookbook-forge "${CODEX_HOME:-$HOME/.codex}/skills/cookbook-forge"
```

其他 Agent 通常也遵循相同的约定：将目标 skill 目录直接放入它的 `skills/`
目录，并保留 `SKILL.md`、`references/`、`agents/`、`assets/` 等内容。

## 工具

`tools/` 存放 skill 可能依赖的独立工具，不属于 skill 的安装内容：

- [`tools/agentmux`](tools/agentmux)：`agentmux` CLI 的 Go 源码、配置示例和发布脚本。它的安装脚本只安装 CLI 和默认配置，不安装 skill。
- [`tools/cmd_mgr`](tools/cmd_mgr)：跨平台命令管理 GUI。

构建 agentmux：

```bash
cd tools/agentmux
go test ./...
go build -o ./bin/agentmux ./cmd/agentmux
```

制作 CLI 发布包：

```bash
cd tools/agentmux
VERSION=v0.1.0 ./scripts/release.sh
```

发布包只包含 `agentmux` CLI、示例配置和工具 README；skill 始终从根目录的
`skills/` 单独安装。

## 开发检查

修改 Go 工具后，在 `tools/agentmux/` 下运行：

```bash
go test ./...
go vet ./...
```
