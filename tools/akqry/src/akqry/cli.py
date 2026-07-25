"""Command line interface for akqry."""

from __future__ import annotations

import argparse
import inspect
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from akqry import __version__, cache
from akqry.catalog import describe, discover, search
from akqry.errors import AkqryError
from akqry.runtime import load_akshare, runtime_provenance
from akqry.serializers import ensure_format_available, infer_format, sha256

_UNSAFE_LABEL = re.compile(r"[^0-9A-Za-z._一-鿿-]+")
# Head-room for the worker's own start-up and AkShare import, on top of the
# per-call budgets it enforces internally.
WORKER_GRACE_SECONDS = 30.0


@dataclass
class PlannedCall:
    """One AkShare call and where its artifact belongs."""

    index: int
    parameters: dict[str, Any]
    label: str | None = None
    destination: Path | None = None
    sidecar: Path | None = None
    temporary_output: Path | None = None
    cache_key: str | None = None
    cached: dict[str, Any] | None = None
    outcome: dict[str, Any] = field(default_factory=dict)


def _json(payload: dict[str, Any]) -> None:
    print(json.dumps(payload, ensure_ascii=False, default=str))


def _print_records(records: list[dict[str, Any]]) -> None:
    for record in records:
        print(f"{record['name']} {record.get('signature', '')}")
        description = record.get("description")
        domains = ", ".join(record.get("domains") or [])
        detail = "  ".join(part for part in (description, f"[{domains}]" if domains else "") if part)
        if detail:
            print(f"    {detail}")


def _human(payload: dict[str, Any]) -> None:
    if not payload.get("ok"):
        error = payload["error"]
        print(f"{error['code']}: {error['message']}", file=sys.stderr)
        if error.get("details"):
            print(json.dumps(error["details"], ensure_ascii=False), file=sys.stderr)
        if "result" not in payload:
            return
    result = payload.get("result", payload)
    if isinstance(result, dict) and "results" in result:
        _print_records(result["results"])
        print(f"matched={result['total_matched']} of {result['candidates']} candidates")
        for hint in result["hints"]:
            print(f"note: {hint}", file=sys.stderr)
    elif isinstance(result, dict) and result.get("kind") == "batch":
        for item in result["items"]:
            status = f"rows={item['rows']} {item.get('output') or ''}" if item["ok"] else item["error"]["code"]
            print(f"{item.get('label')}: {status}")
        print(f"succeeded={result['succeeded']} failed={result['failed']}")
    elif isinstance(result, dict) and "preview" in result:
        print(json.dumps(result["preview"], ensure_ascii=False, indent=2, default=str))
        print(f"rows={result['rows']} columns={len(result['columns'])}")
    else:
        print(json.dumps(result, ensure_ascii=False, indent=2, default=str))


def _emit(payload: dict[str, Any], as_json: bool) -> None:
    _json(payload) if as_json else _human(payload)


def _error_payload(error: Exception, debug: bool = False) -> dict[str, Any]:
    if isinstance(error, AkqryError):
        detail = error.as_dict()
    else:
        detail = {
            "code": "internal_error",
            "message": "Unexpected akqry error.",
            "details": {"exception_type": type(error).__name__},
        }
    if debug:
        import traceback

        detail["traceback"] = traceback.format_exc()
    return {"ok": False, "error": detail}


