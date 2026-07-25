from __future__ import annotations

import argparse
import json
import sys

import pytest

from akqry.cli import _handle_fetch
from akqry.errors import AkqryError


def _fake_akshare(tmp_path) -> str:
    package = tmp_path / "fake-akshare" / "akshare"
    package.mkdir(parents=True)
    (package / "data.py").write_text(
        "import pandas as pd\n"
        "def stock_demo(symbol: str, count: int = 1) -> pd.DataFrame:\n"
        "    if symbol == 'EMPTY':\n"
        "        return pd.DataFrame(columns=['代码', '收盘'])\n"
        "    return pd.DataFrame({'代码': [symbol] * count, '收盘': [10.5] * count})\n",
        encoding="utf-8",
    )
    (package / "__init__.py").write_text(
        "__version__ = 'test'\nfrom .data import stock_demo\n", encoding="utf-8"
    )
    return str(package.parent)


def _args(root: str, output: str | None = None) -> argparse.Namespace:
    return argparse.Namespace(
        function="stock_demo",
        arg=["symbol=000001", "count=2"],
        params_json=None,
        params_file=None,
        require_columns="代码,收盘",
        select="",
        preview_rows=3,
        output=output,
        format=None,
        allow_empty=False,
        overwrite=False,
        no_sidecar=False,
        timeout=30.0,
        retries=0,
        akshare_path=root,
        docs_root=None,
        debug=False,
    )


@pytest.fixture(autouse=True)
def clear_akshare_modules():
    yield
    for key in list(sys.modules):
        if key == "akshare" or key.startswith("akshare."):
            sys.modules.pop(key)


def test_fetch_writes_jsonl_and_provenance(tmp_path) -> None:
    root = _fake_akshare(tmp_path)
    output = tmp_path / "result.jsonl"
    payload = _handle_fetch(_args(root, str(output)))
    assert payload["ok"] is True
    assert output.exists()
    assert (tmp_path / "result.jsonl.meta.json").exists()
    assert json.loads(output.read_text(encoding="utf-8").splitlines()[0])["代码"] == "000001"
    assert payload["result"]["rows"] == 2
    assert payload["provenance"]["parameters"]["symbol"] == "000001"


def test_fetch_refuses_empty_result(tmp_path) -> None:
    root = _fake_akshare(tmp_path)
    args = _args(root)
    args.arg = ["symbol=EMPTY"]
    with pytest.raises(AkqryError) as raised:
        _handle_fetch(args)
    assert raised.value.code == "empty_result"


def test_fetch_refuses_orphaned_sidecar(tmp_path) -> None:
    root = _fake_akshare(tmp_path)
    output = tmp_path / "result.jsonl"
    (tmp_path / "result.jsonl.meta.json").write_text("{}", encoding="utf-8")
    with pytest.raises(AkqryError) as raised:
        _handle_fetch(_args(root, str(output)))
    assert raised.value.code == "output_exists"
