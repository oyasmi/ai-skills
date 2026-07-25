from __future__ import annotations

from akqry.runtime import is_safe_callable


def test_safe_callable_excludes_token_functions() -> None:
    def get_token() -> str:
        return "secret"

    get_token.__module__ = "akshare.utils.token_process"
    assert is_safe_callable("get_token", get_token) == (False, "state_or_secret_callable")


def test_safe_callable_accepts_data_function() -> None:
    def stock_zh_a_hist() -> object:
        return None

    stock_zh_a_hist.__module__ = "akshare.stock_feature.stock_hist_em"
    assert is_safe_callable("stock_zh_a_hist", stock_zh_a_hist) == (True, None)