def _add_runtime_options(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--akshare-path", help="Repository root containing akshare/__init__.py")
    parser.add_argument("--docs-root", help="AkShare docs directory; auto-detected for a source checkout")
    parser.add_argument("--json", action="store_true", help="Emit one machine-readable JSON envelope")
    parser.add_argument("--debug", action="store_true", help="Include traceback in error envelopes")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="akqry", description="Auditable discovery and retrieval for AkShare")
    subparsers = parser.add_subparsers(dest="command", required=True)

    version = subparsers.add_parser("version", help="Show akqry version")
    version.add_argument("--json", action="store_true")

    doctor = subparsers.add_parser("doctor", help="Inspect local runtime without querying market data")
    _add_runtime_options(doctor)
    doctor.add_argument("--cache-dir", help="Cache directory to report on")

    search_parser = subparsers.add_parser("search", help="Search public AkShare data interfaces")
    _add_runtime_options(search_parser)
    search_parser.add_argument("query")
    search_parser.add_argument("--domain", choices=["a-share", "hk-share", "board", "fund", "etf", "index"])
    search_parser.add_argument("--limit", type=int, default=10)
    search_parser.add_argument("--full", action="store_true", help="Return complete records instead of summaries")
    search_parser.add_argument("--all", action="store_true", help="Include excluded non-fetchable callables")

    describe_parser = subparsers.add_parser("describe", help="Show signature and documentation for one interface")
    _add_runtime_options(describe_parser)
    describe_parser.add_argument("function")
    describe_parser.add_argument("--all", action="store_true", help="Allow describing excluded callables")
    describe_parser.add_argument(
        "--probe",
        action="store_true",
        help="Call the interface once to report its real columns, dtypes and date range",
    )
    describe_parser.add_argument("--arg", action="append", default=[], metavar="NAME=VALUE", help="Probe parameter")
    describe_parser.add_argument("--preview-rows", type=int, default=3)
    describe_parser.add_argument("--timeout", type=float, default=120.0)
    describe_parser.add_argument("--retries", type=int, default=1)
    describe_parser.add_argument("--cache-dir", help="Reuse an identical probe from this directory (or AKQRY_CACHE_DIR)")
    describe_parser.add_argument("--cache-ttl", type=float, default=cache.DEFAULT_TTL_SECONDS)

    fetch = subparsers.add_parser("fetch", help="Execute a safe AkShare data interface")
    _add_runtime_options(fetch)
    fetch.add_argument("function")
    fetch.add_argument("--arg", action="append", default=[], metavar="NAME=VALUE")
    fetch.add_argument("--params-json", help="JSON object with typed parameters")
    fetch.add_argument("--params-file", help="Path to JSON parameter object, or - for stdin")
    fetch.add_argument(
        "--for-each",
        metavar="NAME=V1,V2",
        help="Repeat the call over one parameter in a single process; --output must contain {}",
    )
    fetch.add_argument("--require-columns", default="", help="Comma-separated required result columns")
    fetch.add_argument("--select", default="", help="Comma-separated columns to retain in the artifact")
    fetch.add_argument("--preview-rows", type=int, default=10)
    fetch.add_argument("--output", help="Destination ending in .parquet, .jsonl, or .csv")
    fetch.add_argument("--format", choices=["parquet", "jsonl", "csv"])
    fetch.add_argument("--allow-empty", action="store_true")
    fetch.add_argument("--overwrite", action="store_true")
    fetch.add_argument("--no-sidecar", action="store_true")
    fetch.add_argument(
        "--allow-unknown-values",
        action="store_true",
        help="Downgrade documented-enum violations from an error to a warning",
    )
    fetch.add_argument("--cache-dir", help="Reuse identical queries from this directory (or AKQRY_CACHE_DIR)")
    fetch.add_argument("--cache-ttl", type=float, default=cache.DEFAULT_TTL_SECONDS)
    fetch.add_argument("--timeout", type=float, default=120.0, help="Per-call budget, retries included")
    fetch.add_argument("--retries", type=int, default=2)
    fetch.add_argument(
        "--delay",
        type=float,
        default=0.0,
        metavar="SECONDS",
        help="Pause between the calls of a batch; upstream sites throttle bursts",
    )
    return parser


def _split_columns(value: str) -> list[str]:
    return [item.strip() for item in value.split(",") if item.strip()]


def _typed_value(raw: str, parameter: inspect.Parameter) -> Any:
    annotation = parameter.annotation
    expected = annotation if annotation is not inspect.Parameter.empty else type(parameter.default)
    if expected is bool:
        normalized = raw.lower()
        if normalized in {"true", "1", "yes"}:
            return True
        if normalized in {"false", "0", "no"}:
            return False
        raise AkqryError("invalid_parameter", "Boolean parameters accept true/false.", {"parameter": parameter.name})
    if expected is int:
        try:
            return int(raw)
        except ValueError as exc:
            raise AkqryError("invalid_parameter", "Expected an integer parameter.", {"parameter": parameter.name}) from exc
    if expected is float:
        try:
            return float(raw)
        except ValueError as exc:
            raise AkqryError("invalid_parameter", "Expected a numeric parameter.", {"parameter": parameter.name}) from exc
    return raw


