# 安装与环境自检

这个 skill 需要 `agentmux` CLI；skill 本身不包含二进制文件。

## 先自检，再假设没装

```bash
agentmux doctor --json
```

`doctor` 一次性检查：这次调用实际跑的是哪个二进制、PATH 上是否有旧版本在遮蔽它（这是"装了新版本但行为还是老的"最常见的原因）、配置和状态目录、注册表锁、每个模板命令是否能在 PATH 上找到、以及需要 tmux 的模板是否真的有 tmux。只有 `fail` 状态会让退出码非零；`warn` 是提醒但不阻塞。

命令不存在（`command not found: agentmux`）时才需要安装；如果命令存在但行为与文档不符，先看 `doctor` 的 `path` 检查项，而不是怀疑 agentmux 有 bug。

## 从本仓库安装

```bash
cd /path/to/ai-skills/tools/agentmux
./scripts/install.sh
```

脚本会把 CLI 安装到 `~/.local/bin/agentmux`（版本号取自 `git describe`，用于让 `doctor`/`version --json` 能区分新旧构建），并在需要时写入默认配置 `~/.config/agentmux/config.yaml`。安装完成后脚本会自检一次：如果 PATH 上一个 `agentmux` 解析到别的路径（例如 `~/bin` 排在 `~/.local/bin` 前面），会打印警告——按提示调整 PATH 顺序或删掉旧副本，否则新特性不会生效。

也可以只构建到本地目录，不装进 PATH：

```bash
cd /path/to/ai-skills/tools/agentmux
go build -o ./bin/agentmux ./cmd/agentmux
```

## 使用发布包安装（无需克隆仓库）

仓库在 GitHub 公开发布：https://github.com/oyasmi/ai-skills 。发布包只提供 `darwin_arm64` 和 `linux_amd64` 两个平台的 `agentmux` 二进制；其他平台用上面「从本仓库安装」的源码构建方式。

```bash
set -euo pipefail
REPO="oyasmi/ai-skills"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64) ARCH=amd64 ;;
esac
[[ "$OS-$ARCH" == "darwin-arm64" || "$OS-$ARCH" == "linux-amd64" ]] || {
  echo "没有 $OS/$ARCH 的发布包，改用源码构建" >&2; exit 1
}

TMP="$(mktemp -d)"
curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" -o "$TMP/release.json"
urls() { grep -oE '"browser_download_url": *"[^"]+"' "$TMP/release.json" | grep -oE 'https://[^"]+'; }
ASSET_URL="$(urls | grep -E "agentmux_.*_${OS}_${ARCH}\.tar\.gz$")"
CHECKSUMS_URL="$(urls | grep -E "checksums-agentmux\.txt$")"
ASSET_FILE="$(basename "$ASSET_URL")"

curl -fsSL -o "$TMP/$ASSET_FILE" "$ASSET_URL"
curl -fsSL -o "$TMP/checksums-agentmux.txt" "$CHECKSUMS_URL"
(cd "$TMP" && grep "$ASSET_FILE" checksums-agentmux.txt | sha256sum -c -)

tar -C "$TMP" -xzf "$TMP/$ASSET_FILE"
mkdir -p ~/.local/bin
install -m 0755 "$TMP"/agentmux_*_"${OS}_${ARCH}"/agentmux ~/.local/bin/agentmux
rm -rf "$TMP"

agentmux doctor --json
```

装到别的目录用 `BIN_DIR=<path>` 替换 `~/.local/bin`；需要固定版本而不是最新版时，把 `releases/latest` 换成 `releases/tags/<version>`（如 `releases/tags/v0.5.1`），可用版本见 [Releases 页面](https://github.com/oyasmi/ai-skills/releases)。装完同样先跑 `agentmux doctor --json` 确认。

## 外部依赖

运行时还需要按所选模板安装对应的外部 CLI；基于 tmux 的模板需要 `tmux`，结构化模板还需要 `claude`、`codex` 或 `pi`。`doctor` 的 `template:*` 检查项会逐个模板报告命令是否在 PATH 上；具体模板命令也可通过 `agentmux template list --json` 查看。
