from __future__ import annotations

import json
from pathlib import Path

import pytest

from akqry import serializers
from akqry.cli import _handle, build_parser
from akqry.errors import AkqryError


def _run(argv: list[str]) -> dict:
    """Go through the real parser so a new flag can never bypass a test."""
    return _handle(build_parser().parse_args(argv))


def _fetch(root: str, *extra: str) -> list[str]:
    return ["fetch", "stock_demo", "--akshare-path", root, "--require-columns", "日期,收盘", *extra]


def test_fetch_writes_artifact_sidecar_and_full_provenance(fake_akshare: str, tmp_path: Path) -> None:
    output = tmp_path / "result.jsonl"
    payload = _run(_fetch(fake_akshare, "--arg", "symbol=000001", "--output", str(output)))

    assert payload["ok"] is True
    assert payload["result"]["rows"] == 3
    assert json.loads(output.read_text(encoding="utf-8").splitlines()[0])["代码"] == "000001"

    sidecar = json.loads((tmp_path / "result.jsonl.meta.json").read_text(encoding="utf-8"))
    provenance = sidecar["provenance"]
    assert provenance["parameters"]["symbol"] == "000001"
    assert provenance["akqry_version"] and provenance["akshare_version"] == "test"
    assert provenance["source_url"] == "https://example.invalid/demo"
    assert provenance["options"]["require_columns"] == ["日期", "收盘"]
    assert provenance["cache"] == {"hit": False, "key": None, "enabled": False}
    assert payload["result"]["sha256"] == sidecar["result"]["sha256"]


def test_fetch_refuses_empty_result_with_a_hint(fake_akshare: str) -> None:
    with pytest.raises(AkqryError) as raised:
        _run(_fetch(fake_akshare, "--arg", "symbol=EMPTY"))
    assert raised.value.code == "empty_result"
    assert "date range" in raised.value.details["hint"]


def test_fetch_refuses_orphaned_sidecar(fake_akshare: str, tmp_path: Path) -> None:
    output = tmp_path / "result.jsonl"
    (tmp_path / "result.jsonl.meta.json").write_text("{}", encoding="utf-8")
    with pytest.raises(AkqryError) as raised:
        _run(_fetch(fake_akshare, "--arg", "symbol=000001", "--output", str(output)))
    assert raised.value.code == "output_exists"


def test_no_sidecar_overwrite_removes_stale_provenance(fake_akshare: str, tmp_path: Path) -> None:
    output = tmp_path / "result.jsonl"
    sidecar = tmp_path / "result.jsonl.meta.json"
    _run(_fetch(fake_akshare, "--arg", "symbol=000001", "--output", str(output)))
    assert sidecar.is_file()

    _run(_fetch(fake_akshare, "--arg", "symbol=999999", "--output", str(output), "--overwrite", "--no-sidecar"))

    assert "999999" in output.read_text(encoding="utf-8")
    assert not sidecar.exists(), "a sidecar describing the previous query must not survive its data"


def test_upstream_exception_message_reaches_the_caller(fake_akshare: str) -> None:
    with pytest.raises(AkqryError) as raised:
        _run(_fetch(fake_akshare, "--arg", "symbol=BOOM", "--retries", "0"))
    assert raised.value.code == "upstream_error"
    assert "40001" in raised.value.details["exception_message"]
    assert raised.value.details["exception_type"] == "ValueError"
    assert raised.value.details["attempts"] == 1


def test_documented_enum_is_checked_before_the_call(fake_akshare: str, tmp_path: Path, monkeypatch) -> None:
    log = tmp_path / "calls.log"
    monkeypatch.setenv("AKQRY_TEST_CALL_LOG", str(log))
    with pytest.raises(AkqryError) as raised:
        _run(_fetch(fake_akshare, "--arg", "symbol=000001", "--arg", "period=日线"))
    assert raised.value.code == "invalid_parameter"
    assert raised.value.details["allowed_values"] == ["daily", "weekly"]
    assert not log.exists(), "the interface must not be called with a rejected value"

    payload = _run(_fetch(fake_akshare, "--arg", "period=日线", "--allow-unknown-values"))
    assert payload["ok"] is True
    assert "period" in payload["warnings"][0]


def test_parquet_without_an_engine_fails_before_the_call(fake_akshare: str, tmp_path: Path, monkeypatch) -> None:
    log = tmp_path / "calls.log"
    monkeypatch.setenv("AKQRY_TEST_CALL_LOG", str(log))
    monkeypatch.setattr(serializers.importlib.util, "find_spec", lambda _name: None)
    with pytest.raises(AkqryError) as raised:
        _run(_fetch(fake_akshare, "--arg", "symbol=000001", "--output", str(tmp_path / "a.parquet")))
    assert raised.value.code == "dependency_missing"
    assert raised.value.details["missing_any_of"] == ["pyarrow", "fastparquet"]
    assert not log.exists()


def test_batch_fetch_writes_one_artifact_and_sidecar_per_value(fake_akshare: str, tmp_path: Path, monkeypatch) -> None:
    log = tmp_path / "calls.log"
    monkeypatch.setenv("AKQRY_TEST_CALL_LOG", str(log))
    payload = _run(_fetch(fake_akshare, "--for-each", "symbol=000001,600519", "--output", str(tmp_path / "{}.jsonl")))

    assert payload["ok"] is True
    assert payload["result"]["kind"] == "batch"
    assert (payload["result"]["succeeded"], payload["result"]["failed"]) == (2, 0)
    assert log.read_text(encoding="utf-8").split() == ["000001", "600519"]
    for symbol in ("000001", "600519"):
        assert (tmp_path / f"{symbol}.jsonl").is_file()
        sidecar = json.loads((tmp_path / f"{symbol}.jsonl.meta.json").read_text(encoding="utf-8"))
        assert sidecar["provenance"]["parameters"]["symbol"] == symbol
        assert sidecar["provenance"]["label"] == symbol