def _parameter(signature: inspect.Signature, name: str) -> inspect.Parameter:
    parameter = signature.parameters.get(name)
    if parameter is None:
        raise AkqryError(
            "invalid_parameter",
            "Unknown parameter.",
            {"parameter": name, "accepted_parameters": list(signature.parameters)},
        )
    return parameter


def _parse_parameters(args: argparse.Namespace, function: Any) -> dict[str, Any]:
    supplied: dict[str, Any] = {}
    if getattr(args, "params_json", None):
        try:
            parsed = json.loads(args.params_json)
        except json.JSONDecodeError as exc:
            raise AkqryError("invalid_parameter", "--params-json must be a JSON object.") from exc
        if not isinstance(parsed, dict):
            raise AkqryError("invalid_parameter", "--params-json must be a JSON object.")
        supplied.update(parsed)
    if getattr(args, "params_file", None):
        content = sys.stdin.read() if args.params_file == "-" else Path(args.params_file).read_text(encoding="utf-8")
        try:
            parsed = json.loads(content)
        except json.JSONDecodeError as exc:
            raise AkqryError("invalid_parameter", "--params-file must contain a JSON object.") from exc
        if not isinstance(parsed, dict) or set(supplied).intersection(parsed):
            raise AkqryError("invalid_parameter", "Parameter sources must be objects with no duplicate keys.")
        supplied.update(parsed)
    signature = inspect.signature(function)
    for item in args.arg:
        if "=" not in item:
            raise AkqryError("invalid_parameter", "--arg must use NAME=VALUE.", {"argument": item})
        name, raw = item.split("=", 1)
        if name in supplied:
            raise AkqryError("invalid_parameter", "A parameter was supplied more than once.", {"parameter": name})
        supplied[name] = _typed_value(raw, _parameter(signature, name))
    try:
        signature.bind(**supplied)
    except TypeError as exc:
        code = "missing_parameter" if "missing a required" in str(exc) else "invalid_parameter"
        raise AkqryError(
            code,
            "Parameters do not match the selected function signature.",
            {"reason": str(exc), "signature": str(signature)},
        ) from exc
    return supplied


def _documented_enums(record: dict[str, Any]) -> dict[str, list[str]]:
    return {item["name"]: item["enum"] for item in record.get("parameters", []) if item.get("enum")}


def _check_enums(record: dict[str, Any], parameters: dict[str, Any], allow_unknown: bool) -> list[str]:
    """Catch values the docstring does not list, before a network call is spent."""
    warnings: list[str] = []
    for name, allowed in _documented_enums(record).items():
        if name not in parameters:
            continue
        value = parameters[name]
        if not isinstance(value, (str, int, float)) or isinstance(value, bool):
            continue
        if str(value) in {str(item) for item in allowed}:
            continue
        if allow_unknown:
            warnings.append(f"{name}={value!r} is not one of the documented values {allowed}.")
            continue
        raise AkqryError(
            "invalid_parameter",
            "Parameter value is not one of the values documented for this interface.",
            {
                "parameter": name,
                "value": value,
                "allowed_values": allowed,
                "remedy": "Fix the value, or pass --allow-unknown-values if the documentation is stale.",
            },
        )
    return warnings


def _redacted_parameters(parameters: dict[str, Any]) -> dict[str, Any]:
    sensitive = ("token", "secret", "password", "cookie", "api_key", "apikey")
    return {key: "<redacted>" if any(item in key.lower() for item in sensitive) else value for key, value in parameters.items()}


def _temporary_path(destination: Path) -> Path:
    handle, name = tempfile.mkstemp(prefix=f".{destination.name}.", suffix=".akqry.tmp", dir=destination.parent)
    os.close(handle)
    return Path(name)


