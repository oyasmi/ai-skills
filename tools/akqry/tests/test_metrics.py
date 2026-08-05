from __future__ import annotations

import pandas as pd
import pytest

from akqry.metrics import (
    align_price_series,
    alignment_report,
    gap_report,
    max_drawdown,
    performance_summary,
    simple_returns,
)


def _series(values: list[float | None], dates: list[str]) -> pd.Series:
    return pd.Series(values, index=pd.to_datetime(dates))


def test_metrics_do_not_forward_fill() -> None:
    prices = _series([100.0, None, 110.0], ["2025-01-01", "2025-01-02", "2025-01-03"])
    assert simple_returns(prices).tolist() == [pytest.approx(0.1)]


def test_max_drawdown() -> None:
    prices = pd.Series([100.0, 120.0, 90.0, 110.0])
    assert max_drawdown(prices) == pytest.approx(-0.25)


def test_align_prices_uses_intersection() -> None:
    first = _series([1.0, 2.0, 3.0], ["2025-01-01", "2025-01-02", "2025-01-03"])
    second = _series([3.0, 4.0, 5.0], ["2025-01-02", "2025-01-03", "2025-01-04"])
    aligned = align_price_series({"a": first, "b": second})
    assert list(aligned.index.strftime("%Y-%m-%d")) == ["2025-01-02", "2025-01-03"]
    short = _series([1.0], ["2025-01-03"])
    with pytest.raises(ValueError, match="At least two"):
        align_price_series({"a": first, "b": short})


def test_alignment_report_states_what_the_join_discarded() -> None:
    first = _series([1.0, 2.0, 3.0], ["2025-01-01", "2025-01-02", "2025-01-03"])
    second = _series([3.0, 4.0], ["2025-01-02", "2025-01-03"])
    report = alignment_report({"a": first, "b": second})
    assert report["aligned_observations"] == 2
    assert report["dropped_per_series"] == {"a": 1, "b": 0}
    assert report["warnings"] and "a lost 1" in report["warnings"][0]


def test_performance_summary() -> None:
    prices = _series([100.0, 110.0, 121.0], ["2025-01-01", "2025-01-02", "2025-01-03"])
    summary = performance_summary(prices, periods_per_year=252)
    assert summary["cumulative_return"] == pytest.approx(0.21)
    assert summary["observations"] == 3
    assert summary["calendar_days"] == pytest.approx(2.0)
    assert summary["warnings"] == []


def test_summary_flags_a_gap_instead_of_reporting_it_as_continuous() -> None:
    prices = _series(
        [100.0, 101.0, None, 60.0, 61.0],
        ["2025-01-02", "2025-01-03", "2025-03-03", "2025-06-02", "2025-06-03"],
    )
    summary = performance_summary(prices, periods_per_year=252)

    assert summary["dropped_observations"] == 1
    # 2025-01-03 to 2025-06-02, since the unusable observation between them is dropped.
    assert summary["max_gap_days"] == pytest.approx(150.0)
    assert len(summary["warnings"]) == 2
    assert "dropped as missing" in summary["warnings"][0]
    assert "150 calendar days" in summary["warnings"][1]


def test_gap_report_flags_a_non_datetime_index() -> None:
    report = gap_report(pd.Series([1.0, 2.0, 3.0]))
    assert report["max_gap_days"] is None
    assert "not datetime-like" in report["warnings"][0]


def test_metrics_reject_infinite_prices() -> None:
    with pytest.raises(ValueError, match="finite"):
        max_drawdown(pd.Series([100.0, float("inf")]))


def test_single_return_does_not_emit_nan_volatility() -> None:
    summary = performance_summary(pd.Series([100.0, 110.0]), periods_per_year=252)

    assert summary["annualized_volatility"] is None
    assert any("fewer than two returns" in warning for warning in summary["warnings"])


def test_alignment_report_counts_input_rows_dropped_before_join() -> None:
    first = _series([1.0, None, 3.0, 4.0], ["2025-01-01", "2025-01-02", "2025-01-03", "2025-01-04"])
    second = _series([3.0, 4.0, 5.0], ["2025-01-02", "2025-01-03", "2025-01-04"])

    report = alignment_report({"a": first, "b": second})

    assert report["dropped_per_series"] == {"a": 2, "b": 1}
