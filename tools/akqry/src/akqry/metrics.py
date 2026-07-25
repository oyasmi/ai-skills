"""Small, explicit price-series calculations for analysis scripts."""

from __future__ import annotations

import math
from typing import Any

import pandas as pd


def _validated_prices(prices: pd.Series) -> pd.Series:
    series = pd.to_numeric(prices.copy(), errors="coerce").dropna()
    if len(series) < 2:
        raise ValueError("At least two valid prices are required.")
    if not series.index.is_monotonic_increasing:
        series = series.sort_index()
    if series.index.has_duplicates:
        raise ValueError("Price index contains duplicate timestamps.")
    if not series.gt(0).all():
        raise ValueError("Prices must be finite and strictly positive.")
    return series


def simple_returns(prices: pd.Series) -> pd.Series:
    """Return period-to-period simple returns without forward filling."""
    return _validated_prices(prices).pct_change(fill_method=None).dropna()


def max_drawdown(prices: pd.Series) -> float:
    series = _validated_prices(prices)
    drawdown = series / series.cummax() - 1.0
    return float(drawdown.min())


def align_price_series(series: dict[str, pd.Series]) -> pd.DataFrame:
    """Inner-join price series on actual common timestamps; never fill missing values."""
    if not series:
        raise ValueError("At least one named price series is required.")
    prepared = {name: _validated_prices(value).rename(name) for name, value in series.items()}
    aligned = pd.concat(prepared.values(), axis=1, join="inner").dropna()
    if len(aligned) < 2:
        raise ValueError("Fewer than two common observations remain after alignment.")
    return aligned


def performance_summary(prices: pd.Series, periods_per_year: int) -> dict[str, Any]:
    """Summarise price return, annualised volatility and drawdown under explicit assumptions."""
    if periods_per_year <= 0:
        raise ValueError("periods_per_year must be positive.")
    series = _validated_prices(prices)
    returns = simple_returns(series)
    return {
        "observations": int(len(series)),
        "start": str(series.index[0]),
        "end": str(series.index[-1]),
        "start_price": float(series.iloc[0]),
        "end_price": float(series.iloc[-1]),
        "cumulative_return": float(series.iloc[-1] / series.iloc[0] - 1.0),
        "annualized_volatility": float(returns.std(ddof=1) * math.sqrt(periods_per_year)),
        "max_drawdown": max_drawdown(series),
        "periods_per_year": periods_per_year,
    }
