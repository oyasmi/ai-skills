from __future__ import annotations

import re
from pathlib import Path

import pytest

from akqry.errors import EXIT_CODES

SKILL = Path(__file__).resolve().parents[3] / "skills" / "query-akshare"
REFERENCES = (
    "a-shares.md",
    "hk-shares.md",
    "boards.md",
    "funds-etfs.md",
    "indices.md",
    "analysis.md",
    "data-integrity.md",
)


@pytest.fixture(scope="module")
def skill_text() -> str:
    return (SKILL / "SKILL.md").read_text(encoding="utf-8")


def test_skill_has_required_frontmatter_and_references(skill_text: str) -> None:
    assert skill_text.startswith("---\nname: query-akshare\ndescription: ")
    assert skill_text.count("---") >= 2
    assert (SKILL / "agents" / "openai.yaml").is_file()
    for name in REFERENCES:
        assert (SKILL / "references" / name).is_file()


def test_every_reference_link_resolves(skill_text: str) -> None:
    for path in re.findall(r"\]\((references/[^)]+)\)", skill_text):
        assert (SKILL / path).is_file(), path
    for reference in REFERENCES:
        text = (SKILL / "references" / reference).read_text(encoding="utf-8")
        for path in re.findall(r"\]\(([A-Za-z0-9._-]+\.md)\)", text):
            assert (SKILL / "references" / path).is_file(), f"{reference} -> {path}"


def test_skill_documents_a_recovery_step_for_every_recoverable_error(skill_text: str) -> None:
    """An agent that cannot map a code to a next step will guess instead."""
    internal_only = {"internal_error", "upstream_network_error", "duplicate_columns", "unsupported_result_type"}
    for code in set(EXIT_CODES) - internal_only:
        assert f"`{code}`" in skill_text, f"{code} has no documented recovery step"


def test_skill_installs_the_tool_it_depends_on(skill_text: str) -> None:
    assert "uv tool install" in skill_text
    assert "akqry doctor" in skill_text
