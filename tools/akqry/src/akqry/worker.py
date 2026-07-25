"""Subprocess worker used by ``akqry fetch`` to isolate network calls."""

from __future__ import annotations

import argparse
import inspect
import json
import traceback
from pathlib import Path
from typing import Any

from akqry.catalog import discover
from akqry.errors import AkqryError
from akqry.runtime import load_akshare, runtime_provenance
from akqry.serializers import (
    normalise_frame,
    preview,
    schema,
    schema_fingerprint,
    temporal_bounds,
    write_frame,
)


def _error_payload(error: Exception, debug: bool) -> dict[str, Any]:
    if isinstance(error, AkqryError):
        payload = error.as_dict()
    else:
        payload = {
            "code": "upstream_error",
            "message": "AkShare callable raised an exception.",
            "details": {"exception_type": type(error).__name__},
            "retryable": type(error).__name__
            in {"ConnectionError", "ConnectTimeout", "ReadTimeout", "Timeout"},
        }
    if debug:
        payload["traceback"] = traceback.format_exc()
    return {"ok": False, "error": payload}


def _check_parameters(function: object, parameters: dict[str, Any]) -> None:
    try:
        inspect.signature(function).bind(**parameters)
    except TypeError as exc:
        message = str(exc)
        code = "missing_parameter" if "missing a required" in message else "invalid_parameter"
        raise AkqryError(code, "Parameters do not match the selected function signature.", {"reason": message}) from exc


def execute(spec: dict[str, Any]) -> dict[str, Any]:
    module = load_akshare(spec.get("akshare_path"))
    catalog = discover(module, spec.get("docs_root"))
    name = spec["function"]
    record = catalog.get(name)
    if record is None:
        raise AkqryError("function_not_found", "No public AkShare callable with that name was found.", {"function": name})
    if not record["safe_to_fetch"]:
        raise AkqryError("unsafe_callable", "This callable is excluded from fetch.", {"function": name})
    function = getattr(module, name)
    parameters = spec["parameters"]
    _check_parameters(function, parameters)
    frame = function(**parameters)
    frame, frame_metadata = normalise_frame(frame)
    required = spec.get("require_columns", [])
    missing = [column for column in required if column not in frame.columns]
    if missing:
        raise AkqryError(
            "missing_required_columns",
            "Result does not include all required columns.",
            {"missing_columns": missing, "available_columns": list(frame.columns)},
        )
    if frame.empty and not spec.get("allow_empty", False):
        raise AkqryError("empty_result", "AkShare returned an empty DataFrame.")
    selected = spec.get("select")
    if selected:
        absent = [column for column in selected if column not in frame.columns]
        if absent:
            raise AkqryError(
                "missing_required_columns",
                "Selected columns are absent from the result.",
                {"missing_columns": absent, "available_columns": list(frame.columns)},
            )
        frame = frame.loc[:, selected]
    output_path = spec.get("temporary_output")
    if output_path:
        write_frame(frame, Path(output_path), spec["output_format"])
    return {
        "ok": True,
        "result": {
            "kind": "dataframe",
            "rows": int(len(frame)),
            "columns": schema(frame),
            "schema_fingerprint": schema_fingerprint(frame),
            "preview": preview(frame, int(spec.get("preview_rows", 10))),
            "temporal_bounds": temporal_bounds(frame),
            "empty": bool(frame.empty),
            "selected_columns": selected,
            **frame_metadata,
        },
        "provenance": runtime_provenance(module),
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--request", required=True)
    parser.add_argument("--response", required=True)
    args = parser.parse_args(argv)
    request_path = Path(args.request)
    response_path = Path(args.response)
    spec = json.loads(request_path.read_text(encoding="utf-8"))
    try:
        payload = execute(spec)
    except Exception as exc:  # Worker must always leave a machine-readable response.
        payload = _error_payload(exc, bool(spec.get("debug")))
    response_path.write_text(json.dumps(payload, ensure_ascii=False, default=str), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