def _worker_run(spec: dict[str, Any], timeout: float) -> dict[str, Any]:
    """Run one worker process, keeping whatever it reported before a timeout."""
    with tempfile.TemporaryDirectory(prefix="akqry-") as tempdir:
        root = Path(tempdir)
        request, response = root / "request.json", root / "response.json"
        request.write_text(json.dumps(spec, ensure_ascii=False, default=str), encoding="utf-8")
        timed_out, returncode = False, None
        try:
            process = subprocess.run(
                [sys.executable, "-m", "akqry.worker", "--request", str(request), "--response", str(response)],
                capture_output=True,
                text=True,
                timeout=timeout,
                check=False,
            )
            returncode = process.returncode
        except subprocess.TimeoutExpired:
            timed_out = True
        payload: dict[str, Any] | None = None
        if response.exists():
            try:
                payload = json.loads(response.read_text(encoding="utf-8"))
            except json.JSONDecodeError:
                payload = None
    if payload is None:
        if timed_out:
            return {
                "ok": False,
                "error": {
                    "code": "query_timeout",
                    "message": "AkShare query exceeded the configured timeout.",
                    "details": {"timeout_seconds": timeout, "hint": "Narrow the date range or raise --timeout."},
                    "retryable": True,
                },
            }
        return {
            "ok": False,
            "error": {
                "code": "upstream_error",
                "message": "Worker exited without a response.",
                "details": {"returncode": returncode},
                "retryable": False,
            },
        }
    payload["timed_out"] = timed_out
    return payload


def _label_for_path(value: Any) -> str:
    return _UNSAFE_LABEL.sub("_", str(value)) or "value"


def _plan_calls(args: argparse.Namespace, signature: inspect.Signature, base: dict[str, Any]) -> list[PlannedCall]:
    template = args.output
    if not args.for_each:
        destination = Path(template).expanduser().resolve() if template else None
        return [PlannedCall(index=0, parameters=base, destination=destination)]
    if "=" not in args.for_each:
        raise AkqryError("usage_error", "--for-each must use NAME=V1,V2.", {"argument": args.for_each})
    name, raw_values = args.for_each.split("=", 1)
    parameter = _parameter(signature, name)
    if name in base:
        raise AkqryError(
            "usage_error",
            "A parameter cannot be both fixed and iterated over.",
            {"parameter": name, "fixed_value": base[name]},
        )
    values = raw_values.split(",")
    if len(values) < 2:
        raise AkqryError("usage_error", "--for-each needs at least two comma-separated values.", {"parameter": name})
    if len(set(values)) != len(values):
        raise AkqryError("usage_error", "--for-each values must be unique.", {"parameter": name})
    if template and "{}" not in template:
        raise AkqryError(
            "usage_error",
            "A batch --output must contain {} as the per-value placeholder.",
            {"output": template, "example": "/tmp/{}.parquet"},
        )
    calls: list[PlannedCall] = []
    for index, raw in enumerate(values):
        destination = None
        if template:
            destination = Path(template.replace("{}", _label_for_path(raw))).expanduser().resolve()
        calls.append(
            PlannedCall(
                index=index,
                parameters={**base, name: _typed_value(raw, parameter)},
                label=raw,
                destination=destination,
            )
        )
    return calls


def _prepare_destinations(calls: list[PlannedCall], args: argparse.Namespace, output_format: str | None) -> None:
    """Resolve sidecars and refuse unsafe writes before any network call happens."""
    seen: dict[Path, str | None] = {}
    for call in calls:
        if call.destination is None:
            continue
        if output_format is None:
            raise AkqryError(
                "usage_error",
                "Output format must be explicit or inferred from .parquet, .jsonl, or .csv.",
                {"output": str(call.destination)},
            )
        if call.destination in seen:
            raise AkqryError(
                "usage_error",
                "Two calls resolve to the same output path.",
                {"output": str(call.destination), "labels": [seen[call.destination], call.label]},
            )
        seen[call.destination] = call.label
        call.sidecar = Path(f"{call.destination}.meta.json")
        existing = [path for path in (call.destination, call.sidecar) if path.exists()]
        if existing and not args.overwrite:
            raise AkqryError(
                "output_exists",
                "Refusing to overwrite an existing output or provenance sidecar.",
                {"paths": [str(path) for path in existing], "remedy": "Pass --overwrite or choose another path."},
            )
        if not call.destination.parent.is_dir():
            raise AkqryError("write_failed", "Output parent directory does not exist.", {"path": str(call.destination.parent)})


