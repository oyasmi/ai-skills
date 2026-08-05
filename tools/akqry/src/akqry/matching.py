"""Chinese-aware, rarity-weighted scoring for interface discovery.

AkShare names are pinyin-ish English while the vocabulary an analyst types is
Chinese, and the two rarely share wording: the docstring of ``stock_zh_a_hist``
says 每日行情 while a user asks for 历史行情. Whole-phrase substring matching
therefore misses the interfaces that matter most, so a term is expanded through a
small synonym table and, when nothing contains it verbatim, decomposed into
semantic two-character components (or, for a two-character word, into single
characters that must all appear).

That expansion is what makes recall possible and what would otherwise destroy
precision: 个股 expands to ``stock``, which appears in the name of 45% of the
catalog. Every hit is therefore weighted by how rare its form is in the candidate
set, so a ubiquitous form contributes almost nothing while 资金流向 or 龙虎
dominates. Rarity is measured over the records actually being searched, which
means ``stock`` is worth even less once ``--domain a-share`` is in play.

Two haystacks are kept per field. Chinese needles match a separator-free form so
``A股`` finds ``沪深京 A 股``; ASCII needles match a separator-preserving form at
word boundaries so ``fh`` (分红) finds ``fund_fh_em`` without also matching the
``etf``/``hist`` seam inside ``fund_etf_hist_em``.
"""

from __future__ import annotations

import math
import re
import unicodedata
from dataclasses import dataclass
from typing import Any, Sequence

from akqry.errors import AkqryError

_SEPARATORS = re.compile(r"[\s_\-–—/\\.,;:()\[\]{}<>|·、，。；：（）]+")
_CJK = re.compile(r"[㐀-䶿一-鿿぀-ヿ豈-﫿]")
_ASCII_BOUNDARY_CACHE: dict[str, re.Pattern[str]] = {}

# A name hit is worth far more than a hit anywhere in the prose, and an exact
# name match short-circuits everything else.
FIELD_WEIGHTS: tuple[tuple[str, int], ...] = (("name", 60), ("description", 30), ("text", 12))
EXACT_NAME_SCORE = 200.0
SYNONYM_FACTOR = 0.7
BIGRAM_FACTOR = 0.75
# Saying the query word twice is worth something, saying it ten times is not
# worth five times as much.
REPETITION_FACTOR = 0.3
# Weight of the supporting bigrams relative to the strongest one, so that a long
# phrase is not penalised for the junk bigrams that straddle two words.
BIGRAM_SUPPORT_FACTOR = 0.5
# Single characters are the weakest evidence there is, so a two-character word
# only scores when both of its characters land on the same record.
CHARACTER_FACTOR = 0.3
# Even a form present in every record keeps a trace of weight, so that a query
# made entirely of common words still ranks rather than collapsing to zero.
MIN_RARITY = 0.02

