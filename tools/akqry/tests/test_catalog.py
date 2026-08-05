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
        ("stock_margin_sse", ["margin"]),
        ("forex_hist_em", ["currency"]),
        ("futures_zh_spot", ["futures"]),
    ],
)
def test_domains_match_name_segments_not_substrings(name: str, expected: list[str]) -> None:
    assert domain_for(name) == expected


@pytest.fixture(scope="module")
def akshare_catalog() -> dict[str, dict]:
    return discover(load_akshare(None))


def _names(catalog: dict[str, dict], query: str, domain: str | None, limit: int = 5) -> list[str]:
    return [record["name"] for record in search(catalog, query, domain, max(limit, 20))["results"]]


@pytest.mark.parametrize(
    ("query", "domain", "expected"),
    [
        # The wording an analyst types ("历史行情") differs from AkShare's ("每日行情").
        ("历史行情", "a-share", {"stock_zh_a_hist", "stock_zh_a_daily"}),
        ("A股 历史行情", None, {"stock_zh_a_hist", "stock_zh_a_daily"}),
        # Either vendor's daily bars answers this; the point is that one of them wins.
        ("股票 日线", None, {"stock_zh_a_hist", "stock_zh_a_daily"}),
        ("港股 行情", "hk-share", {"stock_hk_hist"}),
        ("ETF 历史行情", "etf", {"fund_etf_hist_em"}),
        ("ETF 实时行情", "etf", {"fund_etf_spot_em"}),
        ("行业板块 成份股", "board", {"stock_board_industry_cons_em"}),
        ("基金 持仓", "fund", {"fund_portfolio_hold_em"}),
        ("指数 历史行情", "index", {"index_zh_a_hist"}),
        ("港股通 成份", None, {"stock_hk_ggt_components_em"}),
        ("融资融券", None, {"stock_margin_sse"}),
        ("资产负债表", None, {"stock_zcfz_em"}),
    ],
)
def test_search_finds_the_canonical_interface(
    akshare_catalog: dict[str, dict], query: str, domain: str | None, expected: set[str]
) -> None:
    names = _names(akshare_catalog, query, domain)
    assert expected.intersection(names), f"none of {sorted(expected)} in {names}"


def test_a_two_character_word_falls_back_to_its_characters(akshare_catalog: dict[str, dict]) -> None:
    # 停牌 appears nowhere verbatim; AkShare writes 停复牌, and a two-character
    # word is too short for the bigram fallback to say anything.
    names = _names(akshare_catalog, "停牌", None)
    assert "stock_tfp_em" in names
    assert "stock_tfp_em" not in _names(akshare_catalog, "A股 停牌", None)
    assert "stock_tfp_em" in [
        record["name"]
        for record in search(akshare_catalog, "A股 停牌", None, 20, match_mode="any")["results"]
    ]


def test_a_ubiquitous_synonym_does_not_outrank_the_specific_term(akshare_catalog: dict[str, dict]) -> None:
    # 个股 expands to `stock`, which is in the name of roughly half the catalog,
    # so it must not drown out 资金流向.
    names = _names(akshare_catalog, "个股 资金流向", None, 3)
    assert "stock_individual_fund_flow" in names, names


def test_repeating_the_query_word_breaks_a_tie_of_equals(akshare_catalog: dict[str, dict]) -> None:
    # Sixteen interfaces contain 龙虎榜; the one whose subject it is says it twice.
    assert _names(akshare_catalog, "龙虎榜", None)[0] == "stock_lhb_detail_em"


def test_search_reports_the_terms_it_could_not_match(akshare_catalog: dict[str, dict]) -> None:
    payload = search(akshare_catalog, "A股 阿斯顿发", None, 5)

    assert payload["unmatched_terms"] == ["阿斯顿发"]
    assert payload["results"] == [], "the safe default must not ignore an unmatched term"
    assert any("阿斯顿发" in hint for hint in payload["hints"])
    exploratory = search(akshare_catalog, "A股 阿斯顿发", None, 5, match_mode="any")
    assert exploratory["results"], "--match any should expose exploratory partial candidates"
    assert exploratory["total_matched"] > len(exploratory["results"])
    assert any("narrow" in hint for hint in exploratory["hints"])


def test_search_that_matches_nothing_says_what_to_try_next(akshare_catalog: dict[str, dict]) -> None:
    payload = search(akshare_catalog, "阿斯顿发", "etf", 5)

    assert payload["results"] == [] and payload["total_matched"] == 0
    assert payload["candidates"] > 0
    assert "--domain" in payload["hints"][0]


def test_search_results_carry_a_description_without_a_docs_checkout(akshare_catalog: dict[str, dict]) -> None:
    top = search(akshare_catalog, "ETF 历史行情", "etf", 1)["results"][0]
    assert top["name"] == "fund_etf_hist_em"
    assert top["description"] and top["source_site"] and top["source_url"]
    assert top["match_reasons"] and top["matched_terms"] == 2


def test_search_coverage_is_normalized_and_reports_per_result_gaps(akshare_catalog: dict[str, dict]) -> None:
    payload = search(akshare_catalog, "收盘 成交量", "a-share", 10)

    assert payload["unmatched_terms"] == []
    assert all(0 <= result["coverage"] <= 1 for result in payload["results"])
    assert all(result["unmatched_terms"] == [] for result in payload["results"])


def test_search_column_hints_keep_etf_history_separate_from_spot(akshare_catalog: dict[str, dict]) -> None:
    payload = search(akshare_catalog, "ETF 历史行情", "etf", 10)
    names = [result["name"] for result in payload["results"]]

    assert names and names[0] == "fund_etf_hist_em"
    assert "fund_etf_spot_em" not in names
    assert "fund_etf_fund_info_em" not in names


def test_search_rejects_an_unsearchable_query(akshare_catalog: dict[str, dict]) -> None:
    with pytest.raises(AkqryError):
        search(akshare_catalog, "  ", None, 5)


def test_describe_exposes_enums_for_parameter_validation(akshare_catalog: dict[str, dict]) -> None:
    record = akshare_catalog["stock_zh_a_hist"]
    enums = {item["name"]: item["enum"] for item in record["parameters"]}
    assert enums["period"] == ["daily", "weekly", "monthly"]
    assert enums["adjust"] == ["qfq", "hfq", ""]
