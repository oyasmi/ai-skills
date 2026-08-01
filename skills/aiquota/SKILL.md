---
name: aiquota
description: 通过 `aiquota` CLI 查看 AI 编程订阅的额度用量与重置时间（Claude / Codex / z.ai / 自定义渠道），只读、支持 --json。用户询问「额度还剩多少」「什么时候刷新」「5小时/一周用了多少」，或需要在决策前判断额度是否够用时使用。
---

# aiquota

只读查额度，一条命令：

```bash
aiquota --json
```

## 速查

```bash
aiquota                          # 文本表格，人看
aiquota --json                   # 结构化输出，Agent 解析用这个
aiquota --provider claude        # 只查一个渠道，逗号分隔可查多个
aiquota --config <path>          # 覆盖配置文件路径（一般不需要）
```

不需要参数、不需要登录步骤——渠道是否可查完全由配置文件决定（见下）。

## 读 `--json` 输出

```jsonc
{
  "providers": [
    {
      "id": "claude",              // claude | codex | zai | custom:<id>
      "name": "Claude",
      "plan": "Claude Code",
      "state": "ok",                // ok | no_data | error
      "windows": [
        {
          "label": "5小时",
          "used_percent": 13,
          "remaining_percent": 87,
          "reset_at": "2026-08-01T20:10:00Z",
          "reset_in_seconds": 16727
        }
      ],
      "valid_until": "2026-08-09T08:22:32Z"  // 订阅到期时间，若有
    }
  ]
}
```

- 一个 provider 可能有多个 `windows`（如 Claude 的 5 小时窗口 + 一周窗口）；判断"够不够用"时看关心的那个窗口的 `remaining_percent`，别只看第一个。
- `state: no_data` = 这个渠道没登录/没配置 token，不是故障，不用重试。
- `state: error` 附带的说明文字已经是给人看的中文，直接引用即可，不用二次解释。
- 拿不到某个渠道的数据不代表命令失败——`aiquota` 退出码只反映命令本身有没有跑起来，渠道级别的状态要看每条 `state`。

## 配置

渠道开关（Claude/Codex/z.ai 是否启用、z.ai token、自定义渠道）全部来自配置文件，`aiquota` 只读不写：

```
~/.config/quota-list/config.json
```

这份配置和桌面应用 QuotaList 共用——用户已经在 QuotaList 里配置过的渠道，`aiquota` 直接可用，不用重复配置；如果用户还没装过 QuotaList，Claude/Codex 走各自 CLI 的本地登录信息即可，无需额外操作。**不要**编写代码或建议用户手动改这份配置文件去"新增渠道"，除非用户明确要求配置自定义渠道（此时可编辑 `customProviders[]`，字段和 QuotaList 一致）。
