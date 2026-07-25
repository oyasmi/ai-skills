# 时间序列分析

## 数据准备

先把每个标的独立落盘；读取时保留代码为字符串。将日期列显式转换为 datetime，排序并检查重复日期。不要按 DataFrame 行号把两个标的拼在一起。

```python
import pandas as pd
from akqry.metrics import align_price_series, performance_summary

first = pd.read_parquet("/tmp/first.parquet").set_index("日期")["收盘"]
second = pd.read_parquet("/tmp/second.parquet").set_index("日期")["收盘"]
prices = align_price_series({"first": first, "second": second})

summary = {
    name: performance_summary(prices[name], periods_per_year=252)
    for name in prices.columns
}
```

若使用 JSONL 或 CSV，按 sidecar 中的 schema 显式设置代码列 dtype，再转换日期。

## 指标口径

- 简单收益：`P_t / P_(t-1) - 1`。
- 累计收益：`P_end / P_start - 1`。
- 年化波动率：日收益样本标准差 × `sqrt(periods_per_year)`；`periods_per_year` 必须报告。
- 最大回撤：价格相对运行峰值的最小跌幅。

这些是价格序列指标，不自动包含分红、交易成本、税费、滑点、汇率或再平衡。

## 结果表达

报告共同样本的起止日期、每个标的有效观测数、对齐后样本数、复权/净值口径和币种。相关性或回归需要足够样本，并说明缺失处理与时间频率。