def _fetch_options(args: argparse.Namespace, output_format: str | None) -> dict[str, Any]:
    return {
        "require_columns": _split_columns(args.require_columns),
        "select": _split_columns(args.select) or None,
        "preview_rows": args.preview_rows,
        "allow_empty": bool(args.allow_empty),
        "output_format": output_format,
        "timeout_seconds": args.timeout,
        "retries": args.retries,
        "delay_seconds": args.delay,
    }


def _batch_budget(args: argparse.Namespace, calls: int) -> float:
    """Backstop for the worker as a whole; each call polices its own budget."""
    return args.timeout * calls + args.delay * max(calls - 1, 0) + WORKER_GRACE_SECONDS


def _cache_material(function: str, call: PlannedCall, options: dict[str, Any], akshare_version: str | None) -> dict[str, Any]:
    return {
        "akqry_version": __version__,
        "akshare_version": akshare_version,
        "function": function,
        "parameters": call.parameters,
        "require_columns": options["require_columns"],
        "select": options["select"],
        "preview_rows": options["preview_rows"],
        "allow_empty": options["allow_empty"],
        "output_format": options["output_format"],
    }


def _promote(source: Path, destination: Path) -> None:
    temporary = _temporary_path(destination)
    try:
        shutil.copyfile(source, temporary)
        os.replace(temporary, destination)
    finally:
        if temporary.exists():
            temporary.unlink()


def _finalize_artifact(call: PlannedCall, result: dict[str, Any], output_format: str | None) -> None:
    if call.destination is None:
        return
    result["output"] = str(call.destination)
    result["output_format"] = output_format
    result["sha256"] = sha256(call.destination)


def _write_sidecar(call: PlannedCall, payload: dict[str, Any], no_sidecar: bool) -> None:
    """Keep the sidecar and the artifact in step, including when it is disabled."""
    if call.sidecar is None:
        return
    if no_sidecar:
        # A stale sidecar next to fresh data would misreport provenance.
        if call.sidecar.exists():
            call.sidecar.unlink()
        return
    temporary = _temporary_path(call.sidecar)
    temporary.write_text(json.dumps(payload, ensure_ascii=False, indent=2, default=str), encoding="utf-8")
    os.replace(temporary, call.sidecar)
    payload["result"]["metadata"] = str(call.sidecar)


def _handle_fetch(args: argparse.Namespace) -> dict[str, Any]:
    if args.timeout <= 0 or args.preview_rows < 0 or args.retries < 0 or args.delay < 0:
        raise AkqryError(
            "usage_error",
            "--timeout must be positive; --preview-rows, --retries and --delay cannot be negative.",
        )
    module = load_akshare(args.akshare_path)
    # Include excluded callables so that asking for one reports why it is excluded
    # rather than claiming the name does not exist.
    catalog = discover(module, args.docs_root, include_unsafe=True)
    record = catalog.get(args.function)
    if record is None:
        raise AkqryError(
            "function_not_found",
            "No public AkShare callable with that name was found.",
            {"function": args.function, "hint": "Use `akqry search` to find the current interface name."},
        )
    if not record["safe_to_fetch"]:
        raise AkqryError(
            "unsafe_callable",
            "This callable is excluded from fetch.",
            {"function": args.function, "excluded_reason": record.get("excluded_reason")},
        )
    function = getattr(module, args.function)
    signature = inspect.signature(function)
    base_parameters = _parse_parameters(args, function)
    output_format = infer_format(args.output, args.format)
    ensure_format_available(output_format)
    calls = _plan_calls(args, signature, base_parameters)
    warnings: list[str] = []
    for call in calls:
        warnings.extend(_check_enums(record, call.parameters, args.allow_unknown_values))
    _prepare_destinations(calls, args, output_format)

    options = _fetch_options(args, output_format)
    runtime = runtime_provenance(module)
    cache_root = cache.resolve_root(args.cache_dir)
    if cache_root is not None:
        for call in calls:
            call.cache_key = cache.cache_key(_cache_material(args.function, call, options, runtime["akshare_version"]))
            call.cached = cache.load(cache_root, call.cache_key, args.cache_ttl)

    pending = [call for call in calls if call.cached is None]
    started = time.monotonic()
    worker_payload: dict[str, Any] = {"ok": True, "items": []}
    try:
        if pending:
            for call in pending:
                if call.destination is not None:
                    call.temporary_output = _temporary_path(call.destination)
            worker_payload = _worker_run(
                {
                    "function": args.function,
                    "akshare_path": args.akshare_path,
                    "docs_root": args.docs_root,
                    "require_columns": options["require_columns"],
                    "select": options["select"],
                    "preview_rows": options["preview_rows"],
                    "allow_empty": options["allow_empty"],
                    "output_format": output_format,
                    "retries": args.retries,
                    "call_timeout": args.timeout,
                    "delay": args.delay,
                    "debug": args.debug,
                    "calls": [
                        {
                            "index": call.index,
                            "label": call.label,
                            "parameters": call.parameters,
                            "temporary_output": str(call.temporary_output) if call.temporary_output else None,
                        }
                        for call in pending
                    ],
                },
                _batch_budget(args, len(pending)),
            )
            if not worker_payload["ok"]:
                error = worker_payload["error"]
                raise AkqryError(error["code"], error["message"], error.get("details", {}), error.get("retryable", False))
            runtime = worker_payload.get("provenance") or runtime
        duration = round(time.monotonic() - started, 3)
        reported = {item["index"]: item for item in worker_payload["items"]}
        for call in pending:
            call.outcome = reported.get(
                call.index,
                {
                    "ok": False,
                    "attempts": 0,
                    "error": {
                        "code": "query_timeout",
                        "message": "The batch timed out before this call ran.",
                        "details": {"timeout_seconds": options["timeout_seconds"], "label": call.label},
                        "retryable": True,
                    },
                },
            )
        return _assemble(args, record, calls, options, runtime, duration, cache_root, warnings)
    finally:
        for call in calls:
            if call.temporary_output and call.temporary_output.exists():
                call.temporary_output.unlink()


