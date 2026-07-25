"""Runtime-first interface discovery with optional local documentation enrichment."""

from __future__ import annotations

import inspect
import re
from pathlib import Path
from typing import Any

from akqry import docstrings, matching
from akqry.errors import AkqryError
from akqry.runtime import is_safe_callable

# Domains are matched on name segments rather than raw substrings so that, for
# example, ``fund_portfolio_industry_allocation_em`` stays a fund interface.
DOMAIN_RULES: dict[str, dict[str, tuple[str, ...]]] = {
    "a-share": {
        "prefixes": (
            "stock_zh_a",
            "stock_a_",
            "stock_individual",
            "stock_financial",
            "stock_zh_ah",
            "stock_profit",
            "stock_balance",
            "stock_cash",
        ),
    },
    "hk-share": {"prefixes": ("stock_hk", "stock_zh_ah")},
    "board": {
        "prefixes": ("stock_board",),
        "segments_with_prefix": ("industry", "concept", "sector"),
    },
    "fund": {"prefixes": ("fund_",)},
    "etf": {"segments": ("etf",)},
    "index": {
        "prefixes": ("index_", "stock_zh_index"),
        "segments": ("index",),
    },
}

# ``segments_with_prefix`` only applies to these name roots, keeping fund and
# futures interfaces out of the board domain.
_SEGMENT_ROOTS = ("stock_",)


def _documentation_root(module_path: str | None, supplied: str | None) -> Path | None:
    if supplied:
        root = Path(supplied).expanduser().resolve()
        return root if root.is_dir() else None
    if not module_path:
        return None
    package = Path(module_path).resolve().parent
    candidate = package.parent / "docs"
    return candidate if candidate.is_dir() else None


def load_documentation(module_path: str | None, supplied_root: str | None) -> dict[str, dict[str, Any]]:
    root = _documentation_root(module_path, supplied_root)
    if root is None:
        return {}
    records: dict[str, dict[str, Any]] = {}
    for path in root.glob("data/**/*.md"):
        text = path.read_text(encoding="utf-8", errors="replace")
        matches = list(re.finditer(r"^接口:\s*([A-Za-z_][A-Za-z0-9_]*)\s*$", text, re.MULTILINE))
        headings = list(re.finditer(r"^(#{2,6})\s+(.+)$", text, re.MULTILINE))
        for match in matches:
            preceding = [heading for heading in headings if heading.start() < match.start()]
            if preceding:
                current_heading = preceding[-1]
                level = len(current_heading.group(1))
                start = current_heading.start()
                following = [
                    heading
                    for heading in headings
                    if heading.start() > match.start() and len(heading.group(1)) <= level
                ]
                end = following[0].start() if following else len(text)
                heading = current_heading.group(2).strip()
            else:
                start, end, heading = match.start(), len(text), None
            block = text[start:end]
            description = re.search(r"^描述:\s*(.+)$", block, re.MULTILINE)
            url = re.search(r"^目标地址:\s*(\S+)", block, re.MULTILINE)
            output_match = re.search(r"^输出参数[^\n]*\n", block, re.MULTILINE)
            output_block = block[output_match.end() :] if output_match else ""
            columns = re.findall(
                r"^\|\s*([^|]+?)\s*\|\s*(?:object|float|int|str|datetime)[^|]*\|",
                output_block,
                re.MULTILINE,
            )
            records[match.group(1)] = {
                "heading": heading,
                "document_description": description.group(1).strip() if description else None,
                "document_url": url.group(1) if url else None,
                "document_path": str(path),
                "document_columns": [column.strip() for column in columns if column.strip() not in {"名称", "名称 "}],
            }
    return records


def domain_for(name: str) -> list[str]:
    lowered = name.lower()
    segments = set(lowered.split("_"))
    domains: list[str] = []
    for domain, rules in DOMAIN_RULES.items():
        if lowered.startswith(rules.get("prefixes", ())):
            domains.append(domain)
            continue
        if segments.intersection(rules.get("segments", ())):
            domains.append(domain)
            continue
        extra = rules.get("segments_with_prefix", ())
        if extra and lowered.startswith(_SEGMENT_ROOTS) and segments.intersection(extra):
            domains.append(domain)
    return domains


def _parameters(signature: inspect.Signature, parameter_docs: dict[str, dict[str, Any]]) -> list[dict[str, Any]]:
    entries: list[dict[str, Any]] = []
    for parameter in signature.parameters.values():
        documented = parameter_docs.get(parameter.name, {})
        entries.append(
            {
                "name": parameter.name,
                "required": parameter.default is inspect.Parameter.empty,
                "default": None if parameter.default is inspect.Parameter.empty else parameter.default,
                "annotation": (
                    None
                    if parameter.annotation is inspect.Parameter.empty
                    else getattr(parameter.annotation, "__name__", str(parameter.annotation))
                ),
                "documented_type": documented.get("type"),
                "description": documented.get("description"),
                "enum": documented.get("enum"),
            }
        )
    return entries


