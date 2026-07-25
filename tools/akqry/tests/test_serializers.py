from __future__ import annotations

import pandas as pd
import pytest

from akqry.errors import AkqryError
from akqry.serializers import normalise_frame, preview, schema_fingerprint, temporal_bounds


def test_normalise_frame_preserves_leading_zero_string() -> None:
    frame = pd.DataFrame({"代码": ["000001"], "价格": [10.5]})
    result, metadata = normalise_frame(frame)
    assert result.loc[0, "代码"] == "000001"
    assert metadata["index_reset"] is False
    assert preview(result, 1) == [{"代码": "000001", "价格": 10.5}]


def test_normalise_frame_resets_meaningful_index() -> None:
    frame = pd.DataFrame({"收盘": [10.0]}, index=pd.Index(["2025-01-01"], name="日期"))
    result, metadata = normalise_frame(frame)
    assert list(result.columns) == ["日期", "收盘"]
    assert metadata["index_reset"] is True


def test_duplicate_columns_fail() -> None:
    frame = pd.DataFrame([[1, 2]], columns=["代码", "代码"])
    with pytest.raises(AkqryError, match="duplicate"):
        normalise_frame(frame)


def test_schema_fingerprint_is_stable() -> None:
    frame = pd.DataFrame({"代码": ["000001"], "收盘": [1.0]})
    assert schema_fingerprint(frame) == schema_fingerprint(frame.copy())


def test_temporal_bounds_are_inferred_without_mutation() -> None:
    frame = pd.DataFrame({"日期": ["2025-01-01", "2025-01-02"], "代码": ["000001", "000001"]})
    assert temporal_bounds(frame) == [
        {"column": "日期", "minimum": "2025-01-01T00:00:00", "maximum": "2025-01-02T00:00:00", "parsed_rows": 2, "inferred": True}
    ]
    assert frame.loc[0, "日期"] == "2025-01-01"
