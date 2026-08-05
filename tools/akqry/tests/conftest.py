from __future__ import annotations

import sys
from pathlib import Path

import pytest

FAKE_MODULE = '''
from __future__ import annotations

import os
import time

import pandas as pd


def _log(name: str) -> None:
    path = os.environ.get("AKQRY_TEST_CALL_LOG")
    if path:
        with open(path, "a", encoding="utf-8") as handle:
            handle.write(name + "\\n")


def stock_demo(symbol: str = "000001", period: str = "daily", n: int = 3) -> pd.DataFrame:
    """测试数据源-A股-每日行情
    https://example.invalid/demo
    :param symbol: 股票代码
    :type symbol: str
    :param period: choice of {'daily', 'weekly'}
    :type period: str
    :param n: 返回行数
    :type n: int
    :return: 每日行情
    :rtype: pandas.DataFrame
    """
    _log(symbol)
    if symbol == "BOOM":
        raise ValueError("上游返回: 参数错误 code=40001")
    if symbol == "SLOW":
        time.sleep(30)
    if symbol == "EMPTY":
        return pd.DataFrame(columns=["日期", "代码", "收盘"])
    return pd.DataFrame(
        {
            "日期": [f"2025-01-0{index + 1}" for index in range(n)],
            "代码": [symbol] * n,
            "收盘": [10.0 + index for index in range(n)],
        }
    )


def set_demo_token(token: str = "") -> None:
    """A state setter that must never be reachable through fetch."""
    _log("token")


def fund_etf_spot_demo() -> pd.DataFrame:
    """测试数据源-ETF-实时行情
    https://example.invalid/etf
    :return: 实时行情
    :rtype: pandas.DataFrame
    """
    _log("etf")
    return pd.DataFrame({"代码": ["510050"], "最新价": [3.5]})


def typed_demo(n: int = 1, labels: list[int] | None = None, enabled: bool = False) -> pd.DataFrame:
    """A future-annotation fixture for parameter coercion tests."""
    values = labels if labels is not None else list(range(n))
    if not enabled:
        values = values[:n]
    return pd.DataFrame({"序号": values})


def quality_demo() -> pd.DataFrame:
    """A fixture containing duplicate dates and a non-finite price."""
    return pd.DataFrame({"日期": [20250101, 20250101], "代码": ["X", "X"], "收盘": [1.0, float("inf")]})
'''


@pytest.fixture
def fake_akshare(tmp_path: Path) -> str:
    """A minimal AkShare stand-in whose docstrings mimic the real ones."""
    package = tmp_path / "fake-akshare" / "akshare"
    package.mkdir(parents=True)
    (package / "data.py").write_text(FAKE_MODULE, encoding="utf-8")
    (package / "__init__.py").write_text(
        "__version__ = 'test'\nfrom .data import fund_etf_spot_demo, quality_demo, set_demo_token, stock_demo, typed_demo\n",
        encoding="utf-8",
    )
    return str(package.parent)


@pytest.fixture(autouse=True)
def clear_akshare_modules():
    """Keep a fake AkShare from leaking into the next test's import.

    ``load_akshare`` prepends the selected root to ``sys.path`` for the life of
    the process, so the path has to be restored too: otherwise a later test that
    imports the real AkShare would silently get the three-function stand-in.
    """
    original_path = list(sys.path)
    yield
    sys.path[:] = original_path
    for key in list(sys.modules):
        if key == "akshare" or key.startswith("akshare."):
            sys.modules.pop(key)
