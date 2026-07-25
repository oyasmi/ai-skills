"""Stable errors emitted by the CLI and worker."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

EXIT_CODES = {
    "internal_error": 1,
    "usage_error": 2,
    "dependency_missing": 3,
    "akshare_import_failed": 3,
    "function_not_found": 4,
    "unsafe_callable": 4,
    "invalid_parameter": 4,
    "missing_parameter": 4,
    "query_timeout": 5,
    "upstream_network_error": 5,
    "upstream_error": 5,
    "empty_result": 6,
    "missing_required_columns": 6,
    "duplicate_columns": 6,
    "unsupported_result_type": 6,
    "partial_failure": 6,
    "output_exists": 7,
    "serialization_failed": 7,
    "write_failed": 7,
}


@dataclass
class AkqryError(Exception):
    code: str
    message: str
    details: dict[str, Any] = field(default_factory=dict)
    retryable: bool = False

    @property
    def exit_code(self) -> int:
        return EXIT_CODES.get(self.code, 1)

    def as_dict(self) -> dict[str, Any]:
        return {
            "code": self.code,
            "message": self.message,
            "details": self.details,
            "retryable": self.retryable,
        }