_SYNONYM_GROUPS: tuple[tuple[str, ...], ...] = (
    # 历史 and 日线 are kept apart: merged, the English ``history`` in
    # ``stock_history_dividend`` would answer a query about daily bars.
    ("历史", "走势", "hist", "history"),
    ("每日", "日线", "日频", "k线", "daily"),
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
    ("资金流向", "主力资金", "fund flow"),
    ("成交量", "volume"),
    ("市值", "market cap", "market capitalization"),
    ("市盈率", "pe", "p/e"),
    ("市净率", "pb", "p/b"),
    ("净利润", "net profit"),
    ("涨停", "limit up"),
    ("停牌", "停复牌", "suspend"),
    ("融资融券", "margin"),
    ("债券", "bond"),
    ("期货", "futures", "future"),
    ("期权", "option", "options"),
    ("外汇", "汇率", "currency", "forex", "fx"),
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
    query = unicodedata.normalize("NFKC", query)
    # Analysts commonly type ``A 股`` while the useful search token is ``A股``.
    # Only join the well-known market abbreviation; joining every CJK/ASCII edge
    # would turn ``ETF 历史行情`` into one meaningless term.
    query = re.sub(r"\b[aA]\s+股\b", "A股", query)
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
        pattern = re.compile(rf"(?<![a-z0-9]){re.escape(needle)}(?![a-z0-9])")
        _ASCII_BOUNDARY_CACHE[needle] = pattern
    return pattern


def occurrences(form: str, field: tuple[str, str]) -> int:
    """How many times a form appears in a field, 0 when it does not."""
    compact_text, spaced_text = field
    if has_cjk(form):
        needle = compact(form)
        return compact_text.count(needle) if needle else 0
    needle = spaced(form)
    return len(_ascii_pattern(needle).findall(spaced_text)) if needle else 0


def repetition_bonus(count: int) -> float:
    """Damped credit for a word the record says more than once.

    ``龙虎榜单-龙虎榜详情`` is a better answer to 龙虎榜 than an interface that
    mentions it in passing, and among interfaces that all contain the query
    verbatim this is the only signal available.
    """
    return 1.0 + REPETITION_FACTOR * math.log(count) if count > 1 else 1.0


Contribution = dict[int, tuple[float, str]]


@dataclass(frozen=True)
class Match:
    """One record's standing against a query."""

    index: int
    matched_terms: int
    # Share of the query's specificity that this record accounts for. Ranking on
    # it rather than on a raw term count keeps a record that matched only the
    # rare word ahead of one that matched only the ubiquitous word.
    coverage: float
    score: float
    reasons: list[dict[str, Any]]


def _merge_best(target: Contribution, source: Contribution) -> None:
    for index, candidate in source.items():
        if candidate[0] > target.get(index, (0.0, ""))[0]:
            target[index] = candidate


def _aggregate_any(parts: list[Contribution], factor: float, tag: str) -> Contribution:
    """Score a record on whichever parts hit it, led by the strongest one."""
    gathered: dict[int, list[tuple[float, str]]] = {}
    for part in parts:
        for index, item in part.items():
            gathered.setdefault(index, []).append(item)
    combined: Contribution = {}
    for index, items in gathered.items():
        items.sort(key=lambda item: -item[0])
        support = sum(score for score, _ in items[1:]) / (len(items) - 1) if len(items) > 1 else 0.0
        combined[index] = (factor * (items[0][0] + BIGRAM_SUPPORT_FACTOR * support), f"{items[0][1]}:{tag}")
    return combined


def _aggregate_all(parts: list[Contribution], factor: float, tag: str) -> Contribution:
    """Score only the records every part hits, at the strength of the weakest one."""
    if not parts:
        return {}
    shared = set(parts[0])
    for part in parts[1:]:
        shared &= set(part)
    combined: Contribution = {}
    for index in shared:
        weakest = min(part[index] for part in parts)
        combined[index] = (factor * weakest[0], f"{weakest[1]}:{tag}")
    return combined


class Corpus:
    """Scores query terms against a fixed candidate set, discounting common forms."""

    def __init__(self, fields: Sequence[dict[str, tuple[str, str]]]) -> None:
        self._fields = list(fields)
        self._exact = {field["name"][0]: index for index, field in enumerate(self._fields)}
        self._hits: dict[str, Contribution] = {}
        self._ceiling = math.log(len(self._fields) + 1) or 1.0

    def _form_hits(self, form: str) -> Contribution:
        """Records containing this form, each with its best field weight."""
        cached = self._hits.get(form)
        if cached is not None:
            return cached
        hits: Contribution = {}
        if form:
            for index, field_map in enumerate(self._fields):
                for field, weight in FIELD_WEIGHTS:
                    count = occurrences(form, field_map[field])
                    if count:
                        hits[index] = (weight * repetition_bonus(count), field)
                        break
        self._hits[form] = hits
        return hits

    def rarity(self, form: str) -> float:
        """Inverse document frequency, normalised so a form seen once is worth ~1."""
        frequency = len(self._form_hits(form))
        if not frequency:
            return 0.0
        return max(math.log((len(self._fields) + 1) / (frequency + 1)) / self._ceiling, MIN_RARITY)

    def _direct(self, term: str) -> Contribution:
        scores: Contribution = {}
        for form, factor in _forms(term):
            weight = factor * self.rarity(form)
            if weight <= 0:
                continue
            scaled = {
                index: (
                    score * weight,
                    field if factor == 1.0 else f"{field}:synonym",
                )
                for index, (score, field) in self._form_hits(form).items()
            }
            _merge_best(scores, scaled)
        return scores

    def _parts(self, term: str) -> list[str]:
        """The semantic two-character pieces of a compound Chinese term."""
        if len(term) >= 3:
            # Overlapping bigrams let one accidental character pair satisfy a
            # long phrase. Non-overlapping pieces preserve recall while
            # requiring all meaningful components, e.g. 历史行情 -> 历史 + 行情.
            parts: list[str] = []
            index = 0
            while index < len(term) - 1:
                pair = term[index : index + 2]
                if has_cjk(pair):
                    parts.append(pair)
                    index += 2
                else:
                    index += 1
            return parts
        # A two-character word has only itself as a bigram, so its characters are
        # the only decomposition left: 停 and 牌 both appear in 停复牌.
        return [character for character in term if has_cjk(character)] if len(term) == 2 else []

    def _partial(self, term: str) -> Contribution:
        """Weaker evidence for records that do not contain the term or a synonym."""
        parts = self._parts(term)
        if not parts:
            return {}
        if len(term) >= 3:
            return _aggregate_all([self._direct(part) for part in parts], BIGRAM_FACTOR, "bigram")
        if len(parts) < 2:
            return {}
        # One character in common is a coincidence, so both have to land.
        return _aggregate_all([self._direct(part) for part in parts], CHARACTER_FACTOR, "character")

    def term_weight(self, term: str, seen: set[str] | None = None) -> float:
        """How much of the query's specificity this term carries.

        Used to decide coverage: a record that matches only 停牌 answers more of
        ``A股 停牌`` than one that matches only 股, even though both cover a
        single term. The literal wording is trusted first, because the rarest
        synonym of a common word is not evidence that the word is specific.
        """
        seen = set() if seen is None else seen
        if term in seen:
            return 0.0
        seen.add(term)
        literal = self.rarity(term)
        if literal:
            return literal
        live: list[float] = []
        for form, _ in _forms(term)[1:]:
            rarity = self.rarity(form)
            if rarity:
                live.append(rarity)
        if live:
            return sum(live) / len(live)
        parts = self._parts(term)
        live = [self.term_weight(form, seen.copy()) for form in parts]
        live = [rarity for rarity in live if rarity]
        if live:
            return sum(live) / len(live)
        return 0.0

    def _term_scores(self, term: str) -> Contribution:
        scores = self._direct(term)
        # Partial evidence only speaks for records the term itself did not reach.
        scores.update({index: item for index, item in self._partial(term).items() if index not in scores})
        exact = self._exact.get(compact(term))
        if exact is not None:
            scores[exact] = (EXACT_NAME_SCORE, "name:exact")
        return scores

    @staticmethod
    def _evidence_strength(reason: str) -> float:
        if reason.endswith(":character"):
            return CHARACTER_FACTOR
        if reason.endswith(":bigram"):
            return BIGRAM_FACTOR
        if ":synonym" in reason:
            return SYNONYM_FACTOR
        return 1.0

    def rank(
        self,
        terms: Sequence[str],
        match_mode: str = "all",
        min_coverage: float = 0.0,
    ) -> tuple[list[Match], list[str]]:
        """Rank records while making partial query coverage explicit.

        ``all`` is the safe default for agent use: every term must have some
        evidence in a record. ``any`` is useful for exploratory discovery, but
        callers must inspect the per-result coverage before using a candidate.
        """
        if match_mode not in {"all", "any"}:
            raise AkqryError("usage_error", "match_mode must be 'all' or 'any'.")
        if not 0 <= min_coverage <= 1:
            raise AkqryError("usage_error", "min_coverage must be between 0 and 1.")

        reasons: dict[int, list[dict[str, Any]]] = {}
        totals: dict[int, float] = {}
        covered: dict[int, float] = {}
        matched_by_term: dict[int, set[str]] = {}
        unmatched: list[str] = []
        weights = {term: self.term_weight(term) for term in terms}
        total_weight = sum(weights.values()) or float(len(terms) or 1)
        for term in terms:
            scores = self._term_scores(term)
            if not scores:
                unmatched.append(term)
                continue
            weight = weights[term] or 1.0
            for index, (score, field) in scores.items():
                reasons.setdefault(index, []).append(
                    {"term": term, "matched_in": field, "score": round(score, 1)}
                )
                totals[index] = totals.get(index, 0.0) + score
                covered[index] = covered.get(index, 0.0) + weight * self._evidence_strength(field)
                matched_by_term.setdefault(index, set()).add(term)

        matches: list[Match] = []
        for index, items in reasons.items():
            matched_names = matched_by_term[index]
            coverage = min(1.0, covered[index] / total_weight)
            if match_mode == "all" and len(matched_names) != len(terms):
                continue
            if coverage < min_coverage:
                continue
            matches.append(Match(index, len(matched_names), round(coverage, 4), round(totals[index], 1), items))
        return matches, unmatched
