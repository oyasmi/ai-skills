# 基金与 ETF

## ETF 链路

1. 用 `fund_etf_spot_em` 获取 ETF 当前行情和代码；明确它是交易所交易价格快照。
2. 用 `fund_etf_hist_em` 获取日/周/月历史行情，明确 `adjust`。
3. 以成交额、换手率、份额、折溢价等字段做筛选前，先确认单位和更新时间。
4. 需要分红或净值解释时，另外获取相应基金资料；交易价格、IOPV、基金净值不是同一指标。

常用接口：

```text
fund_etf_spot_em
fund_etf_hist_em
fund_etf_hist_min_em
fund_etf_dividend_sina
fund_etf_fund_daily_em
fund_etf_fund_info_em
```

不同来源的 ETF 代码格式可能不同，例如纯六位代码与 `sh510050`。按 `describe` 的参数说明传入，不要自行猜测交易所前缀。

## 公募基金链路

```text
基金名称/基础资料 → 净值或排名 → 历史持仓 → 行业配置/变动
```

```text
fund_open_fund_info_em
fund_open_fund_rank_em
fund_portfolio_hold_em
fund_portfolio_industry_allocation_em
fund_portfolio_change_em
```

基金持仓的 `date` 或报告期是披露期。必须在报告中标出，不得把它表述成实时持仓。

## 比较要求

- 基金份额类别（A/C、联接/ETF、人民币/外币）必须可比。
- 说明使用单位净值、累计净值或交易价格。
- 比较收益前统一复权和分红再投资口径；无法统一时只做描述性比较。
