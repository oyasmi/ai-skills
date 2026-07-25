"""Parse the highly regular AkShare docstring into structured metadata.

AkShare docstrings follow a stable shape that carries exactly what an agent needs
before calling an interface, so a pip-installed AkShare needs no documentation
checkout to be discoverable::

    东方财富网-行情首页-沪深京 A 股-每日行情
    https://quote.eastmoney.com/concept/sh603777.html?from=classic
    :param period: choice of {'daily', 'weekly', 'monthly'}
    :type period: str
    :return: 每日行情
    :rtype: pandas.DataFrame

The first line becomes the description (its leading segment is the data source),
``http`` lines become the source URL, and ``choice of {...}`` becomes a closed set
of accepted values that can be checked before spending a network call.
"""

from __future__ import annotations

import re
from typing import Any

_FIELD = re.compile(r"^:(param|type|return|rtype|raise|raises)\b\s*([^:]*?)\s*:\s*(.*)$")
_URL = re.compile(r"https?://\S+")
_BRACES = re.compile(r"\{(.*?)\}", re.DOTALL)
_MAPPING_KEY = re.compile(r"""['"]([^'"]*)['"]\s*:""")
_QUOTED = re.compile(r"""['"]([^'"]*)['"]""")

# ``choice of None or a positive float number`` and similar prose must not be read
# as a closed value set.
_OPEN_ENDED = ("choice of none", "positive float", "positive int", "any ")


def enum_values(text: str) -> list[str] | None:
    """Extract the accepted values of a ``choice of {...}`` parameter, if closed."""
    lowered = text.lower()
    if "choice of" not in lowered and "choice in" not in lowered:
        return None
    if any(marker in lowered for marker in _OPEN_ENDED):
        return None
    braces = _BRACES.search(text)
    if braces is None:
        return None
    body = braces.group(1)
    keys = _MAPPING_KEY.findall(body)
    if keys:
        return _unique(keys)
    quoted = _QUOTED.findall(body)
    if quoted:
        return _unique(quoted)
    parts = [part.strip() for part in body.split(",")]
    if parts and all(part and ":" not in part and len(part) <= 16 for part in parts):
        return _unique(parts)
    return None


def _unique(values: list[str]) -> list[str]:
    seen: dict[str, None] = {}
    for value in values:
        seen.setdefault(value.strip(), None)
    return list(seen)


def parse(docstring: str | None) -> dict[str, Any]:
    """Split a docstring into description, source URLs, parameter docs and return docs."""
    empty: dict[str, Any] = {
        "description": None,
        "description_extra": [],
        "source_site": None,
        "source_url": None,
        "source_urls": [],
        "parameter_docs": {},
        "returns": None,
        "returns_type": None,
    }
    if not docstring:
        return empty

    summary_lines: list[str] = []
    urls: list[str] = []
    fields: list[tuple[str, str, list[str]]] = []
    for raw in docstring.splitlines():
        line = raw.strip()
        if not line:
            continue
        match = _FIELD.match(line)
        if match:
            fields.append((match.group(1), match.group(2).strip(), [match.group(3).strip()]))
            continue
        found = _URL.findall(line)
        if found:
            urls.extend(found)
            remainder = _URL.sub("", line).strip(" \t-—,;:")
            if remainder:
                summary_lines.append(remainder)
            continue
        if fields:
            fields[-1][2].append(line)  # Continuation of the previous field.
        else:
            summary_lines.append(line)

    parameter_docs: dict[str, dict[str, Any]] = {}
    returns: str | None = None
    returns_type: str | None = None
    for kind, name, chunks in fields:
        text = " ".join(chunk for chunk in chunks if chunk).strip()
        if kind == "param" and name:
            entry = parameter_docs.setdefault(name, {"description": None, "type": None, "enum": None})
            entry["description"] = text or entry["description"]
            entry["enum"] = enum_values(text) or entry["enum"]
        elif kind == "type" and name:
            entry = parameter_docs.setdefault(name, {"description": None, "type": None, "enum": None})
            entry["type"] = text or entry["type"]
        elif kind == "return":
            returns = text or returns
        elif kind == "rtype":
            returns_type = text or returns_type

    description = summary_lines[0] if summary_lines else None
    site = None
    if description:
        head = re.split(r"[-—:：]", description, maxsplit=1)[0].strip()
        # A leading segment is only a data source when it is a short site name.
        if head and len(head) <= 12 and head != description:
            site = head
    return {
        "description": description,
        "description_extra": summary_lines[1:],
        "source_site": site,
        "source_url": urls[0] if urls else None,
        "source_urls": _unique(urls),
        "parameter_docs": parameter_docs,
        "returns": returns,
        "returns_type": returns_type,
    }
