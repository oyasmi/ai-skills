# 时间序列与横截面分析

## 数据准备

先把每个标的独立落盘（多标的用 `akqry fetch --for-each`）；读取时保留代码为字符串。将日期列显式转换为 datetime，排序并检查重复日期。不要按 DataFrame 行号把两个标的拼在一起。

```python
import pandas as pd
from akqry.metrics import align_price_series, alignment_report, performance_summary

first = pd.read_parquet("/tmp/first.parquet").set_index("日期")["收盘"]
second = pd.read_parquet("/tmp/second.parquet").set_index("日期")["收盘"]

report = alignment_report({"first": first, "second": second})
prices = align_price_series({"first": first, "second": second})
summary = {
    name: performance_summary(prices[name], periods_per_year=252)
    for name in prices.columns
}
```

若使用 JSONL 或 CSV，按 sidecar 中的 schema 显式设置代码列 dtype，再转换日期。

## 横截面筛选

"今天涨幅前 10 的 ETF"这类问题必须在**落盘的完整表**上算。`--preview-rows` 只是抽样，直接用预览行排名会得到错误答案。

```bash
akqry fetch fund_etf_spot_em --require-columns 代码,名称,涨跌幅 --output /tmp/etf_spot.parquet --json
```

```python
import pandas as pd

spot = pd.read_parquet("/tmp/etf_spot.parquet")
spot["代码"] = spot["代码"].astype(str)
values = pd.to_numeric(spot["涨跌幅"], errors="coerce")
ranked = spot.assign(涨跌幅=values).dropna(subset=["涨跌幅"]).nlargest(10, "涨跌幅")
print(len(spot), "rows in;", int(values.isna().sum()), "unusable")
```

报告时说明：总样本数、被剔除的无效值数、快照对应的数据时间（读 sidecar），以及百分比字段本身已是百分点数，不要再乘 100。停牌、新上市和无成交标的要显式说明如何处理。

## 指标口径

- 简单收益：`P_t / P_(t-1) - 1`。
- 累计收益：`P_end / P_start - 1`。
- 年化波动率：日收益样本标准差 × `sqrt(periods_per_year)`；`periods_per_year` 必须报告。
- 最大回撤：价格相对运行峰值的最小跌幅。

这些是价格序列指标，不自动包含分红、交易成本、税费、滑点、汇率或再平衡。

## 缺口与丢弃必须报告

`metrics` 不做任何填充，缺失观测会被丢弃——这意味着跨缺口的那一期收益覆盖了整个缺口，不是持有期收益。`performance_summary` 返回 `dropped_observations`、`max_gap_days` 和 `warnings`，`alignment_report` 返回 `dropped_per_series`；这些非空时必须在结论中写出来，不能只报收益和波动率。

## 结果表达

报告共同样本的起止日期、每个标的有效观测数、对齐后样本数、复权/净值口径和币种。相关性或回归需要足够样本，并说明缺失处理与时间频率。