def _item_provenance(
    args: argparse.Namespace,
    record: dict[str, Any],
    call: PlannedCall,
    options: dict[str, Any],
    runtime: dict[str, Any],
    duration: float,
) -> dict[str, Any]:
    now = datetime.now(timezone.utc)
    provenance: dict[str, Any] = {
        **runtime,
        "akqry_version": __version__,
        "function": args.function,
        "parameters": _redacted_parameters(call.parameters),
        "label": call.label,
        "source_url": record.get("source_url"),
        "source_site": record.get("source_site"),
        "options": options,
    }
    if call.cached is not None:
        provenance.update(
            {
                "retrieved_at_utc": call.cached.get("retrieved_at_utc"),
                "retrieved_at_local": call.cached.get("retrieved_at_local"),
                "started_at_utc": call.cached.get("started_at_utc"),
                "duration_seconds": call.cached.get("duration_seconds"),
                "attempts": call.cached.get("attempts"),
                "served_at_utc": now.isoformat(),
                "cache": {
                    "hit": True,
                    "key": call.cache_key,
                    "age_seconds": call.cached.get("age_seconds"),
                    "ttl_seconds": args.cache_ttl,
                },
            }
        )
        return provenance
    # A batch spans minutes, so each artifact is stamped with when its own call
    # finished rather than with when the batch was assembled.
    retrieved = call.outcome.get("finished_at_utc")
    stamp = datetime.fromisoformat(retrieved) if retrieved else now
    provenance.update(
        {
            "retrieved_at_utc": stamp.isoformat(),
            "retrieved_at_local": stamp.astimezone().isoformat(),
            "started_at_utc": call.outcome.get("started_at_utc"),
            "duration_seconds": call.outcome.get("duration_seconds", duration),
            "attempts": call.outcome.get("attempts"),
            "cache": {"hit": False, "key": call.cache_key, "enabled": call.cache_key is not None},
        }
    )
    return provenance


