from __future__ import annotations

from pathlib import Path

from akqry import cache


def test_cache_key_is_stable_for_mapping_order_and_sets() -> None:
    first = {"parameters": {"symbols": {"600519", "000001"}, "n": 2}}
    second = {"parameters": {"n": 2, "symbols": {"000001", "600519"}}}

    assert cache.cache_key(first) == cache.cache_key(second)


def test_cache_rejects_a_corrupted_artifact(tmp_path: Path) -> None:
    artifact = tmp_path / "source.jsonl"
    artifact.write_text("{\"value\": 1}\n", encoding="utf-8")
    root = tmp_path / "cache"
    cache.store(root, "a" * 64, {"result": {"rows": 1}}, artifact)

    entry = cache.load(root, "a" * 64, ttl=900)
    assert entry is not None and entry["has_data"] is True
    Path(entry["data_path"]).write_text("corrupted\n", encoding="utf-8")

    assert cache.load(root, "a" * 64, ttl=900) is None


def test_preview_cache_entry_can_be_filtered_when_data_is_required(tmp_path: Path) -> None:
    key = "b" * 64
    root = tmp_path / "cache"
    cache.store(root, key, {"result": {"preview_only": True}}, None)

    assert cache.load(root, key, ttl=900) is not None
    assert cache.load(root, key, ttl=900, require_data=True) is None
