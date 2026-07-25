---
name: query-akshare
description: 使用 akqry CLI 发现、检查和调用 AkShare 金融数据接口，并用 Python 分析 A 股、港股、行业与概念板块、公募基金和 ETF 数据。用户提到 AkShare，或需要查询、筛选、比较、统计这些中国市场金融数据并保留可追溯的数据来源、参数和时间时使用。
---

# Query AkShare

使用 `akqry` 取得原始数据，用 pandas 完成分析。不要把 AkShare 接口或数据源的字段语义凭记忆补全。

## 运行前检查

先确认工具和实际 AkShare 版本；不要为普通查询自动安装或升级依赖。

```bash
akqry doctor --json
```

若 `akqry` 不在 `PATH`，按其仓库 README 安装。若需要使用本地 AkShare 源码，在每个命令中显式传入 `--akshare-path /path/to/akshare`，并在报告中保留输出的版本与模块路径。

## 标准工作流

1. 从问题明确市场、证券类型、代码、时间区间、频率、复权口径、币种和所需指标；不明确时在结论中标出假设。
2. 使用 `akqry search` 找候选接口；再使用 `akqry describe` 确认签名、参数、字段和数据源。运行时签名优先于文档记忆。
3. 先调用一次带 `--require-columns` 的查询，检查返回行数、字段和数据日期。查询命令默认拒绝空结果。
4. 用 `--output` 写入完整原始结果；默认保留 `.meta.json` sidecar。不要只依赖终端预览做分析。
5. 在独立 Python 脚本中读取落盘数据、显式进行日期对齐和计算；保留脚本或关键公式。
6. 报告结论时附上函数名、参数、AkShare 版本、获取时间、最后数据日期、数据源、复权方式、币种、缺失和限制。

示例：

```bash
akqry search 'ETF 历史行情' --domain etf --json
akqry describe fund_etf_hist_em --json
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

## 数据完整性规则

- 始终将证券和基金代码视为字符串；保留前导零。
- 不隐式选择复权、汇率、单位、填充方式或交易日日历。比较价格收益前，确认所有序列使用相同的价格口径。
- 当前板块成份股不是历史成份股；不得用当前成份解释过去的板块表现而不标注幸存者偏差。
- 基金持仓和行业配置存在披露滞后；使用披露期，而非“当前持仓”措辞。
- A 股和港股的货币与交易日不同；跨市场比较必须说明是否换汇，以及如何按日期对齐。
- “实时”结果在休市日可能是最后一次快照。根据返回的数据日期和更新时间表述，不要把执行日期当作行情日期。
- 不混用东财、同花顺、新浪等不同分类体系或指标口径，除非明确说明。
- `akqry fetch` 的 sidecar 是可追溯性的一部分；除非用户明确不要，否则不要使用 `--no-sidecar`。

读取 [data-integrity.md](references/data-integrity.md) 处理收益、数据日期、单位、空值、复权和报告口径。

## 按领域选择参考

- A 股个股、行情、财务和 A/H 对照：读取 [a-shares.md](references/a-shares.md)。
- 港股行情、资料、财务、港股通和币种问题：读取 [hk-shares.md](references/hk-shares.md)。
- 行业/概念板块排名、成份、历史指数：读取 [boards.md](references/boards.md)。
- 公募基金、ETF 行情、净值、持仓、规模和分红：读取 [funds-etfs.md](references/funds-etfs.md)。
- 多标的收益、波动率、回撤、相关性或图表：读取 [analysis.md](references/analysis.md)。

## 分析纪律

对一个简单单表问题，可在查询结果上筛选、排序和汇总。对多标的或时间序列问题：先分别落盘，再用 pandas 合并；不要把预览行当作完整数据。

`akqry.metrics` 仅提供不带投资假设的基础函数。年化频率必须显式传入；不要把 Sharpe、Alpha、目标收益或投资建议伪装成数据事实。
