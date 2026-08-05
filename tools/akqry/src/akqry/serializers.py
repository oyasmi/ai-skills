"""Stable previews, raw-data serialization, and file hashes."""

from __future__ import annotations

import csv
import hashlib
import importlib.util
import json
import math
from datetime import date, datetime
from pathlib import Path
from typing import Any

import numpy as np
import pandas as pd

from akqry.errors import AkqryError


def json_value(value: Any) -> Any:
    if value is None or isinstance(value, (str, int, bool)):
        return value
    if value is pd.NA:
        return None
    if isinstance(value, float):
        return value if math.isfinite(value) else None
    if isinstance(value, (datetime, date, pd.Timestamp)):
        return value.isoformat()
    if isinstance(value, dict):
        return {str(key): json_value(item) for key, item in value.items()}
    if isinstance(value, (list, tuple, set)):
        return [json_value(item) for item in value]
    missing = pd.isna(value)
    if isinstance(missing, (bool, np.bool_)) and bool(missing):
        return None
    if hasattr(value, "item"):
        try:
            return json_value(value.item())
        except (TypeError, ValueError):
            pass
    return str(value)


def normalise_frame(frame: pd.DataFrame) -> tuple[pd.DataFrame, dict[str, Any]]:
    if not isinstance(frame, pd.DataFrame):
        raise AkqryError("unsupported_result_type", "AkShare callable did not return a pandas.DataFrame.")
    metadata: dict[str, Any] = {"index_reset": False}
    if not isinstance(frame.index, pd.RangeIndex) or frame.index.start != 0 or frame.index.step != 1:
        frame = frame.reset_index()
        metadata["index_reset"] = True
    column_names = [str(column) for column in frame.columns]
    if len(set(column_names)) != len(column_names):
        raise AkqryError("duplicate_columns", "Result contains duplicate column names.", {"columns": column_names})
    frame = frame.copy()
    frame.columns = column_names
    return frame, metadata


def schema(frame: pd.DataFrame) -> list[dict[str, str]]:
    return [{"name": str(name), "dtype": str(dtype)} for name, dtype in frame.dtypes.items()]


