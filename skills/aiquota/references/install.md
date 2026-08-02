# 安装与环境自检

这个 skill 需要 `aiquota` CLI；skill 本身不包含二进制文件。

## 先自检，再假设没装

```bash
command -v aiquota && aiquota --json
```

`aiquota` 没有 `doctor` 子命令。命令能跑但某个渠道读不到数据，问题通常在配置文件（`~/.config/quota-list/config.json`）或对应 CLI 的本地登录状态，不是 `aiquota` 本身有 bug——先看 `--json` 输出里该渠道的 `state`/`error` 字段。

## 使用发布包安装（无需克隆仓库）

仓库在 GitHub 公开发布：https://github.com/oyasmi/ai-skills 。发布包只提供 `darwin_arm64` 和 `linux_amd64` 两个平台的 `aiquota` 二进制；其他平台用下面「从本仓库安装」的源码构建方式。

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
ASSET_URL="$(urls | grep -E "aiquota_.*_${OS}_${ARCH}\.tar\.gz$")"
CHECKSUMS_URL="$(urls | grep -E "checksums-aiquota\.txt$")"
ASSET_FILE="$(basename "$ASSET_URL")"

curl -fsSL -o "$TMP/$ASSET_FILE" "$ASSET_URL"
curl -fsSL -o "$TMP/checksums-aiquota.txt" "$CHECKSUMS_URL"
(cd "$TMP" && grep "$ASSET_FILE" checksums-aiquota.txt | sha256sum -c -)

tar -C "$TMP" -xzf "$TMP/$ASSET_FILE"
mkdir -p ~/.local/bin
install -m 0755 "$TMP"/aiquota_*_"${OS}_${ARCH}"/aiquota ~/.local/bin/aiquota
rm -rf "$TMP"

aiquota --json
```

装到别的目录用 `BIN_DIR=<path>` 替换 `~/.local/bin`；需要固定版本而不是最新版时，把 `releases/latest` 换成 `releases/tags/<version>`（如 `releases/tags/v0.5.1`），可用版本见 [Releases 页面](https://github.com/oyasmi/ai-skills/releases)。

## 从本仓库安装

```bash
cd /path/to/ai-skills/tools/aiquota
scripts/install.sh
```

脚本把 CLI 装到 `~/.local/bin/aiquota`（`BIN_DIR=<path>` 可覆盖）。也可以只构建到本地目录，不装进 PATH：

```bash
cd /path/to/ai-skills/tools/aiquota
go build -o ./bin/aiquota ./cmd/aiquota
```

## 配置

`aiquota` 和桌面应用 QuotaList 共用同一份配置文件 `~/.config/quota-list/config.json`，只读不写——渠道开关、z.ai token、自定义渠道都在这里配置。未装过 QuotaList 时使用出厂默认值（Claude/Codex 默认开启），Claude/Codex 走各自 CLI 的本地登录信息即可。