def _assemble(
    args: argparse.Namespace,
    record: dict[str, Any],
    calls: list[PlannedCall],
    options: dict[str, Any],
    runtime: dict[str, Any],
    duration: float,
    cache_root: Path | None,
    warnings: list[str],
) -> dict[str, Any]:
    """Promote artifacts, write sidecars, refresh the cache and build the envelope."""
    payloads: dict[int, dict[str, Any]] = {}
    for call in calls:
        if call.cached is not None:
            result = dict(call.cached["result"])
            if call.destination is not None:
                data_path = call.cached.get("data_path")
                if data_path is None:
                    raise AkqryError("internal_error", "Cache entry has no artifact for a requested output.")
                _promote(Path(data_path), call.destination)
        elif call.outcome.get("ok"):
            result = call.outcome["result"]
            if call.destination is not None and call.temporary_output is not None:
                os.replace(call.temporary_output, call.destination)
        else:
            continue
        provenance = _item_provenance(args, record, call, options, runtime, duration)
        _finalize_artifact(call, result, options["output_format"])
        item_payload = {"ok": True, "command": "fetch", "result": result, "provenance": provenance}
        if warnings:
            item_payload["warnings"] = warnings
        _write_sidecar(call, item_payload, args.no_sidecar)
        payloads[call.index] = item_payload
        if cache_root is not None and call.cached is None and call.cache_key is not None:
            cache.store(
                cache_root,
                call.cache_key,
                {
                    "function": args.function,
                    "parameters": _redacted_parameters(call.parameters),
                    "result": {key: value for key, value in result.items() if key not in {"output", "output_format", "sha256", "metadata"}},
                    "retrieved_at_utc": provenance["retrieved_at_utc"],
                    "retrieved_at_local": provenance["retrieved_at_local"],
                    "started_at_utc": provenance.get("started_at_utc"),
                    "duration_seconds": provenance["duration_seconds"],
                    "attempts": provenance["attempts"],
                },
                call.destination,
            )

    if len(calls) == 1:
        call = calls[0]
        if call.index in payloads:
            return payloads[call.index]
        error = call.outcome["error"]
        details = {**error.get("details", {}), "attempts": call.outcome.get("attempts")}
        raise AkqryError(error["code"], error["message"], details, error.get("retryable", False))

    items: list[dict[str, Any]] = []
    for call in calls:
        payload = payloads.get(call.index)
        if payload is not None:
            result = payload["result"]
            items.append(
                {
                    "label": call.label,
                    "ok": True,
                    "rows": result["rows"],
                    "output": result.get("output"),
                    "metadata": result.get("metadata"),
                    "schema_fingerprint": result["schema_fingerprint"],
                    "temporal_bounds": result["temporal_bounds"],
                    "cache_hit": payload["provenance"]["cache"]["hit"],
                }
            )
        else:
            items.append({"label": call.label, "ok": False, "error": call.outcome["error"]})
    succeeded = [item for item in items if item["ok"]]
    failed = [item for item in items if not item["ok"]]
    result = {
        "kind": "batch",
        "requested": len(calls),
        "succeeded": len(succeeded),
        "failed": len(failed),
        "items": items,
    }
    provenance = {
        **runtime,
        "akqry_version": __version__,
        "function": args.function,
        "source_url": record.get("source_url"),
        "source_site": record.get("source_site"),
        "options": options,
        "duration_seconds": duration,
        "retrieved_at_utc": datetime.now(timezone.utc).isoformat(),
    }
    envelope: dict[str, Any] = {"ok": not failed, "command": "fetch", "result": result, "provenance": provenance}
    if warnings:
        envelope["warnings"] = warnings
    if failed:
        first = failed[0]["error"]
        envelope["error"] = (
            {
                "code": "partial_failure",
                "message": "Some calls in the batch failed; successful artifacts were still written.",
                "details": {"failed_labels": [item["label"] for item in failed], "first_error": first},
                "retryable": False,
            }
            if succeeded
            else first
        )
    return envelope


