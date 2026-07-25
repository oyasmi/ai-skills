"""Opt-in on-disk reuse of identical queries.

Agents iterate: the same daily bar series gets pulled again while a script is
being fixed, and upstream sites throttle. A cache hit must never be mistaken for
fresh data, so an entry keeps the timestamp of the original retrieval and the
served payload is always marked ``cache_hit``.
"""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import time
from pathlib import Path
from typing import Any

from akqry.errors import AkqryError

DEFAULT_TTL_SECONDS = 900.0


def cache_key(material: dict[str, Any]) -> str:
    payload = json.dumps(material, ensure_ascii=False, sort_keys=True, default=str, separators=(",", ":"))
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def _entry_paths(root: Path, key: str) -> tuple[Path, Path]:
    directory = root / key[:2]
    return directory / f"{key}.json", directory / f"{key}.data"


def load(root: Path, key: str, ttl: float) -> dict[str, Any] | None:
    """Return a live cache entry, or None when absent, expired or unreadable."""
    entry_path, data_path = _entry_paths(root, key)
    if not entry_path.is_file():
        return None
    age = time.time() - entry_path.stat().st_mtime
    if ttl >= 0 and age > ttl:
        return None
    try:
        entry = json.loads(entry_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    if entry.get("has_data") and not data_path.is_file():
        return None
    entry["age_seconds"] = round(age, 3)
    entry["data_path"] = str(data_path) if entry.get("has_data") else None
    return entry


def store(root: Path, key: str, entry: dict[str, Any], artifact: Path | None) -> None:
    """Persist an entry, tolerating a cache directory that cannot be written."""
    entry_path, data_path = _entry_paths(root, key)
    try:
        entry_path.parent.mkdir(parents=True, exist_ok=True)
        if artifact is not None:
            shutil.copyfile(artifact, data_path)
        payload = {**entry, "has_data": artifact is not None}
        temporary = entry_path.with_suffix(".json.tmp")
        temporary.write_text(json.dumps(payload, ensure_ascii=False, default=str), encoding="utf-8")
        os.replace(temporary, entry_path)
    except OSError:
        # A cache is an optimisation; a broken cache must not fail a query.
        return


def resolve_root(value: str | None) -> Path | None:
    selected = value or os.environ.get("AKQRY_CACHE_DIR")
    if not selected:
        return None
    root = Path(selected).expanduser().resolve()
    try:
        root.mkdir(parents=True, exist_ok=True)
    except OSError as exc:
        raise AkqryError("write_failed", "Cache directory is not writable.", {"path": str(root)}) from exc
    return root