def _search_fields(record: dict[str, Any]) -> dict[str, tuple[str, str]]:
    parameter_text = " ".join(
        f"{item['name']} {item.get('description') or ''} {' '.join(item.get('enum') or [])}"
        for item in record.get("parameters", [])
    )
    return {
        "name": matching.field_text(record["name"]),
        "description": matching.field_text(
            record.get("description"),
            record.get("heading"),
            record.get("document_description"),
        ),
        "text": matching.field_text(
            " ".join(record.get("description_extra") or []),
            record.get("returns"),
            parameter_text,
            record.get("module"),
            " ".join(record.get("document_columns") or []),
            record.get("source_url"),
        ),
    }


def discover(module: object, docs_root: str | None = None, include_unsafe: bool = False) -> dict[str, dict[str, Any]]:
    module_path = getattr(module, "__file__", None)
    docs = load_documentation(module_path, docs_root)
    catalog: dict[str, dict[str, Any]] = {}
    for name, value in inspect.getmembers(module):
        safe, excluded_reason = is_safe_callable(name, value)
        if not safe and not include_unsafe:
            continue
        if not callable(value) or name.startswith("_"):
            continue
        try:
            signature: inspect.Signature | None = inspect.signature(value)
        except (TypeError, ValueError):
            signature = None
        doc = inspect.getdoc(value) or ""
        parsed = docstrings.parse(doc)
        try:
            source = inspect.getsourcefile(value)
        except TypeError:
            source = None
        documented = docs.get(name, {})
        record: dict[str, Any] = {
            "name": name,
            "signature": str(signature) if signature else "(...)",
            "description": parsed["description"] or documented.get("document_description"),
            "description_extra": parsed["description_extra"],
            "source_site": parsed["source_site"],
            "source_url": parsed["source_url"] or documented.get("document_url"),
            "source_urls": parsed["source_urls"],
            "returns": parsed["returns"],
            "returns_type": parsed["returns_type"],
            "parameters": _parameters(signature, parsed["parameter_docs"]) if signature else [],
            "docstring": doc,
            "module": getattr(value, "__module__", None),
            "source_file": source,
            "domains": domain_for(name),
            "safe_to_fetch": safe,
            "excluded_reason": excluded_reason,
            **documented,
        }
        record["search_fields"] = _search_fields(record)
        catalog[name] = record
    return catalog


SUMMARY_FIELDS = ("name", "signature", "description", "source_site", "source_url", "domains", "safe_to_fetch")
INTERNAL_FIELDS = ("search_fields",)


def _summary(record: dict[str, Any]) -> dict[str, Any]:
    summary = {key: record.get(key) for key in SUMMARY_FIELDS}
    summary["required_parameters"] = [item["name"] for item in record.get("parameters", []) if item["required"]]
    return summary


def _search_hints(
    domain: str | None, terms: list[str], unmatched: list[str], total: int, limit: int
) -> list[str]:
    hints: list[str] = []
    if not total:
        hints.append(
            "Nothing matched. Try fewer or broader terms"
            + (", or drop --domain to search the whole catalog." if domain else ".")
        )
    elif unmatched:
        hints.append(
            "These terms matched no interface at all, so the results do not cover them: "
            + ", ".join(unmatched)
            + ". Treat the results as answering only "
            + ", ".join(term for term in terms if term not in unmatched)
            + "."
        )
    if total > limit:
        hints.append(f"{total} interfaces matched and {limit} are shown; add a term or --domain to narrow.")
    return hints


def search(
    catalog: dict[str, dict[str, Any]],
    query: str,
    domain: str | None,
    limit: int,
    full: bool = False,
) -> dict[str, Any]:
    """Rank interfaces for a query, reporting what the ranking does not cover."""
    terms = matching.query_terms(query)
    if not terms:
        raise AkqryError("usage_error", "Search query must contain at least one searchable term.")
    candidates = [record for record in catalog.values() if not domain or domain in record["domains"]]
    # Rarity is measured over the candidates, so ``stock`` counts for even less
    # once a domain filter has already selected for it.
    corpus = matching.Corpus([record["search_fields"] for record in candidates])
    ranked, unmatched = corpus.rank(terms)
    # Account for as much of the query's specificity as possible first, then
    # weight, then prefer the shorter (usually more general) interface name.
    ranked.sort(key=lambda item: (-item.coverage, -item.score, len(candidates[item.index]["name"])))
    results: list[dict[str, Any]] = []
    for item in ranked[:limit]:
        record = candidates[item.index]
        payload = describe(catalog, record["name"]) if full else _summary(record)
        results.append(
            {
                **payload,
                "score": item.score,
                "matched_terms": item.matched_terms,
                "coverage": item.coverage,
                "match_reasons": item.reasons,
            }
        )
    return {
        "query": query,
        "domain": domain,
        "terms": terms,
        "unmatched_terms": unmatched,
        "candidates": len(candidates),
        "total_matched": len(ranked),
        "results": results,
        "hints": _search_hints(domain, terms, unmatched, len(ranked), limit),
    }


def describe(catalog: dict[str, dict[str, Any]], name: str) -> dict[str, Any]:
    record = catalog.get(name)
    if record is None:
        raise AkqryError(
            "function_not_found",
            "No public AkShare callable with that name was found.",
            {"function": name, "hint": "Use `akqry search` to find the current interface name."},
        )
    return {key: value for key, value in record.items() if key not in INTERNAL_FIELDS}
