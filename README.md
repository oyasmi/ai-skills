# ai-skills

这里是一组可直接安装到 Agent skills 目录的 skills。每个 skill 都是自包含的目录：复制对应目录即可使用，不需要复制整个仓库；skill 依赖的 CLI 或其他工具会在它自己的 `SKILL.md` 中说明安装方法。

## 可用 skills

| Skill | 用途 |
|---|---|
| [`skills/agentmux`](skills/agentmux) | 通过 `agentmux` 委派和管理外部 coding agent。使用前请按 [`SKILL.md`](skills/agentmux/SKILL.md) 安装 `agentmux` CLI。 |
| [`skills/aiquota`](skills/aiquota) | 通过 `aiquota` 查看 AI 编程订阅的额度用量与重置时间。使用前请安装 `aiquota` CLI。 |
| [`skills/cookbook-forge`](skills/cookbook-forge) | 研究、写作并构建可离线阅读的中文 HTML cookbook。需要 Node.js 来运行模板脚本。 |
| [`skills/query-akshare`](skills/query-akshare) | 使用 akqry 查询并分析 A 股、港股、行业板块、基金与 ETF 的可追溯数据。 |

## 安装 skill

以 Codex 为例，把 skill 目录复制到 `$CODEX_HOME/skills/`；未设置
`CODEX_HOME` 时通常是 `~/.codex`：

```bash
cp -R skills/agentmux "${CODEX_HOME:-$HOME/.codex}/skills/agentmux"
cp -R skills/aiquota "${CODEX_HOME:-$HOME/.codex}/skills/aiquota"
cp -R skills/cookbook-forge "${CODEX_HOME:-$HOME/.codex}/skills/cookbook-forge"
cp -R skills/query-akshare "${CODEX_HOME:-$HOME/.codex}/skills/query-akshare"
```

其他 Agent 通常也遵循相同的约定：将目标 skill 目录直接放入它的 `skills/`
目录，并保留 `SKILL.md`、`references/`、`agents/`、`assets/` 等内容。

## 工具

`tools/` 存放 skill 可能依赖的独立工具，不属于 skill 的安装内容：

- [`tools/agentmux`](tools/agentmux)：`agentmux` CLI 的 Go 源码、配置示例和发布脚本。它的安装脚本只安装 CLI 和默认配置，不安装 skill。
- [`tools/aiquota`](tools/aiquota)：查看 AI 编程订阅额度用量的 Go CLI，供 `aiquota` skill 使用，也可独立使用。
- [`tools/akqry`](tools/akqry)：AkShare 数据接口发现、参数检查、查询与可追溯落盘 CLI；供 `query-akshare` skill 使用。
- [`tools/cmd_mgr`](tools/cmd_mgr)：跨平台命令管理 GUI，不参与本仓库的自动化测试/发布流程。
- [`tools/unsplash_wallpaper.py`](tools/unsplash_wallpaper.py)：仅依赖 Python 标准库的 macOS 壁纸轮换脚本，优先 Unsplash，自动回退至 Bing 每日图片和本地缓存。

### 自动更换 macOS 壁纸

需要 Python 3.9+，脚本可以单独复制使用，无需安装包、登录或 API key：

```bash
python3 tools/unsplash_wallpaper.py                    # 下载并设置壁纸
python3 tools/unsplash_wallpaper.py --download-only    # 仅下载，打印图片路径和来源
python3 tools/unsplash_wallpaper.py --offline          # 不联网，轮换已有图片
python3 tools/unsplash_wallpaper.py --keep 5 --width 2560
python3 tools/unsplash_wallpaper.py --help
```

脚本随机抽取 Unsplash **Wallpapers 主题**最近约 1,500 条内容里的免费横图，
排除 Unsplash+、低于 1920×1080 的图片和最近 200 次使用过的图片。
默认下载最大宽度 3840 的 JPEG，不放大原图。已经实测网站会返回 Anubis
反爬挑战；脚本使用标准库处理 `preact` / `fast` 挑战并保存 cookies，
计算挑战最多花费 10 秒，不依赖浏览器。私有列表接口或反爬机制可能变化，
失败时自动尝试 Bing 每日图片：优先今天，再选最近一周内尚未使用的图片，
使用 UHD 版本（不受 `--width` 控制）。两个来源都失败时，轮换本地缓存。
HTTP 429 会保存 Unsplash 的冷却时间，遵守 `Retry-After`，期间直接使用后备来源。

默认缓存目录为 `~/Library/Caches/unsplash-wallpaper`，可通过 `--cache-dir` 修改。
保留最多 8 张（`--keep`，至少 2 张），清理 30 天未使用的旧图（`--max-age`），
保护脚本当前设置的壁纸。下载、状态和 cookies 均原子写入；并发运行直接跳过，
不会同时下载或清理。图片来源记录在 `state.json` 中。

先在 Terminal 运行一次确认可用，再用当前桌面用户的 `crontab -e` 定时运行。
通过 `command -v python3` 确认 Python 的绝对路径；例如 Apple Silicon 的 Homebrew：

```cron
0 */3 * * * /opt/homebrew/bin/python3 /Users/oyasmi/projects/ai-skills/tools/unsplash_wallpaper.py --quiet
```

`--quiet` 只隐藏成功输出，错误和后备来源的提示保留在 stderr，方便 cron 记录。
cron 中请使用绝对路径，不依赖 PATH 或工作目录。脚本通过系统自带的
`osascript` 调用 AppKit，设置所有已连接显示器的**当前桌面**，不遍历其他 Spaces；
需要已登录的图形桌面会话。退出码：成功或并发跳过为 0，运行失败为 1，
参数错误为 2，中断为 130。

验证脚本的缓存、来源切换、反爬、限流和并发行为：

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools/tests -v
```

构建 Go 工具（以 agentmux 为例，aiquota 同理）：

```bash
cd tools/agentmux
go test ./...
go build -o ./bin/agentmux ./cmd/agentmux
```

制作单个 CLI 的本地发布包：

```bash
cd tools/agentmux
VERSION=v0.1.0 ./scripts/release.sh   # 产物在 dist/，只打 darwin_arm64 和 linux_amd64 两个平台
```

安装 akqry：

```bash
cd tools/akqry
uv tool install --editable '.[parquet]'
akqry doctor --json
```

## 发布

推送 `v*` 标签会触发 [`.github/workflows/release.yml`](.github/workflows/release.yml)，在一次 GitHub Release 里发布：

- `agentmux`、`aiquota`：Go 二进制压缩包，只保留 `darwin_arm64` 和 `linux_amd64` 两个平台（`agentmux_<version>_<os>_<arch>.tar.gz` / `aiquota_<version>_<os>_<arch>.tar.gz`），附各自的 checksums。
- `akqry`：与平台无关的 Python 包（wheel + sdist）。
- 每个 `skills/<name>` 目录单独打包成 `skill-<name>-<version>.tar.gz`，与上面的可执行文件产物完全分开；也可以直接从 `skills/` 目录复制安装，无需等发布。

也可以在 Actions 里手动触发该 workflow（`workflow_dispatch`）来验证构建，但只有打了 tag 的运行才会真正发布 Release。

## 开发检查

修改 Go 工具后，在对应的 `tools/agentmux/` 或 `tools/aiquota/` 下运行：

```bash
go test ./...
go vet ./...
```
