from __future__ import annotations

import pandas as pd
import pytest

from akqry.metrics import align_price_series, max_drawdown, performance_summary, simple_returns


def test_metrics_do_not_forward_fill() -> None:
    prices = pd.Series([100.0, None, 110.0], index=pd.to_datetime(["2025-01-01", "2025-01-02", "2025-01-03"]))
    assert simple_returns(prices).tolist() == [0.10000000000000009]


def test_max_drawdown() -> None:
    prices = pd.Series([100.0, 120.0, 90.0, 110.0])
    assert max_drawdown(prices) == pytest.approx(-0.25)


def test_align_prices_uses_intersection() -> None:
    first = pd.Series([1.0, 2.0, 3.0], index=pd.to_datetime(["2025-01-01", "2025-01-02", "2025-01-03"]))
    second = pd.Series([3.0, 4.0, 5.0], index=pd.to_datetime(["2025-01-02", "2025-01-03", "2025-01-04"]))
    aligned = align_price_series({"a": first, "b": second})
    assert list(aligned.index.strftime("%Y-%m-%d")) == ["2025-01-02", "2025-01-03"]
    short = pd.Series([1.0], index=pd.to_datetime(["2025-01-03"]))
    with pytest.raises(ValueError, match="At least two"):
        align_price_series({"a": first, "b": short})


def test_performance_summary() -> None:
    prices = pd.Series([100.0, 110.0, 121.0])
    summary = performance_summary(prices, periods_per_year=252)
    assert summary["cumulative_return"] == pytest.approx(0.21)
    assert summary["observations"] == 3
