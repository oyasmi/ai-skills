# aiquota

只读命令行工具：查看 AI 编程订阅的额度用量与重置时间（Claude / Codex / z.ai / 自定义渠道）。

是 [QuotaList](../../../quota-list)（macOS 菜单栏应用）的 CLI 对应物，面向 Agent/脚本场景：同一份配置文件，同一套渠道，输出改成文本表格或 `--json`。全程只读，不写入任何凭据或配置文件。

目标平台：Linux、macOS。

## 安装

```bash
scripts/install.sh
```

默认装到 `~/.local/bin/aiquota`，用 `BIN_DIR=<path>` 覆盖。

## 用法

```bash
aiquota                          # 文本表格：所有已启用渠道
aiquota --json                   # 结构化输出，供 Agent 解析（短参数 -j）
aiquota --provider claude,codex  # 只查询指定渠道（短参数 -p）
aiquota --config <path>          # 覆盖配置文件路径（短参数 -c）
aiquota --refresh                # 跳过节流缓存，强制重新查询上游（短参数 -r）
aiquota --timeout 30s            # 所有渠道并发请求共用的总体超时上限（默认 15s），不是单个请求各自的超时（短参数 -t）
```

每个长参数都有对应的单字母短参数（`-c` `-j` `-n` `-p` `-r` `-t` `-v`），可以组合使用，如 `aiquota -p claude -j`。运行 `aiquota -h` 查看完整列表。

```
$ aiquota
Claude  Claude Code
  5小时   13.0%    ↻ 4小时38分钟（08-02 12:53:45）
  一周    63.0%    ↻ 1天17小时（08-04 01:15:45）

Codex  Plus
  5小时    1.0%    ↻ 4小时38分钟（08-02 12:53:45）
  到期 2026-08-10 · 剩 8 天
```

所有时间（含 JSON 里的 `reset_at` / `valid_until` / `fetched_at` / `now`）都换算成本地时区显示，精确到秒；文本表格里每个额度窗口在倒计时后面额外带上重置的绝对时间。

## 配置

**和 QuotaList 共用同一份配置文件**：`~/.config/quota-list/config.json`（可用 `AIQUOTA_CONFIG` 或 `--config` 覆盖）。`aiquota` 只读该文件，从不写入——两边都读同一份配置，改一处即可，QuotaList 的设置面板或直接编辑配置文件对 `aiquota` 同样生效。

尚未装过 QuotaList、配置文件不存在时，`aiquota` 使用与 QuotaList 相同的出厂默认值（Claude/Codex 默认开启）。

- **Claude**：读取 `~/.claude/.credentials.json`（`claude` 登录后自动生成），调用 Claude Code OAuth usage API。QuotaList 的 Web-Cookie 数据源依赖 macOS 钥匙链里手动保存的浏览器 Cookie，命令行场景没有对应物，未实现。
- **Codex**：读取 `~/.codex/auth.json`，优先调用 ChatGPT 用量 API，接口不可用时回退最近一次本地 session 文件里的 `rate_limits`。
- **z.ai**：需要在配置文件里启用并填入 token（`zaiEnabled` + `zaiToken`），仅支持中国大陆端点。
- **自定义渠道**：`customProviders[]`，字段与 QuotaList 完全一致（`url` / `headers` / `windows[].usedPath` 等 keypath 映射 / `autoDetect`）。高级映射请直接编辑配置文件。

## 查询节流

工具调用的频次不等于应该发往上游的查询频次——尤其是被 Agent 反复调用时。每个渠道的查询结果和最近一次查询时间会缓存在本地（默认 `~/.cache/aiquota/`，一个渠道一个文件，可用 `AIQUOTA_CACHE_DIR` 覆盖），同一渠道两次真正的上游请求之间至少间隔 10 秒；间隔内的调用直接返回上一次缓存的结果（`fetched_at` 会显示数据实际取回的时间，不是本次调用时间）。缓存文件通过文件锁协调，多个并发的 `aiquota` 进程共享同一节流窗口。

一次性的上游失败（网络抖动、429 等）不会被缓存，下一次调用会立刻重试，不受 10 秒节流限制。需要绕过节流强制刷新时用 `--refresh`。

## 输出字段（`--json`）

```jsonc
{
  "ok": true,
  "now": "2026-08-01T23:31:13+08:00",
  "providers": [
    {
      "id": "claude",
      "name": "Claude",
      "plan": "Claude Code",
      "state": "ok",           // ok | no_data | error
      "source": "oauth_usage_api",
      "windows": [
        {
          "key": "5h",
          "label": "5小时",
          "used_percent": 13,
          "remaining_percent": 87,
          "reset_in_seconds": 16727,
          "reset_at": "2026-08-02T04:10:00+08:00"
        }
      ],
      "valid_until": "2026-08-09T16:22:32+08:00",  // 订阅/账号到期时间，若有
      "fetched_at": "2026-08-01T23:31:13+08:00"
    }
  ]
}
```

`state: no_data` 表示该渠道未登录/未配置（不是错误）；`state: error` 附带 `error` 说明原因（网络失败、token 失效等）。

## 退出码

`0` 表示命令本身成功执行并生成了报告——单个渠道 `no_data`/`error` 不影响退出码，需要看 `--json` 里每个 provider 的 `state`。`2` 是参数错误（如 `--provider` 未匹配到任何渠道），`1` 是配置文件读取/解析失败。
