---
name: query-akshare
description: 使用 akqry CLI 发现、检查和调用 AkShare 金融数据接口，并用 Python 分析 A 股、港股、行业与概念板块、公募基金、ETF 和指数数据。用户提到 AkShare，或需要查询、筛选、比较、统计这些中国市场金融数据并保留可追溯的数据来源、参数和时间时使用。
---

# Query AkShare

使用 `akqry` 取得原始数据，用 pandas 完成分析。不要把 AkShare 接口名、参数取值或字段语义凭记忆补全——用 `describe` 和 `--probe` 从运行时确认。

## 运行前检查

先确认工具和实际 AkShare 版本；不要为普通查询自动安装或升级依赖。

```bash
akqry doctor --json
```

若 `akqry` 不在 `PATH`，从本仓库安装：`uv tool install --editable 'tools/akqry[parquet]'`。若需要使用本地 AkShare 源码，在每个命令中显式传入 `--akshare-path /path/to/akshare`，并在报告中保留输出的版本与模块路径。

`doctor` 会报告可用的输出格式。若 `parquet` 不可用，改用 `.jsonl` 落盘，不要因为示例写着 parquet 就照抄。

## 标准工作流

1. 从问题明确市场、证券类型、代码、时间区间、频率、复权口径、币种和所需指标；不明确时在结论中标出假设。
2. 用 `akqry search` 找候选接口，尽量带 `--domain` 收窄；再用 `akqry describe` 确认签名、参数枚举和数据源。运行时签名优先于文档记忆。
3. 用 `akqry describe <function> --probe` 拿到真实列名、dtype 和日期区间，再据此决定 `--require-columns`。不要凭记忆猜中文列名。
4. 用 `--output` 写入完整原始结果；默认保留 `.meta.json` sidecar。不要只依赖终端预览做分析。
5. 在独立 Python 脚本中读取落盘数据、显式进行日期对齐和计算；保留脚本或关键公式。
6. 报告结论时附上函数名、参数、AkShare 版本、获取时间、最后数据日期、数据源、复权方式、币种、缺失和限制。

```bash
akqry search 'ETF 历史行情' --domain etf --json
akqry describe fund_etf_hist_em --json
akqry describe fund_etf_hist_em --probe --arg symbol=513500 --json

akqry fetch fund_etf_hist_em \
  --arg symbol=513500 \
  --arg period=daily \
  --arg start_date=20250101 \
  --arg end_date=20251231 \
  --arg adjust=qfq \
  --require-columns 日期,收盘,成交量,成交额 \
  --output /tmp/513500.parquet \
  --json
```

`search` 结果自带 `description`、`source_site`、`source_url` 和 `match_reasons`；先读这些再决定用哪个接口，不要只看函数名。

## 多标的与重复查询

多个标的用 `--for-each` 在一个进程内顺序取，`--output` 必须含 `{}` 占位符；每个产物各自带 sidecar。

```bash
akqry fetch stock_zh_a_hist \
  --for-each symbol=000001,600519,300750 \
  --arg start_date=20250101 --arg end_date=20250630 --arg adjust=qfq \
  --output '/tmp/a/{}.parquet' --json
```

批量中个别标的失败不会丢弃其他结果：信封返回 `partial_failure`，`details.failed_labels` 列出失败项，成功的产物已落盘。

反复调试同一个分析时加 `--cache-dir ~/.cache/akqry` 复用相同查询，避免重复打上游、触发限流。缓存命中时 `provenance.cache.hit` 为 `true`，`retrieved_at_utc` 仍是**原始获取时间**——报告时按它表述，不要说成刚取的数。

## 错误码与下一步

失败信封的 `code` 稳定，`details` 带恢复所需信息。按下表处理，不要盲目重试或直接放宽约束。

