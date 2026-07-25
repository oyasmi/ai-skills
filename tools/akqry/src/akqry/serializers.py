"""Stable previews, raw-data serialization, and file hashes."""

from __future__ import annotations

import csv
import hashlib
import json
import math
from datetime import date, datetime
from pathlib import Path
from typing import Any

import pandas as pd

from akqry.errors import AkqryError


def json_value(value: Any) -> Any:
    if value is None or isinstance(value, (str, int, bool)):
        return value
    if isinstance(value, float):
        return value if math.isfinite(value) else None
    if isinstance(value, (datetime, date, pd.Timestamp)):
        return value.isoformat()
    if pd.isna(value):
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


def temporal_bounds(frame: pd.DataFrame) -> list[dict[str, Any]]:
    """Report clearly inferred date bounds without mutating the source table."""
    bounds: list[dict[str, Any]] = []
    for column in frame.columns:
        name = str(column)
        if not any(token in name.lower() for token in ("date", "time", "日期", "时间")):
            continue
        parsed = pd.to_datetime(frame[column], errors="coerce")
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
            }
        )
    return bounds


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


def infer_format(path: str | None, supplied: str | None) -> str | None:
    if supplied:
        return supplied
    if not path:
        return None
    suffix = Path(path).suffix.lower()
    return {".csv": "csv", ".jsonl": "jsonl", ".ndjson": "jsonl", ".parquet": "parquet"}.get(suffix)
