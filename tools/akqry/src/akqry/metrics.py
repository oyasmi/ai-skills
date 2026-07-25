"""Small, explicit price-series calculations for analysis scripts.

Nothing here fills a missing observation, so a suspended stock or a partially
downloaded series leaves a hole in the index. A return computed across such a
hole is not a holding-period return, so every summary reports how many
observations were dropped and how wide the largest gap is instead of quietly
presenting the number as continuous.
"""

from __future__ import annotations

import math
from typing import Any

import pandas as pd

# A calendar gap wider than this is longer than any regular market break
# (weekends and the longest mainland holiday), so it is worth a warning.
GAP_WARNING_DAYS = 12


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


def _index_gaps(series: pd.Series) -> tuple[float | None, float | None]:
    """Largest and total calendar-day span of the index, when it is datetime-like."""
    if not pd.api.types.is_datetime64_any_dtype(series.index):
        return None, None
    stamps = pd.DatetimeIndex(series.index)
    spans = stamps.to_series().diff().dropna()
    largest = float(spans.max().total_seconds() / 86400) if len(spans) else 0.0
    total = float((stamps[-1] - stamps[0]).total_seconds() / 86400)
    return largest, total


def simple_returns(prices: pd.Series) -> pd.Series:
    """Period-to-period simple returns.

    Invalid observations are dropped rather than filled, so a return spanning a
    gap covers the whole gap. Use :func:`performance_summary` (or
    :func:`gap_report`) to see whether that happened.
    """
    return _validated_prices(prices).pct_change(fill_method=None).dropna()


def max_drawdown(prices: pd.Series) -> float:
    series = _validated_prices(prices)
    drawdown = series / series.cummax() - 1.0
    return float(drawdown.min())


def gap_report(prices: pd.Series, gap_warning_days: int = GAP_WARNING_DAYS) -> dict[str, Any]:
    """Report how much of the input survived validation and how continuous it is."""
    series = _validated_prices(prices)
    dropped = int(len(prices) - len(series))
    max_gap, calendar_days = _index_gaps(series)
    warnings: list[str] = []
    if dropped:
        warnings.append(
            f"{dropped} observation(s) were dropped as missing or non-numeric; "
            "returns are computed across those holes rather than filled."
        )
    if max_gap is None:
        warnings.append("The index is not datetime-like, so calendar gaps could not be checked.")
    elif max_gap > gap_warning_days:
        warnings.append(
            f"The widest gap between consecutive observations is {max_gap:.0f} calendar days "
            f"(threshold {gap_warning_days}); a return spanning it is not a continuous holding return."
        )
    return {
        "observations": int(len(series)),
        "dropped_observations": dropped,
        "max_gap_days": max_gap,
        "calendar_days": calendar_days,
        "gap_warning_days": gap_warning_days,
        "warnings": warnings,
    }


def align_price_series(series: dict[str, pd.Series]) -> pd.DataFrame:
    """Inner-join price series on actual common timestamps; never fill missing values."""
    if not series:
        raise ValueError("At least one named price series is required.")
    prepared = {name: _validated_prices(value).rename(name) for name, value in series.items()}
    aligned = pd.concat(prepared.values(), axis=1, join="inner").dropna()
    if len(aligned) < 2:
        raise ValueError("Fewer than two common observations remain after alignment.")
    return aligned


def alignment_report(series: dict[str, pd.Series]) -> dict[str, Any]:
    """Report what an inner join discarded, so a comparison can state its sample."""
    prepared = {name: _validated_prices(value) for name, value in series.items()}
    aligned = align_price_series(series)
    dropped = {name: int(len(value) - len(aligned)) for name, value in prepared.items()}
    return {
        "names": list(prepared),
        "per_series_observations": {name: int(len(value)) for name, value in prepared.items()},
        "aligned_observations": int(len(aligned)),
        "dropped_per_series": dropped,
        "start": str(aligned.index[0]),
        "end": str(aligned.index[-1]),
        "warnings": [
            f"{name} lost {count} observation(s) to the common-date intersection."
            for name, count in dropped.items()
            if count
        ],
    }


def performance_summary(prices: pd.Series, periods_per_year: int) -> dict[str, Any]:
    """Summarise price return, annualised volatility and drawdown under explicit assumptions."""
    if periods_per_year <= 0:
        raise ValueError("periods_per_year must be positive.")
    series = _validated_prices(prices)
    returns = simple_returns(series)
    diagnostics = gap_report(prices)
    return {
        "observations": int(len(series)),
        "return_observations": int(len(returns)),
        "dropped_observations": diagnostics["dropped_observations"],
        "start": str(series.index[0]),
        "end": str(series.index[-1]),
        "calendar_days": diagnostics["calendar_days"],
        "max_gap_days": diagnostics["max_gap_days"],
        "start_price": float(series.iloc[0]),
        "end_price": float(series.iloc[-1]),
        "cumulative_return": float(series.iloc[-1] / series.iloc[0] - 1.0),
        "annualized_volatility": float(returns.std(ddof=1) * math.sqrt(periods_per_year)),
        "max_drawdown": max_drawdown(series),
        "periods_per_year": periods_per_year,
        "warnings": diagnostics["warnings"],
    }
