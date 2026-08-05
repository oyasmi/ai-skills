"""Opt-in on-disk reuse of identical queries.

Agents iterate: the same daily bar series gets pulled again while a script is
being fixed, and upstream sites throttle. A cache hit must never be mistaken for
fresh data, so an entry keeps the timestamp of the original retrieval and the
served payload is always marked ``cache_hit``.
"""

from __future__ import annotations

import hashlib
import json
import math
import os
import shutil
import tempfile
import time
from pathlib import Path
from typing import Any

from akqry.errors import AkqryError

DEFAULT_TTL_SECONDS = 900.0


def _canonical(value: Any) -> Any:
    if isinstance(value, dict):
        return {str(key): _canonical(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [_canonical(item) for item in value]
    if isinstance(value, (set, frozenset)):
        values = [_canonical(item) for item in value]
        return sorted(values, key=lambda item: json.dumps(item, ensure_ascii=False, default=str, sort_keys=True))
    return value


def cache_key(material: dict[str, Any]) -> str:
    payload = json.dumps(
        _canonical(material),
        ensure_ascii=False,
        sort_keys=True,
        default=str,
        allow_nan=False,
        separators=(",", ":"),
    )
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def _entry_paths(root: Path, key: str) -> tuple[Path, Path]:
    directory = root / key[:2]
    return directory / f"{key}.json", directory / f"{key}.data"


def _file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def _temporary_path(path: Path, suffix: str) -> Path:
    descriptor, name = tempfile.mkstemp(prefix=f".{path.name}.", suffix=suffix, dir=path.parent)
    os.close(descriptor)
    return Path(name)


def _atomic_write(path: Path, payload: str) -> None:
    temporary = _temporary_path(path, ".tmp")
    try:
        with temporary.open("w", encoding="utf-8") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()


def load(root: Path, key: str, ttl: float, require_data: bool = False) -> dict[str, Any] | None:
    """Return a live cache entry, or None when absent, expired or unreadable."""
    entry_path, data_path = _entry_paths(root, key)
    if not entry_path.is_file():
        return None
    try:
        age = max(0.0, time.time() - entry_path.stat().st_mtime)
        if math.isnan(ttl) or (ttl >= 0 and age > ttl):
            return None
        entry = json.loads(entry_path.read_text(encoding="utf-8"))
    except (OSError, TypeError, ValueError, json.JSONDecodeError):
        return None
    if not isinstance(entry, dict):
        return None
    if "result" not in entry and "probe" not in entry:
        return None
    if not isinstance(entry.get("result", entry.get("probe", {})), dict):
        return None
    has_data = bool(entry.get("has_data"))
    if require_data and not has_data:
        return None
    if has_data and not data_path.is_file():
        return None
    expected_hash = entry.get("artifact_sha256")
    if has_data and expected_hash:
        try:
            if _file_sha256(data_path) != expected_hash:
                return None
        except OSError:
            return None
    if has_data and entry.get("artifact_size") is not None:
        try:
            if data_path.stat().st_size != int(entry["artifact_size"]):
                return None
        except (OSError, TypeError, ValueError):
            return None
    entry["age_seconds"] = round(age, 3)
    entry["data_path"] = str(data_path) if has_data else None
    return entry


def store(root: Path, key: str, entry: dict[str, Any], artifact: Path | None) -> None:
    """Persist an entry, tolerating a cache directory that cannot be written."""
    entry_path, data_path = _entry_paths(root, key)
    try:
        entry_path.parent.mkdir(parents=True, exist_ok=True)
        artifact_hash = None
        artifact_size = None
        if artifact is not None:
            artifact_hash = _file_sha256(artifact)
            artifact_size = artifact.stat().st_size
            temporary_data = _temporary_path(data_path, ".data.tmp")
            try:
                shutil.copyfile(artifact, temporary_data)
                os.replace(temporary_data, data_path)
            finally:
                if temporary_data.exists():
                    temporary_data.unlink()
        payload = {
            **entry,
            "has_data": artifact is not None,
            "artifact_sha256": artifact_hash,
            "artifact_size": artifact_size,
        }
        _atomic_write(
            entry_path,
            json.dumps(payload, ensure_ascii=False, default=str, allow_nan=False),
        )
    except (OSError, TypeError, ValueError):
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
