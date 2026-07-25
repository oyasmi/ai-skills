"""Subprocess worker used by ``akqry fetch`` to isolate network calls.

One worker run handles a list of calls so that a multi-symbol query pays the
AkShare import once. The response file is rewritten after every call, which lets
the parent keep whatever finished before an overall timeout.

Each call also carries its own deadline. Without one, a single unresponsive
symbol would spend the whole batch's budget and every symbol queued behind it
would be reported as timed out despite never having been attempted.
"""

from __future__ import annotations

import argparse
import contextlib
import inspect
import json
import signal
import time
import traceback
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Iterator

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

# Transient failures worth one more attempt. AkShare reaches upstream through
# requests and curl_cffi, and a throttled endpoint most often shows up as a
# truncated body or unparseable JSON rather than a clean connection error.
RETRYABLE_EXCEPTIONS = frozenset(
    {
        "ChunkedEncodingError",
        "ConnectionError",
        "ConnectionResetError",
        "ConnectTimeout",
        "CurlError",
        "IncompleteRead",
        "JSONDecodeError",
        "ProtocolError",
        "ProxyError",
        "ReadTimeout",
        "RemoteDisconnected",
        "RequestsError",
        "SSLError",
        "Timeout",
        "TooManyRedirects",
    }
)
MESSAGE_LIMIT = 400
SENSITIVE_MARKERS = ("token", "secret", "password", "cookie", "api_key", "apikey", "authorization")
DEADLINES_SUPPORTED = hasattr(signal, "SIGALRM") and hasattr(signal, "setitimer")


class CallTimeout(Exception):
    """One call exceeded its own share of the batch budget."""


@contextlib.contextmanager
def deadline(seconds: float) -> Iterator[None]:
    """Interrupt the wrapped call once its budget is spent, where the OS allows it."""
    if not DEADLINES_SUPPORTED or seconds <= 0:
        yield
        return

    def expire(signum: int, frame: Any) -> None:
        raise CallTimeout(f"Call exceeded its {seconds:g}s budget.")

    previous = signal.signal(signal.SIGALRM, expire)
    signal.setitimer(signal.ITIMER_REAL, seconds)
    try:
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)


def _safe_message(error: Exception) -> str | None:
    """Surface the upstream message, truncated, unless it looks like it holds a secret."""
    text = " ".join(str(error).split())
    if not text:
        return None
    if any(marker in text.lower() for marker in SENSITIVE_MARKERS):
        return "<redacted>"
    return text[:MESSAGE_LIMIT] + ("…" if len(text) > MESSAGE_LIMIT else "")


def _error_payload(error: Exception, debug: bool) -> dict[str, Any]:
    if isinstance(error, CallTimeout):
        payload = {
            "code": "query_timeout",
            "message": "AkShare call exceeded its per-call timeout.",
            "details": {"hint": "Narrow the date range, or raise --timeout for this interface."},
            # Retrying inside the batch would spend another call's budget.
            "retryable": False,
        }
    elif isinstance(error, AkqryError):
        payload = error.as_dict()
    else:
        name = type(error).__name__
        payload = {
            "code": "upstream_error",
            "message": "AkShare callable raised an exception.",
            "details": {"exception_type": name, "exception_message": _safe_message(error)},
            "retryable": name in RETRYABLE_EXCEPTIONS,
        }
    if debug:
        payload["traceback"] = traceback.format_exc()
    return payload


def _check_parameters(function: Callable[..., Any], parameters: dict[str, Any]) -> None:
    try:
        inspect.signature(function).bind(**parameters)
    except TypeError as exc:
        message = str(exc)
        code = "missing_parameter" if "missing a required" in message else "invalid_parameter"
        raise AkqryError(code, "Parameters do not match the selected function signature.", {"reason": message}) from exc


