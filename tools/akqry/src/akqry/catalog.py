"""Runtime-first interface discovery with optional local documentation enrichment."""

from __future__ import annotations

import inspect
import re
from pathlib import Path
from typing import Any

from akqry.errors import AkqryError
from akqry.runtime import is_safe_callable

DOMAIN_RULES = {
    "a-share": ("stock_zh_a", "stock_individual", "stock_financial", "stock_zh_ah"),
    "hk-share": ("stock_hk", "stock_zh_ah"),
    "board": ("stock_board", "industry", "concept"),
    "fund": ("fund_",),
    "etf": ("etf",),
}


def _normalise(value: str) -> str:
    return re.sub(r"[\s_\-]+", " ", value.lower()).strip()


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
                "description": description.group(1).strip() if description else None,
                "source_url": url.group(1) if url else None,
                "document_path": str(path),
                "document_columns": [column.strip() for column in columns if column.strip() not in {"名称", "名称 "}],
            }
    return records


def domain_for(name: str) -> list[str]:
    lowered = name.lower()
    return [domain for domain, fragments in DOMAIN_RULES.items() if any(item in lowered for item in fragments)]


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
            signature = str(inspect.signature(value))
        except (TypeError, ValueError):
            signature = "(...)"
        doc = inspect.getdoc(value) or ""
        try:
            source = inspect.getsourcefile(value)
        except TypeError:
            source = None
        catalog[name] = {
            "name": name,
            "signature": signature,
            "docstring": doc,
            "module": getattr(value, "__module__", None),
            "source_file": source,
            "domains": domain_for(name),
            "safe_to_fetch": safe,
            "excluded_reason": excluded_reason,
            **docs.get(name, {}),
        }
    return catalog


def search(catalog: dict[str, dict[str, Any]], query: str, domain: str | None, limit: int) -> list[dict[str, Any]]:
    terms = [term for term in _normalise(query).split() if term]
    if not terms:
        raise AkqryError("usage_error", "Search query must contain at least one non-space character.")
    results: list[tuple[int, dict[str, Any]]] = []
    for record in catalog.values():
        if domain and domain not in record["domains"]:
            continue
        name = _normalise(record["name"])
        title = _normalise(" ".join(str(record.get(key) or "") for key in ("heading", "description", "docstring")))
        score = 0
        for term in terms:
            if term == name:
                score += 100
            elif term in name:
                score += 40
            if term in title:
                score += 15
        if score:
            results.append((score, record))
    results.sort(key=lambda item: (-item[0], item[1]["name"]))
    return [{key: value for key, value in record.items() if key != "docstring"} | {"score": score} for score, record in results[:limit]]


def describe(catalog: dict[str, dict[str, Any]], name: str) -> dict[str, Any]:
    record = catalog.get(name)
    if record is None:
        raise AkqryError("function_not_found", "No public AkShare callable with that name was found.", {"function": name})
    return record
