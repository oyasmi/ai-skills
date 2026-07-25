# A 股

## 高频链路

### 个股历史收益或走势

1. 用 `stock_zh_a_spot_em` 确认代码和当前名称；全市场表只作代码发现或当期筛选。
2. 用 `stock_zh_a_hist` 获取日/周/月行情。显式传 `period`、日期和 `adjust`（枚举见 `describe`：`daily/weekly/monthly` 与 `qfq/hfq/""`）。
3. 用 `--probe` 确认实际列名后再设 `--require-columns`；以返回的最后日期确认数据截止点。
4. 多标的用 `--for-each symbol=...` 分别落盘，再内连接日期，不按行号拼接。
5. 需要与沪深300 等基准对比时读取 [indices.md](indices.md)，不要用另一只股票代替基准。

### 基本资料与财务

- 个股基础资料：优先检查 `stock_individual_info_em`。
- 财务指标：先搜索/描述 `stock_financial_analysis_indicator_em` 或适合问题的数据源；报告报表期而不是把它称为实时指标。
- 任何市盈率、净利率或 ROE 比较都应说明指标期、TTM/报告期和负值处理。

### A/H 对照

- 先用 `stock_zh_ah_spot_em` 或相关名称接口确定对应证券。
- A 股与 H 股价格默认不同币种；没有明确汇率序列时，不报告“折价率”或直接比较绝对股价。

## 常用接口

```text
stock_zh_a_spot_em
stock_zh_a_hist
stock_zh_a_hist_min_em
stock_individual_info_em
stock_financial_analysis_indicator_em
stock_zh_ah_spot_em
```

调用前始终 `akqry describe`（必要时 `--probe`），因为参数和字段可能随 AkShare 版本改变。上面的接口名同样以 `akqry search --domain a-share` 的实际结果为准。
