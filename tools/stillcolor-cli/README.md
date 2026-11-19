# stillcolor-cli

把 [Stillcolor](https://github.com/aiaf/Stillcolor) 的时间抖动控制做成 macOS 命令行工具。适用于 Apple 芯片 Mac 和 macOS 13 及以上。可在终端运行，也可由 [Barback](https://github.com/oyasmi/barback) 提供菜单入口、登录自启和日志。stillcolor-cli 本身无窗口或菜单栏图标。

## 编译和安装

需要 Xcode Command Line Tools（`xcode-select --install`）。在 Mac 上以日常使用的账户，从本仓库根目录运行以下命令，默认安装到 `~/.local/bin/stillcolor-cli`。

```sh
cd tools/stillcolor-cli
make
make install
"$HOME/.local/bin/stillcolor-cli" status
```

## 命令

| 命令 | 用途 |
| --- | --- |
| `stillcolor-cli status` | 显示保存的策略和各匹配驱动当前报告的 `enableDither` 值 |
| `stillcolor-cli off` | 保存并立即应用关闭时间抖动的策略 |
| `stillcolor-cli on` | 保存并立即应用开启时间抖动的策略 |
| `stillcolor-cli apply` | 执行一次已保存的策略 |
| `stillcolor-cli watch` | 前台运行，启动时应用策略，显示重配置或睡眠唤醒后重新应用。另有约 30 秒一次的补查 |

首次运行时，默认策略为 `off`。`status` 只读显示硬件，运行 `on`、`off`、`apply` 或启动 `watch` 时才会改变它。策略保存在 `~/Library/Application Support/stillcolor-cli/dithering`，每次重应用都会读入最新值。即使当前驱动写入失败，`on` 和 `off` 也会保留新策略供下次重试。

重复运行 `on` 或 `off` 会复用已保存的策略，只更新当前状态与策略不一致的显示驱动。

`watch` 收到 `SIGTERM` 或 `Ctrl+C` 后退出。当前硬件设置保持到系统或其他程序改写它。如需开启时间抖动，运行 `stillcolor-cli on`。通常退出旧版 Stillcolor 并关闭其登录自启后，再由 Barback 接管。

工具使用 Stillcolor 相同的 `IOMobileFramebufferAP` 驱动匹配和 `enableDither` 属性，覆盖匹配到的内置和外置显示驱动。这是苹果显示驱动的属性，覆盖范围随硬件和系统版本而异。`status` 中的 `unknown` 表示该驱动未提供可识别的属性值。一次性命令退出码为 `0`（成功）、`1`（配置或驱动错误）或 `2`（参数错误）。

## 接入 Barback

要在插拔屏幕和唤醒后自动恢复策略，在 Barback 右键菜单中选择「从 supervisor 粘贴导入」。复制 [examples/barback.ini](examples/barback.ini) 的内容，把 `/Users/YOUR_USER` 换成 Mac 上的实际用户主目录，再导入并运行 `stillcolor-watch`。导入后可以在服务设置中打开「自动启动」，并在 Barback 偏好设置中打开「登录时启动」。

在配置窗口新建以下**一次性命令**。将示例改成实际绝对路径，Shell 选项保持关闭。完成后可直接在 Barback 面板点「运行」，在本次输出中查看结果。

| 名称 | 用途 | 命令 |
| --- | --- | --- |
| `stillcolor-off` | 关闭时间抖动 | `"/Users/YOUR_USER/.local/bin/stillcolor-cli" off` |
| `stillcolor-on` | 开启时间抖动 | `"/Users/YOUR_USER/.local/bin/stillcolor-cli" on` |
| `stillcolor-status` | 查看时间抖动 | `"/Users/YOUR_USER/.local/bin/stillcolor-cli" status` |

只需要手动切换时，可以只添加这三个条目。启用常驻服务后，三个条目仍然可用。

## 验证

`make test` 使用模拟驱动，可以在 macOS 或 Linux 上验证命令解析、策略持久化、驱动写入失败后的重试，以及运行中的 watcher 读取最新策略。

在 Mac 上可以先运行 `off` 和 `status`，确认匹配驱动报告 `enableDither=off`。再由 Barback 启动 `watch`，运行 `on`，插拔外接显示器或睡眠唤醒后再次查看状态，确认最后一次选择仍然生效。最后从 Barback 停止服务，检查服务日志中的正常退出记录。

仍沿用原版的 MIT 许可证。