def schema_fingerprint(frame: pd.DataFrame) -> str:
    payload = json.dumps(schema(frame), ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def preview(frame: pd.DataFrame, rows: int) -> list[dict[str, Any]]:
    return [
        {str(key): json_value(value) for key, value in row.items()}
        for row in frame.head(rows).to_dict(orient="records")
    ]


def _parse_temporal(values: pd.Series) -> tuple[pd.Series, str]:
    """Parse common market date encodings without treating YYYYMMDD as nanos."""
    text = values.astype("string").str.strip()
    parsed = pd.Series(pd.NaT, index=values.index, dtype="datetime64[ns]")
    formats: list[str] = []

    yyyymmdd = text.str.fullmatch(r"\d{8}").fillna(False)
    if yyyymmdd.any():
        parsed.loc[yyyymmdd] = pd.to_datetime(text.loc[yyyymmdd], format="%Y%m%d", errors="coerce")
        formats.append("yyyymmdd")

    remaining = parsed.isna() & text.notna()
    numeric = pd.to_numeric(text.where(remaining), errors="coerce")
    epoch_ms = remaining & numeric.notna() & numeric.abs().ge(10**11)
    epoch_seconds = remaining & numeric.notna() & numeric.abs().ge(10**9) & ~epoch_ms
    if epoch_ms.any():
        parsed.loc[epoch_ms] = pd.to_datetime(numeric.loc[epoch_ms], unit="ms", errors="coerce")
        formats.append("unix_ms")
    if epoch_seconds.any():
        parsed.loc[epoch_seconds] = pd.to_datetime(numeric.loc[epoch_seconds], unit="s", errors="coerce")
        formats.append("unix_s")

    remaining = parsed.isna() & text.notna()
    if remaining.any():
        try:
            parsed.loc[remaining] = pd.to_datetime(text.loc[remaining], errors="coerce", format="mixed")
        except (TypeError, ValueError):
            parsed.loc[remaining] = pd.to_datetime(text.loc[remaining], errors="coerce")
        formats.append("datetime")
    return parsed, "+".join(dict.fromkeys(formats)) or "unknown"


def temporal_bounds(frame: pd.DataFrame) -> list[dict[str, Any]]:
    """Report clearly inferred date bounds without mutating the source table."""
    bounds: list[dict[str, Any]] = []
    for column in frame.columns:
        name = str(column)
        if not any(token in name.lower() for token in ("date", "time", "日期", "时间")):
            continue
        parsed, parse_format = _parse_temporal(frame[column])
        valid = parsed.dropna()
        if not len(valid) or len(valid) / max(len(frame), 1) < 0.8:
            continue
        bounds.append(
            {
                "column": name,
                "minimum": json_value(valid.min()),
                "maximum": json_value(valid.max()),
                "parsed_rows": int(len(valid)),
                "inferred": True,
                "parse_format": parse_format,
            }
        )
    return bounds


def quality_report(
    frame: pd.DataFrame,
    date_column: str | None = None,
    key_columns: list[str] | None = None,
) -> dict[str, Any]:
    """Describe common table-quality hazards without changing the returned data."""
    rows, columns = len(frame), len(frame.columns)
    null_cells = int(frame.isna().sum().sum()) if rows and columns else 0
    warnings: list[str] = []
    errors: list[str] = []
    date_info: dict[str, Any] | None = None
    key_columns = key_columns or []

    if null_cells:
        warnings.append(f"{null_cells} null cell(s) are present in the returned table.")

    numeric = frame.select_dtypes(include=["number"])
    if numeric.empty:
        nonfinite = 0
    else:
        try:
            numeric_values = numeric.to_numpy(dtype=float, na_value=np.nan)
        except TypeError:
            numeric_values = numeric.astype(float).to_numpy()
        # Missing numeric cells are reported by null_cells; only infinities are
        # a non-finite numeric value that can silently poison calculations.
        nonfinite = int(np.isinf(numeric_values).sum())
    if nonfinite:
        errors.append(f"{nonfinite} non-finite numeric value(s) are present.")

    if date_column:
        if date_column not in frame.columns:
            errors.append(f"Date column {date_column!r} is absent from the returned table.")
        else:
            parsed, parse_format = _parse_temporal(frame[date_column])
            valid = parsed.notna()
            invalid = int(frame[date_column].notna().sum() - valid.sum())
            duplicate_dates = int(parsed[valid].duplicated().sum())
            date_info = {
                "column": date_column,
                "parsed_rows": int(valid.sum()),
                "missing_rows": int(frame[date_column].isna().sum()),
                "invalid_rows": invalid,
                "duplicate_values": duplicate_dates,
                "monotonic_increasing": bool(parsed[valid].is_monotonic_increasing),
                "parse_format": parse_format,
            }
            if invalid:
                errors.append(f"{invalid} non-null value(s) in {date_column!r} could not be parsed as dates.")
            if duplicate_dates:
                message = f"{duplicate_dates} duplicate date value(s) are present in {date_column!r}."
                (warnings if date_column in key_columns else errors).append(message)
            if len(parsed[valid]) > 1 and not parsed[valid].is_monotonic_increasing:
                warnings.append(f"Date column {date_column!r} is not sorted ascending.")

    missing_keys = [column for column in key_columns if column not in frame.columns]
    if missing_keys:
        errors.append("Key column(s) are absent: " + ", ".join(missing_keys) + ".")
    duplicate_keys = int(frame.duplicated(subset=key_columns, keep=False).sum()) if key_columns and not missing_keys else 0
    if duplicate_keys:
        errors.append(f"{duplicate_keys} row(s) belong to duplicate key groups.")

    return {
        "rows": int(rows),
        "columns": int(columns),
        "null_cells": null_cells,
        "null_ratio": round(null_cells / max(rows * columns, 1), 6),
        "nonfinite_numeric_values": nonfinite,
        "date": date_info,
        "key_columns": key_columns,
        "duplicate_key_rows": duplicate_keys,
        "warnings": warnings,
        "errors": errors,
    }


def write_frame(frame: pd.DataFrame, path: Path, output_format: str) -> None:
    try:
        if output_format == "csv":
            frame.to_csv(path, index=False, encoding="utf-8", quoting=csv.QUOTE_MINIMAL)
        elif output_format == "jsonl":
            frame.to_json(path, orient="records", lines=True, force_ascii=False, date_format="iso")
        elif output_format == "parquet":
            frame.to_parquet(path, index=False)
        else:
            raise AkqryError("serialization_failed", "Unsupported output format.", {"format": output_format})
    except AkqryError:
        raise
    except Exception as exc:
        raise AkqryError(
            "serialization_failed",
            "Unable to serialize the DataFrame.",
            {"format": output_format, "exception_type": type(exc).__name__},
        ) from exc


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


FORMAT_ENGINES: dict[str, tuple[tuple[str, ...], str]] = {
    "parquet": (("pyarrow", "fastparquet"), "Install the extra with `uv tool install --editable 'tools/akqry[parquet]'`, or write .jsonl instead.")
}


def ensure_format_available(output_format: str | None) -> None:
    """Fail before the network call when the requested artifact cannot be written."""
    requirement = FORMAT_ENGINES.get(output_format or "")
    if requirement is None:
        return
    engines, remedy = requirement
    if any(importlib.util.find_spec(engine) is not None for engine in engines):
        return
    raise AkqryError(
        "dependency_missing",
        "The requested output format needs an engine that is not installed.",
        {"format": output_format, "missing_any_of": list(engines), "remedy": remedy},
    )


def infer_format(path: str | None, supplied: str | None) -> str | None:
    if supplied:
        return supplied
    if not path:
        return None
    suffix = Path(path).suffix.lower()
    return {".csv": "csv", ".jsonl": "jsonl", ".ndjson": "jsonl", ".parquet": "parquet"}.get(suffix)
