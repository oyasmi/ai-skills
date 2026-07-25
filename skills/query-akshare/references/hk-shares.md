# 港股

## 高频链路

### 港股个股行情

1. 用 `stock_hk_spot_em` 确认五位代码、名称和当期快照。
2. 用 `stock_hk_hist` 获取日/周/月行情；显式指定日期、周期和复权。
3. 报告港币口径。除非显式获取并使用 FX 数据，不得与人民币资产的绝对价格或市值直接比较。

### 公司资料、财务与分红

- 公司与证券资料：`stock_hk_security_profile_em`、`stock_hk_company_profile_em`。
- 财务指标：`stock_hk_financial_indicator_em`；先确认报表期与币种。
- 分红：`stock_hk_dividend_payout_em`。只有把分红事件纳入计算，才能讨论总收益。

### 港股通

`stock_hk_ggt_components_em` 描述当前/指定接口返回的成份。不要将当前名单回填到历史研究。

## 常用接口

```text
stock_hk_spot_em
stock_hk_hist
stock_hk_hist_min_em
stock_hk_security_profile_em
stock_hk_company_profile_em
stock_hk_financial_indicator_em
stock_hk_dividend_payout_em
stock_hk_ggt_components_em
```

港股交易日不同于 A 股；横向时间序列比较默认取共同交易日，并报告 `alignment_report` 给出的丢弃数。多标的用 `--for-each symbol=...` 一次取完。

接口名与字段以 `akqry search --domain hk-share`、`akqry describe --probe` 的实际结果为准。