def test_batch_keeps_successes_and_reports_failures(fake_akshare: str, tmp_path: Path) -> None:
    payload = _run(
        _fetch(
            fake_akshare,
            "--for-each",
            "symbol=000001,BOOM",
            "--output",
            str(tmp_path / "{}.jsonl"),
            "--retries",
            "0",
        )
    )

    assert payload["ok"] is False
    assert payload["error"]["code"] == "partial_failure"
    assert payload["error"]["details"]["failed_labels"] == ["BOOM"]
    assert (tmp_path / "000001.jsonl").is_file()
    assert not (tmp_path / "BOOM.jsonl").exists()


def test_batch_that_times_out_keeps_what_already_completed(fake_akshare: str, tmp_path: Path) -> None:
    payload = _run(
        _fetch(
            fake_akshare,
            "--for-each",
            "symbol=000001,SLOW",
            "--output",
            str(tmp_path / "{}.jsonl"),
            "--timeout",
            "3",
            "--retries",
            "0",
        )
    )

    assert payload["error"]["code"] == "partial_failure"
    items = {item["label"]: item for item in payload["result"]["items"]}
    assert items["000001"]["ok"] is True
    assert items["SLOW"]["error"]["code"] == "query_timeout"
    assert (tmp_path / "000001.jsonl").is_file()
    assert (tmp_path / "000001.jsonl.meta.json").is_file()
    assert not list(tmp_path.glob(".*akqry.tmp")), "temporary artifacts must not be left behind"


def test_batch_output_requires_a_placeholder(fake_akshare: str, tmp_path: Path) -> None:
    with pytest.raises(AkqryError) as raised:
        _run(_fetch(fake_akshare, "--for-each", "symbol=1,2", "--output", str(tmp_path / "fixed.jsonl")))
    assert raised.value.code == "usage_error"


def test_excluded_callable_reports_why_it_is_excluded(fake_akshare: str) -> None:
    with pytest.raises(AkqryError) as raised:
        _run(["fetch", "set_demo_token", "--akshare-path", fake_akshare])
    assert raised.value.code == "unsafe_callable"
    assert raised.value.details["excluded_reason"] == "state_or_secret_callable"


def test_iterated_parameter_cannot_also_be_fixed(fake_akshare: str) -> None:
    with pytest.raises(AkqryError) as raised:
        _run(_fetch(fake_akshare, "--arg", "symbol=000001", "--for-each", "symbol=1,2"))
    assert raised.value.code == "usage_error"


def test_cache_hit_reuses_the_artifact_and_keeps_the_original_timestamp(
    fake_akshare: str, tmp_path: Path, monkeypatch
) -> None:
    log = tmp_path / "calls.log"
    monkeypatch.setenv("AKQRY_TEST_CALL_LOG", str(log))
    shared = ("--arg", "symbol=000001", "--cache-dir", str(tmp_path / "cache"))
    first = _run(_fetch(fake_akshare, *shared, "--output", str(tmp_path / "one.jsonl")))
    second = _run(_fetch(fake_akshare, *shared, "--output", str(tmp_path / "two.jsonl")))

    assert log.read_text(encoding="utf-8").split() == ["000001"], "a cache hit must not call upstream again"
    assert first["provenance"]["cache"]["hit"] is False
    assert second["provenance"]["cache"]["hit"] is True
    assert second["provenance"]["retrieved_at_utc"] == first["provenance"]["retrieved_at_utc"]
    assert "served_at_utc" in second["provenance"]
    assert second["result"]["sha256"] == first["result"]["sha256"]
    assert (tmp_path / "two.jsonl").read_bytes() == (tmp_path / "one.jsonl").read_bytes()


def test_expired_cache_entry_is_refetched(fake_akshare: str, tmp_path: Path, monkeypatch) -> None:
    log = tmp_path / "calls.log"
    monkeypatch.setenv("AKQRY_TEST_CALL_LOG", str(log))
    shared = ("--arg", "symbol=000001", "--cache-dir", str(tmp_path / "cache"), "--cache-ttl", "0")
    _run(_fetch(fake_akshare, *shared))
    payload = _run(_fetch(fake_akshare, *shared))

    assert payload["provenance"]["cache"]["hit"] is False
    assert log.read_text(encoding="utf-8").split() == ["000001", "000001"]


def test_describe_probe_reports_the_real_schema(fake_akshare: str) -> None:
    payload = _run(["describe", "stock_demo", "--akshare-path", fake_akshare, "--probe", "--arg", "n=2"])
    probe = payload["result"]["probe"]

    assert probe["ok"] is True
    assert probe["rows"] == 2
    assert [column["name"] for column in probe["columns"]] == ["日期", "代码", "收盘"]
    assert probe["temporal_bounds"][0]["column"] == "日期"


def test_describe_probe_reports_upstream_failure_without_hiding_metadata(fake_akshare: str) -> None:
    payload = _run(
        ["describe", "stock_demo", "--akshare-path", fake_akshare, "--probe", "--arg", "symbol=BOOM", "--retries", "0"]
    )
    assert payload["result"]["signature"].startswith("(symbol")
    assert payload["result"]["probe"]["ok"] is False
    assert "40001" in payload["result"]["probe"]["error"]["details"]["exception_message"]
