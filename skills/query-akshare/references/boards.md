# 行业与概念板块

## 先固定分类体系

东方财富和同花顺的行业、概念名称、覆盖范围和指数编制不同。一次分析只选一个体系；若需要比较，分别报告，不能直接合并为同一分类。

## 东方财富标准链路

```text
板块列表 → 确认板块名称/代码 → 当前板块或成份 → 板块历史行情
```

行业：

```text
stock_board_industry_name_em
stock_board_industry_spot_em
stock_board_industry_cons_em
stock_board_industry_hist_em
```

概念：

```text
stock_board_concept_name_em
stock_board_concept_spot_em
stock_board_concept_cons_em
stock_board_concept_hist_em
```

查询成份股前先从列表确认名称/代码；板块名歧义或改名时优先使用返回的板块代码。

## 当日板块强弱

行业/概念列表中的 `上涨家数`、`下跌家数` 可计算广度：

```text
上涨占比 = 上涨家数 / (上涨家数 + 下跌家数)
```

先处理分母为零、停牌和未知值。广度是当前快照，不是历史回测信号。

## 历史研究限制

当前成份股会产生幸存者偏差。历史板块指数可用于板块表现，但不能据当前成份解释历史贡献，除非另有点时成份数据。