| code | 下一步 |
| --- | --- |
| `empty_result` | 检查代码格式（是否需要 `sh`/`sz` 前缀）、日期区间、是否休市或未上市。**不要**直接加 `--allow-empty` 掩盖。 |
| `missing_required_columns` | 读 `details.available_columns`，改用真实列名重跑；必要时先 `--probe`。 |
| `invalid_parameter` | 读 `details.allowed_values` / `accepted_parameters` / `signature`。确认文档过期才用 `--allow-unknown-values`。 |
| `missing_parameter` | 按 `details.signature` 补必填参数。 |
| `usage_error` | 命令行用法错，`details` 指出具体参数；批量 `--output` 必须含 `{}`。 |
| `function_not_found` | 用 `akqry search` 找当前接口名；跨版本会改名。 |
| `unsafe_callable` | 该 callable 被排除，换数据接口。 |
| `upstream_error` | 读 `details.exception_message` 判断是网络、限流还是参数问题；退避重试一次，仍失败则换数据源（`_em` → `_sina` / `_ths`）并在报告中注明换源。 |
| `query_timeout` | 缩小日期区间或降低频率，必要时提高 `--timeout`。 |
| `dependency_missing` | 按 `details.remedy` 处理，或改用 `.jsonl` 输出。 |
| `akshare_import_failed` | 用 `akqry doctor --json` 定位；向用户报告缺什么，不要自行升级或改动环境依赖。 |
| `output_exists` | 换路径，或确认要覆盖再加 `--overwrite`。 |
| `write_failed` / `serialization_failed` | 先创建父目录，或换 `--format`；同一路径重试不会自愈。 |
| `partial_failure` | 批量里部分标的失败。成功产物已落盘，只针对 `details.failed_labels` 重取，并在结论中说明缺哪些标的。 |

## 数据完整性规则

- 始终将证券和基金代码视为字符串；保留前导零。
- 不隐式选择复权、汇率、单位、填充方式或交易日日历。比较价格收益前，确认所有序列使用相同的价格口径。
- 当前板块成份股不是历史成份股；不得用当前成份解释过去的板块表现而不标注幸存者偏差。
- 基金持仓和行业配置存在披露滞后；使用披露期，而非"当前持仓"措辞。
- A 股和港股的货币与交易日不同；跨市场比较必须说明是否换汇，以及如何按日期对齐。
- "实时"结果在休市日可能是最后一次快照。根据返回的数据日期和更新时间表述，不要把执行日期当作行情日期。
- 不混用东财、同花顺、新浪等不同分类体系或指标口径，除非明确说明；`search` 结果里的 `source_site` 就是判断依据。
- `akqry fetch` 的 sidecar 是可追溯性的一部分；除非用户明确不要，否则不要使用 `--no-sidecar`。

读取 [data-integrity.md](references/data-integrity.md) 处理收益、数据日期、单位、空值、复权和报告口径。

## 按领域选择参考

- A 股个股、行情、财务和 A/H 对照：读取 [a-shares.md](references/a-shares.md)。
- 港股行情、资料、财务、港股通和币种问题：读取 [hk-shares.md](references/hk-shares.md)。
- 行业/概念板块排名、成份、历史指数：读取 [boards.md](references/boards.md)。
- 公募基金、ETF 行情、净值、持仓、规模和分红：读取 [funds-etfs.md](references/funds-etfs.md)。
- 指数点位、基准对比（沪深300/中证500 等）：读取 [indices.md](references/indices.md)。
- 多标的收益、波动率、回撤、相关性或图表：读取 [analysis.md](references/analysis.md)。

## 分析纪律

对一个简单单表问题，可在查询结果上筛选、排序和汇总。对多标的或时间序列问题：先分别落盘，再用 pandas 合并；不要把预览行当作完整数据——`--preview-rows` 只是抽样，横截面排名必须在落盘的完整表上算。

`akqry.metrics` 仅提供不带投资假设的基础函数。年化频率必须显式传入；`performance_summary` 与 `alignment_report` 返回的 `warnings`（丢弃观测、缺口天数、对齐损失）必须一并报告。不要把 Sharpe、Alpha、目标收益或投资建议伪装成数据事实。
