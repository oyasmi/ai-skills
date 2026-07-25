"""Load a selected AkShare installation without importing it at CLI startup."""

from __future__ import annotations

import importlib
import os
import sys
from pathlib import Path
from types import ModuleType

from akqry.errors import AkqryError

EXCLUDED_MODULE_PREFIXES = (
    "akshare.utils",
    "akshare.pro",
)
EXCLUDED_NAMES = {"set_token", "get_token", "set_proxies", "get_proxies", "pro_api"}
UNSAFE_PREFIXES = ("set_", "save_", "write_", "delete_", "remove_", "upload_", "login", "logout")


def configured_akshare_path(value: str | None) -> str | None:
    return value or os.environ.get("AKQRY_AKSHARE_PATH")


def load_akshare(akshare_path: str | None = None) -> ModuleType:
    selected = configured_akshare_path(akshare_path)
    if selected:
        root = Path(selected).expanduser().resolve()
        if not (root / "akshare" / "__init__.py").is_file():
            raise AkqryError(
                "akshare_import_failed",
                "--akshare-path must contain akshare/__init__.py",
                {"path": str(root)},
            )
        root_text = str(root)
        if root_text not in sys.path:
            sys.path.insert(0, root_text)
    try:
        return importlib.import_module("akshare")
    except ModuleNotFoundError as exc:
        raise AkqryError(
            "dependency_missing",
            "Unable to import AkShare or one of its dependencies.",
            {"missing_module": exc.name, "akshare_path": selected},
        ) from exc
    except Exception as exc:  # Import-time optional dependencies can fail in many ways.
        raise AkqryError(
            "akshare_import_failed",
            "AkShare failed while importing.",
            {"exception_type": type(exc).__name__, "akshare_path": selected},
        ) from exc


def is_safe_callable(name: str, value: object) -> tuple[bool, str | None]:
    if not callable(value) or name.startswith("_"):
        return False, "not_a_public_callable"
    if name in EXCLUDED_NAMES or name.startswith(UNSAFE_PREFIXES):
        return False, "state_or_secret_callable"
    module_name = getattr(value, "__module__", "")
    if not module_name.startswith("akshare."):
        return False, "not_defined_by_akshare"
    if module_name.startswith(EXCLUDED_MODULE_PREFIXES):
        return False, "internal_or_pro_callable"
    return True, None


def runtime_provenance(module: ModuleType) -> dict[str, str | None]:
    return {
        "akshare_version": getattr(module, "__version__", None),
        "akshare_module_path": str(Path(module.__file__).resolve()) if module.__file__ else None,
        "python_executable": sys.executable,
        "python_version": sys.version.split()[0],
    }
