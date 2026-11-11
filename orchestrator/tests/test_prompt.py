from __future__ import annotations

from pathlib import Path

import pytest

from app.retrieval.corpus import CorpusEntry
from app.schema_ctx.manifest import Manifest, load_manifest
from app.schema_ctx.prompt import build_messages, build_schema_slice

REPO_ROOT = Path(__file__).resolve().parents[2]
REAL_MANIFEST = REPO_ROOT / "data" / "schema" / "manifest.yaml"


@pytest.fixture(scope="module")
def manifest() -> Manifest:
    return load_manifest(REAL_MANIFEST)


def test_slice_contains_every_whitelisted_table(manifest: Manifest) -> None:
    slice_text = build_schema_slice(manifest)

    for table_name in (
        "sports.leagues",
        "sports.teams",
        "sports.games",
        "sports.players",
        "sports.player_stats",
    ):
        assert table_name in slice_text
    assert "joins:" in slice_text
    for relation in manifest.relations:
        assert relation in slice_text


def test_slice_never_leaks_internal_schemas(manifest: Manifest) -> None:
    slice_text = build_schema_slice(manifest)

    assert "ingest." not in slice_text
    assert "migrator" not in slice_text
    assert "watermarks" not in slice_text
    assert "schema_migrations" not in slice_text


def test_messages_carry_question_and_sport(manifest: Manifest) -> None:
    messages = build_messages("who scored the most", "basketball", manifest)

    assert messages[0]["role"] == "system"
    assert messages[1]["role"] == "user"
    assert "who scored the most" in messages[1]["content"]
    assert "basketball" in messages[1]["content"]
    assert "sports.player_stats" in messages[0]["content"]
    assert "SELECT only" in messages[0]["content"]


def test_exemplars_sandwich_between_system_and_question(manifest: Manifest) -> None:
    exemplars = [
        CorpusEntry(
            sport="nba",
            question="Who scored most?",
            sql="SELECT p.full_name FROM sports.players AS p",
        ),
        CorpusEntry(
            sport="lol",
            question="Most kills?",
            sql="SELECT s.stat_value FROM sports.player_stats AS s",
        ),
    ]

    messages = build_messages("top rebounders", "basketball", manifest, exemplars)

    roles = [message["role"] for message in messages]
    assert roles == ["system", "user", "assistant", "user", "assistant", "user"]
    assert messages[-1]["content"] == "sport: basketball\nquestion: top rebounders"
    assert messages[1]["content"] == "sport: nba\nquestion: Who scored most?"
    assert messages[2]["content"] == exemplars[0].sql
    assert messages[3]["content"] == "sport: lol\nquestion: Most kills?"
    assert messages[4]["content"] == exemplars[1].sql


def test_no_exemplars_keeps_original_shape(manifest: Manifest) -> None:
    messages = build_messages("whatever", "esports", manifest)
    assert len(messages) == 2
