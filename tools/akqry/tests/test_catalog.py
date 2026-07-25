from __future__ import annotations

import pytest

from akqry import docstrings
from akqry.catalog import discover, domain_for, search
from akqry.errors import AkqryError
from akqry.runtime import load_akshare

STOCK_HIST_DOC = """东方财富网-行情首页-沪深京 A 股-每日行情
https://quote.eastmoney.com/concept/sh603777.html?from=classic
:param symbol: 股票代码
:type symbol: str
:param period: choice of {'daily', 'weekly', 'monthly'}
:type period: str
:param adjust: choice of {"qfq": "前复权", "hfq": "后复权", "": "不复权"}
:type adjust: str
:param timeout: choice of None or a positive float number
:type timeout: float
:return: 每日行情
:rtype: pandas.DataFrame
"""


def test_docstring_yields_description_source_and_enums() -> None:
    parsed = docstrings.parse(STOCK_HIST_DOC)

    assert parsed["description"] == "东方财富网-行情首页-沪深京 A 股-每日行情"
    assert parsed["source_site"] == "东方财富网"
    assert parsed["source_url"].startswith("https://quote.eastmoney.com/")
    assert parsed["returns"] == "每日行情"
    assert parsed["parameter_docs"]["period"]["enum"] == ["daily", "weekly", "monthly"]
    # A mapping documents the accepted keys, including the no-adjustment default.
    assert parsed["parameter_docs"]["adjust"]["enum"] == ["qfq", "hfq", ""]
    # Prose describing an open-ended value must not be read as a closed set.
    assert parsed["parameter_docs"]["timeout"]["enum"] is None


def test_docstring_parsing_tolerates_missing_sections() -> None:
    assert docstrings.parse(None)["description"] is None
    parsed = docstrings.parse("只有一行说明")
    assert parsed["description"] == "只有一行说明"
    assert parsed["source_url"] is None and parsed["parameter_docs"] == {}


@pytest.mark.parametrize(
    ("name", "expected"),
    [
        ("stock_zh_a_hist", ["a-share"]),
        ("stock_hk_hist", ["hk-share"]),
        ("stock_board_industry_cons_em", ["board"]),
        ("stock_board_concept_name_em", ["board"]),
        ("fund_etf_hist_em", ["fund", "etf"]),
        ("index_zh_a_hist", ["index"]),
        ("stock_zh_index_daily", ["index"]),
        # An industry allocation of a fund's holdings is not a board interface.
        ("fund_portfolio_industry_allocation_em", ["fund"]),
    ],
)
def test_domains_match_name_segments_not_substrings(name: str, expected: list[str]) -> None:
    assert domain_for(name) == expected


@pytest.fixture(scope="module")
def akshare_catalog() -> dict[str, dict]:
    return discover(load_akshare(None))


@pytest.mark.parametrize(
    ("query", "domain", "expected"),
    [
        # The wording an analyst types ("历史行情") differs from AkShare's ("每日行情").
        ("历史行情", "a-share", "stock_zh_a_hist"),
        ("A股 历史行情", None, "stock_zh_a_hist"),
        ("股票 日线", None, "stock_zh_a_hist"),
        ("港股 行情", "hk-share", "stock_hk_hist"),
        ("ETF 历史行情", "etf", "fund_etf_hist_em"),
        ("ETF 实时行情", "etf", "fund_etf_spot_em"),
        ("行业板块 成份股", "board", "stock_board_industry_cons_em"),
        ("基金 持仓", "fund", "fund_portfolio_hold_em"),
        ("指数 历史行情", "index", "index_zh_a_hist"),
        ("港股通 成份", None, "stock_hk_ggt_components_em"),
    ],
)
def test_search_finds_the_canonical_interface(
    akshare_catalog: dict[str, dict], query: str, domain: str | None, expected: str
) -> None:
    names = [record["name"] for record in search(akshare_catalog, query, domain, 5)]
    assert expected in names, f"{expected} missing from {names}"


def test_search_results_carry_a_description_without_a_docs_checkout(akshare_catalog: dict[str, dict]) -> None:
    top = search(akshare_catalog, "ETF 历史行情", "etf", 1)[0]
    assert top["name"] == "fund_etf_hist_em"
    assert top["description"] and top["source_site"] and top["source_url"]
    assert top["match_reasons"] and top["matched_terms"] == 2


def test_search_rejects_an_unsearchable_query(akshare_catalog: dict[str, dict]) -> None:
    with pytest.raises(AkqryError):
        search(akshare_catalog, "  ", None, 5)


def test_describe_exposes_enums_for_parameter_validation(akshare_catalog: dict[str, dict]) -> None:
    record = akshare_catalog["stock_zh_a_hist"]
    enums = {item["name"]: item["enum"] for item in record["parameters"]}
    assert enums["period"] == ["daily", "weekly", "monthly"]
    assert enums["adjust"] == ["qfq", "hfq", ""]
