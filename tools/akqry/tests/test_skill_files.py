from __future__ import annotations

from pathlib import Path


def test_query_akshare_skill_has_required_frontmatter_and_references() -> None:
    repository = Path(__file__).resolve().parents[3]
    skill = repository / "skills" / "query-akshare"
    content = (skill / "SKILL.md").read_text(encoding="utf-8")
    assert content.startswith("---\nname: query-akshare\ndescription: ")
    assert content.count("---") >= 2
    assert (skill / "agents" / "openai.yaml").is_file()
    for name in ("a-shares.md", "hk-shares.md", "boards.md", "funds-etfs.md", "analysis.md", "data-integrity.md"):
        assert (skill / "references" / name).is_file()