def _handle_describe(args: argparse.Namespace, module: Any, catalog: dict[str, dict[str, Any]]) -> dict[str, Any]:
    record = describe(catalog, args.function)
    payload = {"ok": True, "command": "describe", "result": record, "provenance": runtime_provenance(module)}
    if not args.probe:
        return payload
    if not record["safe_to_fetch"]:
        raise AkqryError(
            "unsafe_callable",
            "This callable is excluded from fetch, so it cannot be probed.",
            {"function": args.function, "excluded_reason": record.get("excluded_reason")},
        )
    function = getattr(module, args.function)
    parameters = _parse_parameters(args, function)
    probe: dict[str, Any] = {"parameters": _redacted_parameters(parameters)}

    # The workflow asks for a probe before every analysis, so the same schema
    # question is asked over and over; serve it from the cache when there is one.
    cache_root = cache.resolve_root(args.cache_dir)
    key = (
        cache.cache_key(
            {
                "kind": "probe",
                "akqry_version": __version__,
                "akshare_version": runtime_provenance(module)["akshare_version"],
                "function": args.function,
                "parameters": parameters,
                "preview_rows": max(args.preview_rows, 0),
            }
        )
        if cache_root is not None
        else None
    )
    if cache_root is not None and key is not None:
        entry = cache.load(cache_root, key, args.cache_ttl)
        if entry is not None:
            record["probe"] = {
                **probe,
                **entry["probe"],
                "cache": {"hit": True, "key": key, "age_seconds": entry.get("age_seconds")},
            }
            return payload

    worker_payload = _worker_run(
        {
            "function": args.function,
            "akshare_path": args.akshare_path,
            "docs_root": args.docs_root,
            "require_columns": [],
            "select": None,
            "preview_rows": max(args.preview_rows, 0),
            "allow_empty": True,
            "output_format": None,
            "retries": max(args.retries, 0),
            "call_timeout": args.timeout,
            "debug": args.debug,
            "calls": [{"index": 0, "label": None, "parameters": parameters, "temporary_output": None}],
        },
        args.timeout + WORKER_GRACE_SECONDS,
    )
    if not worker_payload["ok"]:
        probe.update({"ok": False, "error": worker_payload["error"]})
    else:
        item = worker_payload["items"][0]
        if item["ok"]:
            result = item["result"]
            probe.update(
                {
                    "ok": True,
                    "rows": result["rows"],
                    "columns": result["columns"],
                    "schema_fingerprint": result["schema_fingerprint"],
                    "temporal_bounds": result["temporal_bounds"],
                    "preview": result["preview"],
                    "attempts": item["attempts"],
                }
            )
        else:
            probe.update({"ok": False, "error": item["error"], "attempts": item["attempts"]})
    probe["retrieved_at_utc"] = datetime.now(timezone.utc).isoformat()
    if cache_root is not None and key is not None and probe.get("ok"):
        # Only a schema that was actually observed is worth replaying.
        cache.store(cache_root, key, {"probe": {key_: probe[key_] for key_ in probe if key_ != "parameters"}}, None)
    record["probe"] = {**probe, "cache": {"hit": False, "key": key, "enabled": cache_root is not None}}
    return payload


def _handle(args: argparse.Namespace) -> dict[str, Any]:
    if args.command == "version":
        return {"ok": True, "command": "version", "result": {"akqry_version": __version__}}
    if args.command == "doctor":
        module = load_akshare(args.akshare_path)
        provenance = runtime_provenance(module)
        docs_root = args.docs_root or str(Path(provenance["akshare_module_path"] or ".").parent.parent / "docs")
        formats = {"jsonl": True, "csv": True, "parquet": _format_available("parquet")}
        cache_root = cache.resolve_root(args.cache_dir)
        return {
            "ok": True,
            "command": "doctor",
            "result": {
                **provenance,
                "akqry_version": __version__,
                "docs_root": docs_root,
                "docs_available": Path(docs_root).is_dir(),
                "output_formats_available": formats,
                "cache_dir": str(cache_root) if cache_root else None,
            },
        }
    module = load_akshare(args.akshare_path)
    catalog = discover(module, args.docs_root, getattr(args, "all", False))
    if args.command == "search":
        if args.limit <= 0:
            raise AkqryError("usage_error", "--limit must be positive.")
        return {
            "ok": True,
            "command": "search",
            "result": search(catalog, args.query, args.domain, args.limit, args.full),
            "provenance": runtime_provenance(module),
        }
    if args.command == "describe":
        return _handle_describe(args, module, catalog)
    if args.command == "fetch":
        return _handle_fetch(args)
    raise AkqryError("usage_error", "Unknown command.")


def _format_available(output_format: str) -> bool:
    try:
        ensure_format_available(output_format)
    except AkqryError:
        return False
    return True


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        payload = _handle(args)
    except Exception as exc:
        payload = _error_payload(exc, bool(getattr(args, "debug", False)))
    _emit(payload, bool(getattr(args, "json", False)))
    if payload.get("ok"):
        return 0
    error = payload["error"]
    return AkqryError(error["code"], error["message"]).exit_code
