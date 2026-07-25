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


# AkShare namespaces its interfaces by market, and the skill only ever names one
# of these families. Anything else in the prose is not an interface reference.
INTERFACE_FAMILIES = ("stock", "fund", "index", "bond", "futures", "macro", "news", "option", "currency", "spot")
_INTERFACE = re.compile(r"\b[a-z][a-z0-9]*(?:_[a-z0-9]+){2,}\b")


def test_every_interface_the_skill_names_still_exists() -> None:
    """A renamed interface turns advice into a `function_not_found` loop.

    The skill tells the agent to trust `describe` over its own memory; the same
    rule has to bind the skill, whose interface names age with every AkShare
    release.
    """
    catalog = pytest.importorskip("akqry.catalog").discover(pytest.importorskip("akshare"))
    missing: dict[str, list[str]] = {}
    for path in sorted(SKILL.rglob("*.md")):
        named = {
            name
            for name in _INTERFACE.findall(path.read_text(encoding="utf-8"))
            if name.split("_")[0] in INTERFACE_FAMILIES
        }
        absent = sorted(name for name in named if name not in catalog)
        if absent:
            missing[path.name] = absent
    assert not missing, f"interfaces named by the skill are gone from AkShare: {missing}"
