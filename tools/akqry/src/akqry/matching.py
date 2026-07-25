"""Chinese-aware scoring for interface discovery.

AkShare names are pinyin-ish English while the vocabulary an analyst types is
Chinese, and the two rarely share wording: the docstring of ``stock_zh_a_hist``
says 每日行情 while a user asks for 历史行情. Whole-phrase substring matching
therefore misses the interfaces that matter most, so this module decomposes
Chinese terms into character bigrams, expands both through a small synonym table,
and ranks by how many of the query's terms a record covers before it ranks by
weight.

Two haystacks are kept per field. Chinese needles match a separator-free form so
``A股`` finds ``沪深京 A 股``; ASCII needles match a separator-preserving form at
word boundaries so ``fh`` (分红) finds ``fund_fh_em`` without also matching the
``etf``/``hist`` seam inside ``fund_etf_hist_em``.
"""

from __future__ import annotations

import re
from typing import Any, Iterable

_SEPARATORS = re.compile(r"[\s_\-–—/\\.,;:()\[\]{}<>|·、，。；：（）]+")
_CJK = re.compile(r"[㐀-䶿一-鿿぀-ヿ豈-﫿]")
_ASCII_BOUNDARY_CACHE: dict[str, re.Pattern[str]] = {}

# A name hit is worth far more than a hit anywhere in the prose, and an exact
# name match short-circuits everything else.
FIELD_WEIGHTS: tuple[tuple[str, int], ...] = (("name", 60), ("description", 30), ("text", 12))
EXACT_NAME_SCORE = 200
SYNONYM_FACTOR = 0.7
BIGRAM_FACTOR = 0.6
# Weight of the supporting bigrams relative to the strongest one, so that a long
# phrase is not penalised for the junk bigrams that straddle two words.
BIGRAM_SUPPORT_FACTOR = 0.5

_SYNONYM_GROUPS: tuple[tuple[str, ...], ...] = (
    ("历史", "每日", "日线", "日频", "k线", "走势", "hist", "daily", "history"),
    ("行情", "报价", "价格", "quote", "price"),
    ("实时", "最新", "当前", "快照", "spot", "realtime"),
    ("股票", "个股", "stock", "证券"),
    ("a股", "沪深", "沪深京", "zh a"),
    ("港股", "香港", "hk"),
    ("美股", "us"),
    ("基金", "fund"),
    ("净值", "nav"),
    ("etf", "交易型开放式", "场内基金"),
    ("板块", "行业", "industry", "sector", "board"),
    ("概念", "concept"),
    ("成份", "成分", "成份股", "成分股", "cons", "constituent"),
    ("指数", "index"),
    ("分钟", "分时", "min", "minute"),
    ("财务", "财报", "报表", "financial"),
    ("分红", "股息", "派息", "dividend", "fh"),
    ("持仓", "持股", "重仓", "portfolio", "hold"),
    ("规模", "份额", "scale"),
    ("排名", "排行", "rank"),
    ("资料", "简介", "信息", "档案", "profile", "info"),
    ("成交", "换手", "volume", "turnover"),
    ("涨跌", "涨幅", "跌幅"),
    ("估值", "市盈率", "市净率", "valuation"),
    ("港股通", "沪深港通", "ggt", "hsgt"),
    ("复权", "qfq", "hfq", "adjust"),
)

SYNONYMS: dict[str, tuple[str, ...]] = {}
for _group in _SYNONYM_GROUPS:
    for _term in _group:
        SYNONYMS[_term] = SYNONYMS.get(_term, ()) + tuple(item for item in _group if item != _term)


def has_cjk(text: str) -> bool:
    return bool(_CJK.search(text))


def compact(text: str) -> str:
    """Lowercase and strip separators so ``沪深京 A 股`` matches a query for ``A股``."""
    return _SEPARATORS.sub("", text.lower())


def spaced(text: str) -> str:
    """Lowercase and collapse separators to single spaces, preserving word edges."""
    return _SEPARATORS.sub(" ", text.lower()).strip()


def field_text(*parts: Any) -> tuple[str, str]:
    joined = " ".join(str(part) for part in parts if part)
    return compact(joined), spaced(joined)


def query_terms(query: str) -> list[str]:
    """Split a query into terms, dropping noise such as a lone ASCII character."""
    terms: list[str] = []
    for chunk in _SEPARATORS.split(query.lower()):
        term = chunk.strip()
        if not term or term in terms:
            continue
        if len(term) == 1 and not has_cjk(term):
            continue
        terms.append(term)
    return terms


def bigrams(term: str) -> list[str]:
    """Character bigrams of a Chinese term, skipping purely ASCII/digit pairs."""
    grams = [term[index : index + 2] for index in range(len(term) - 1)]
    return [gram for gram in grams if has_cjk(gram)]


def _forms(term: str) -> list[tuple[str, float]]:
    return [(term, 1.0)] + [(synonym, SYNONYM_FACTOR) for synonym in SYNONYMS.get(term, ())]


def _ascii_pattern(needle: str) -> re.Pattern[str]:
    pattern = _ASCII_BOUNDARY_CACHE.get(needle)
    if pattern is None:
        pattern = re.compile(rf"(?<![a-z0-9]){re.escape(needle)}")
        _ASCII_BOUNDARY_CACHE[needle] = pattern
    return pattern


def _hit(form: str, field: tuple[str, str]) -> bool:
    compact_text, spaced_text = field
    if has_cjk(form):
        needle = compact(form)
        return bool(needle) and needle in compact_text
    needle = spaced(form)
    return bool(needle) and _ascii_pattern(needle).search(spaced_text) is not None


def _direct_score(term: str, fields: dict[str, tuple[str, str]]) -> tuple[float, str | None]:
    best, field_hit = 0.0, None
    for form, factor in _forms(term):
        if not form:
            continue
        for field, weight in FIELD_WEIGHTS:
            if weight * factor > best and _hit(form, fields[field]):
                best, field_hit = weight * factor, field
    return best, field_hit


def term_score(term: str, fields: dict[str, tuple[str, str]]) -> tuple[float, str | None]:
    """Score one query term, falling back to bigrams when the whole phrase misses."""
    needle = compact(term)
    if needle and needle == fields["name"][0]:
        return float(EXACT_NAME_SCORE), "name"
    direct, field_hit = _direct_score(term, fields)
    if direct:
        return direct, field_hit
    if not has_cjk(term) or len(term) < 3:
        return 0.0, None
    grams = bigrams(term)
    if not grams:
        return 0.0, None
    scores: list[tuple[float, str | None]] = [_direct_score(gram, fields) for gram in grams]
    hits = sorted((item for item in scores if item[0] > 0), key=lambda item: -item[0])
    if not hits:
        return 0.0, None
    support = sum(score for score, _ in hits[1:]) / (len(hits) - 1) if len(hits) > 1 else 0.0
    return BIGRAM_FACTOR * (hits[0][0] + BIGRAM_SUPPORT_FACTOR * support), f"{hits[0][1]}:bigram"


def score_record(
    terms: Iterable[str], fields: dict[str, tuple[str, str]]
) -> tuple[int, float, list[dict[str, Any]]]:
    """Return matched-term count, total score and per-term match reasons."""
    matched, total = 0, 0.0
    reasons: list[dict[str, Any]] = []
    for term in terms:
        score, field = term_score(term, fields)
        if score <= 0:
            continue
        matched += 1
        total += score
        reasons.append({"term": term, "matched_in": field, "score": round(score, 1)})
    return matched, round(total, 1), reasons
