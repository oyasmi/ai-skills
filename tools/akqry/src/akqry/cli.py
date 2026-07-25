"""Command line interface for akqry."""

from __future__ import annotations

import argparse
import inspect
import json
import os
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from akqry import __version__
from akqry.catalog import describe, discover, search
from akqry.errors import AkqryError
from akqry.runtime import load_akshare, runtime_provenance
from akqry.serializers import infer_format, sha256


def _json(payload: dict[str, Any]) -> None:
    print(json.dumps(payload, ensure_ascii=False, default=str))


def _human(payload: dict[str, Any]) -> None:
    if not payload.get("ok"):
        error = payload["error"]
        print(f"{error['code']}: {error['message']}", file=sys.stderr)
        if error.get("details"):
            print(json.dumps(error["details"], ensure_ascii=False), file=sys.stderr)
        return
    result = payload.get("result", payload)
    if isinstance(result, list):
        for record in result:
            print(f"{record['name']} {record['signature']}  {record.get('description') or ''}")
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
        detail = {"code": "internal_error", "message": "Unexpected akqry error.", "details": {"exception_type": type(error).__name__}}
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

    search_parser = subparsers.add_parser("search", help="Search public AkShare data interfaces")
    _add_runtime_options(search_parser)
    search_parser.add_argument("query")
    search_parser.add_argument("--domain", choices=["a-share", "hk-share", "board", "fund", "etf"])
    search_parser.add_argument("--limit", type=int, default=20)
    search_parser.add_argument("--all", action="store_true", help="Include excluded non-fetchable callables")

    describe_parser = subparsers.add_parser("describe", help="Show signature and documentation for one interface")
    _add_runtime_options(describe_parser)
    describe_parser.add_argument("function")
    describe_parser.add_argument("--all", action="store_true", help="Allow describing excluded callables")

    fetch = subparsers.add_parser("fetch", help="Execute a safe AkShare data interface")
    _add_runtime_options(fetch)
    fetch.add_argument("function")
    fetch.add_argument("--arg", action="append", default=[], metavar="NAME=VALUE")
    fetch.add_argument("--params-json", help="JSON object with typed parameters")
    fetch.add_argument("--params-file", help="Path to JSON parameter object, or - for stdin")
    fetch.add_argument("--require-columns", default="", help="Comma-separated required result columns")
    fetch.add_argument("--select", default="", help="Comma-separated columns to retain in the artifact")
    fetch.add_argument("--preview-rows", type=int, default=10)
    fetch.add_argument("--output", help="Destination ending in .parquet, .jsonl, or .csv")
    fetch.add_argument("--format", choices=["parquet", "jsonl", "csv"])
    fetch.add_argument("--allow-empty", action="store_true")
    fetch.add_argument("--overwrite", action="store_true")
    fetch.add_argument("--no-sidecar", action="store_true")
    fetch.add_argument("--timeout", type=float, default=120.0)
    fetch.add_argument("--retries", type=int, default=2)
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


def _parse_parameters(args: argparse.Namespace, function: object) -> dict[str, Any]:
    supplied: dict[str, Any] = {}
    if args.params_json:
        try:
            parsed = json.loads(args.params_json)
        except json.JSONDecodeError as exc:
            raise AkqryError("invalid_parameter", "--params-json must be a JSON object.") from exc
        if not isinstance(parsed, dict):
            raise AkqryError("invalid_parameter", "--params-json must be a JSON object.")
        supplied.update(parsed)
    if args.params_file:
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
        parameter = signature.parameters.get(name)
        if parameter is None:
            raise AkqryError("invalid_parameter", "Unknown parameter.", {"parameter": name})
        supplied[name] = _typed_value(raw, parameter)
    try:
        signature.bind(**supplied)
    except TypeError as exc:
        code = "missing_parameter" if "missing a required" in str(exc) else "invalid_parameter"
        raise AkqryError(code, "Parameters do not match the selected function signature.", {"reason": str(exc)}) from exc
    return supplied


def _redacted_parameters(parameters: dict[str, Any]) -> dict[str, Any]:
    sensitive = ("token", "secret", "password", "cookie", "api_key", "apikey")
    return {key: "<redacted>" if any(item in key.lower() for item in sensitive) else value for key, value in parameters.items()}


def _temporary_path(destination: Path) -> Path:
    handle, name = tempfile.mkstemp(prefix=f".{destination.name}.", suffix=".akqry.tmp", dir=destination.parent)
    os.close(handle)
    return Path(name)


def _worker_fetch(spec: dict[str, Any], timeout: float, retries: int) -> dict[str, Any]:
    last_payload: dict[str, Any] | None = None
    for attempt in range(retries + 1):
        with tempfile.TemporaryDirectory(prefix="akqry-") as tempdir:
            root = Path(tempdir)
            request = root / "request.json"
            response = root / "response.json"
            request.write_text(json.dumps(spec, ensure_ascii=False, default=str), encoding="utf-8")
            try:
                process = subprocess.run(
                    [sys.executable, "-m", "akqry.worker", "--request", str(request), "--response", str(response)],
                    capture_output=True,
                    text=True,
                    timeout=timeout,
                    check=False,
                )
            except subprocess.TimeoutExpired:
                last_payload = {"ok": False, "error": {"code": "query_timeout", "message": "AkShare query exceeded the configured timeout.", "details": {"timeout_seconds": timeout}, "retryable": True}}
            else:
                if response.exists():
                    last_payload = json.loads(response.read_text(encoding="utf-8"))
                else:
                    last_payload = {"ok": False, "error": {"code": "upstream_error", "message": "Worker exited without a response.", "details": {"returncode": process.returncode}, "retryable": False}}
        if last_payload.get("ok") or not last_payload["error"].get("retryable"):
            break
        if attempt < retries:
            time.sleep(0.5 * (2**attempt))
    assert last_payload is not None
    last_payload["attempts"] = attempt + 1
    return last_payload