def _run_call(function: Callable[..., Any], call: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
    parameters = call["parameters"]
    _check_parameters(function, parameters)
    frame = function(**parameters)
    frame, frame_metadata = normalise_frame(frame)
    required = spec.get("require_columns") or []
    missing = [column for column in required if column not in frame.columns]
    if missing:
        raise AkqryError(
            "missing_required_columns",
            "Result does not include all required columns.",
            {"missing_columns": missing, "available_columns": list(frame.columns)},
        )
    if frame.empty and not spec.get("allow_empty", False):
        raise AkqryError(
            "empty_result",
            "AkShare returned an empty DataFrame.",
            {"hint": "Check the code format, the date range and whether the market was open."},
        )
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
    output_path = call.get("temporary_output")
    if output_path:
        write_frame(frame, Path(output_path), spec["output_format"])
    return {
        "kind": "dataframe",
        "rows": int(len(frame)),
        "columns": schema(frame),
        "schema_fingerprint": schema_fingerprint(frame),
        "preview": preview(frame, int(spec.get("preview_rows", 10))),
        "temporal_bounds": temporal_bounds(frame),
        "empty": bool(frame.empty),
        "selected_columns": selected,
        **frame_metadata,
    }


def _attempt_call(
    function: Callable[..., Any],
    call: dict[str, Any],
    spec: dict[str, Any],
    retries: int,
    call_timeout: float,
    debug: bool,
) -> dict[str, Any]:
    """Run one call with its retries, all of them inside a single budget."""
    attempts = 0
    try:
        # The budget covers the retries too, so a batch of n calls can never
        # outlive n budgets however many times a flaky endpoint is retried.
        with deadline(call_timeout):
            for attempt in range(retries + 1):
                attempts = attempt + 1
                try:
                    return {"ok": True, "attempts": attempts, "result": _run_call(function, call, spec)}
                except CallTimeout:
                    raise
                except Exception as exc:  # Isolate one call's failure from the rest of the batch.
                    error = _error_payload(exc, debug)
                    if not error.get("retryable") or attempt >= retries:
                        return {"ok": False, "attempts": attempts, "error": error}
                    time.sleep(0.5 * (2**attempt))
    except CallTimeout as exc:
        return {"ok": False, "attempts": attempts, "error": _error_payload(exc, debug)}
    raise AssertionError("unreachable: the retry loop always returns or raises")


def execute(spec: dict[str, Any], progress: Callable[[dict[str, Any]], None] | None = None) -> dict[str, Any]:
    module = load_akshare(spec.get("akshare_path"))
    catalog = discover(module, spec.get("docs_root"), include_unsafe=True)
    name = spec["function"]
    record = catalog.get(name)
    if record is None:
        raise AkqryError(
            "function_not_found",
            "No public AkShare callable with that name was found.",
            {"function": name, "hint": "Use `akqry search` to find the current interface name."},
        )
    if not record["safe_to_fetch"]:
        raise AkqryError(
            "unsafe_callable",
            "This callable is excluded from fetch.",
            {"function": name, "excluded_reason": record.get("excluded_reason")},
        )
    function = getattr(module, name)
    retries = max(int(spec.get("retries", 0)), 0)
    call_timeout = float(spec.get("call_timeout") or 0.0)
    delay = max(float(spec.get("delay") or 0.0), 0.0)
    debug = bool(spec.get("debug"))
    payload: dict[str, Any] = {"ok": True, "provenance": runtime_provenance(module), "items": []}
    for position, call in enumerate(spec["calls"]):
        if position and delay:
            # Upstream sites throttle a burst far more readily than a trickle.
            time.sleep(delay)
        started = time.monotonic()
        item: dict[str, Any] = {
            "index": call["index"],
            "label": call.get("label"),
            "started_at_utc": datetime.now(timezone.utc).isoformat(),
        }
        item.update(_attempt_call(function, call, spec, retries, call_timeout, debug))
        item["finished_at_utc"] = datetime.now(timezone.utc).isoformat()
        item["duration_seconds"] = round(time.monotonic() - started, 3)
        payload["items"].append(item)
        if progress is not None:
            progress(payload)
    return payload


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--request", required=True)
    parser.add_argument("--response", required=True)
    args = parser.parse_args(argv)
    response_path = Path(args.response)
    spec = json.loads(Path(args.request).read_text(encoding="utf-8"))

    def write(payload: dict[str, Any]) -> None:
        response_path.write_text(json.dumps(payload, ensure_ascii=False, default=str), encoding="utf-8")

    try:
        write(execute(spec, progress=write))
    except Exception as exc:  # Worker must always leave a machine-readable response.
        write({"ok": False, "error": _error_payload(exc, bool(spec.get("debug")))})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