def _handle_fetch(args: argparse.Namespace) -> dict[str, Any]:
    if args.timeout <= 0 or args.retries < 0 or args.preview_rows < 0:
        raise AkqryError("usage_error", "Timeout and preview rows must be positive; retries cannot be negative.")
    module = load_akshare(args.akshare_path)
    catalog = discover(module, args.docs_root)
    record = catalog.get(args.function)
    if record is None:
        raise AkqryError("function_not_found", "No public AkShare callable with that name was found.", {"function": args.function})
    if not record["safe_to_fetch"]:
        raise AkqryError("unsafe_callable", "This callable is excluded from fetch.", {"function": args.function})
    parameters = _parse_parameters(args, getattr(module, args.function))
    destination = Path(args.output).expanduser().resolve() if args.output else None
    output_format = infer_format(str(destination) if destination else None, args.format)
    if destination and output_format is None:
        raise AkqryError("usage_error", "Output format must be explicit or inferred from .parquet, .jsonl, or .csv.")
    sidecar_destination = Path(f"{destination}.meta.json") if destination else None
    protected_paths = [destination] if destination else []
    if sidecar_destination and not args.no_sidecar:
        protected_paths.append(sidecar_destination)
    if any(path.exists() for path in protected_paths) and not args.overwrite:
        raise AkqryError(
            "output_exists",
            "Refusing to overwrite an existing output or provenance sidecar.",
            {"paths": [str(path) for path in protected_paths if path.exists()]},
        )
    if destination and not destination.parent.is_dir():
        raise AkqryError("write_failed", "Output parent directory does not exist.", {"path": str(destination.parent)})
    temporary_output = _temporary_path(destination) if destination else None
    started = time.monotonic()
    try:
        worker_result = _worker_fetch(
            {
                "function": args.function,
                "parameters": parameters,
                "akshare_path": args.akshare_path,
                "docs_root": args.docs_root,
                "require_columns": _split_columns(args.require_columns),
                "select": _split_columns(args.select) or None,
                "preview_rows": args.preview_rows,
                "allow_empty": args.allow_empty,
                "temporary_output": str(temporary_output) if temporary_output else None,
                "output_format": output_format,
                "debug": args.debug,
            },
            args.timeout,
            args.retries,
        )
        if not worker_result["ok"]:
            error = worker_result["error"]
            raise AkqryError(error["code"], error["message"], error.get("details", {}), error.get("retryable", False))
        if destination and temporary_output:
            os.replace(temporary_output, destination)
            worker_result["result"]["output"] = str(destination)
            worker_result["result"]["output_format"] = output_format
            worker_result["result"]["sha256"] = sha256(destination)
        provenance = worker_result["provenance"]
        provenance.update(
            {
                "function": args.function,
                "parameters": _redacted_parameters(parameters),
                "retrieved_at_utc": datetime.now(timezone.utc).isoformat(),
                "retrieved_at_local": datetime.now().astimezone().isoformat(),
                "duration_seconds": round(time.monotonic() - started, 3),
                "attempts": worker_result["attempts"],
                "source_url": record.get("source_url"),
            }
        )
        payload = {"ok": True, "command": "fetch", "result": worker_result["result"], "provenance": provenance}
        if destination and not args.no_sidecar:
            assert sidecar_destination is not None
            sidecar = sidecar_destination
            sidecar_temp = _temporary_path(sidecar)
            sidecar_temp.write_text(json.dumps(payload, ensure_ascii=False, indent=2, default=str), encoding="utf-8")
            os.replace(sidecar_temp, sidecar)
            payload["result"]["metadata"] = str(sidecar)
        return payload
    finally:
        if temporary_output and temporary_output.exists():
            temporary_output.unlink()


def _handle(args: argparse.Namespace) -> dict[str, Any]:
    if args.command == "version":
        return {"ok": True, "command": "version", "result": {"akqry_version": __version__}}
    if args.command == "doctor":
        module = load_akshare(args.akshare_path)
        provenance = runtime_provenance(module)
        docs_root = args.docs_root or str(Path(provenance["akshare_module_path"]).parent.parent / "docs")
        return {"ok": True, "command": "doctor", "result": {**provenance, "docs_root": docs_root, "docs_available": Path(docs_root).is_dir()}}
    module = load_akshare(args.akshare_path)
    catalog = discover(module, args.docs_root, getattr(args, "all", False))
    if args.command == "search":
        if args.limit <= 0:
            raise AkqryError("usage_error", "--limit must be positive.")
        return {"ok": True, "command": "search", "result": search(catalog, args.query, args.domain, args.limit), "provenance": runtime_provenance(module)}
    if args.command == "describe":
        return {"ok": True, "command": "describe", "result": describe(catalog, args.function), "provenance": runtime_provenance(module)}
    if args.command == "fetch":
        return _handle_fetch(args)
    raise AkqryError("usage_error", "Unknown command.")


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        payload = _handle(args)
        _emit(payload, bool(getattr(args, "json", False)))
        return 0
    except Exception as exc:
        payload = _error_payload(exc, bool(getattr(args, "debug", False)))
        _emit(payload, bool(getattr(args, "json", False)))
        return AkqryError(payload["error"]["code"], payload["error"]["message"]).exit_code
