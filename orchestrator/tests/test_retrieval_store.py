from __future__ import annotations

from pathlib import Path

import pytest

from app.retrieval.store import QuestionStore

ENTRIES_A = [
    {
        "sport": "nba",
        "question": "Who scored the most points last season?",
        "sql": "SELECT p.full_name FROM sports.players AS p LIMIT 1",
    },
    {
        "sport": "nba",
        "question": "How many rebounds did the team collect?",
        "sql": "SELECT SUM(s.stat_value) FROM sports.player_stats AS s LIMIT 1",
    },
    {
        "sport": "lol",
        "question": "Which player had the most kills?",
        "sql": "SELECT p.full_name FROM sports.players AS p LIMIT 1",
    },
]


def make_store(tmp_path: Path) -> QuestionStore:
    from app.retrieval.corpus import CorpusEntry

    store = QuestionStore(tmp_path / "chroma")
    store.ingest([CorpusEntry.model_validate(entry) for entry in ENTRIES_A])
    return store


def test_ingest_then_similar_returns_nearest(tmp_path: Path) -> None:
    store = make_store(tmp_path)
    entries = store.similar("Who scored the most points last season?", "nba", 3)
    assert entries
    assert entries[0].question == "Who scored the most points last season?"
    assert entries[0].sql.startswith("SELECT p.full_name")
    assert all(entry.sport == "nba" for entry in entries)


def test_sport_filter_prefers_matching_sport(tmp_path: Path) -> None:
    store = make_store(tmp_path)
    entries = store.similar("Which player had the most kills?", "lol", 3)
    assert entries[0].sport == "lol"


def test_fallback_when_sport_has_no_entries(tmp_path: Path) -> None:
    store = make_store(tmp_path)
    entries = store.similar("Who scored the most points last season?", "nfl", 3)
    assert entries
    assert len(entries) <= 3


def test_reingest_is_idempotent(tmp_path: Path) -> None:
    store = make_store(tmp_path)
    count_after_first = store.count()
    assert count_after_first == 3
    store.similar("anything at all?", "nba", 2)
    from app.retrieval.corpus import CorpusEntry

    again = int(store.ingest([CorpusEntry.model_validate(entry) for entry in ENTRIES_A]))
    assert again == 3
    assert store.count() == 3


def test_zero_k_returns_nothing(tmp_path: Path) -> None:
    store = make_store(tmp_path)
    assert store.similar("Who scored points?", "nba", 0) == []


def test_empty_store_similar_is_empty(tmp_path: Path) -> None:
    store = QuestionStore(tmp_path / "chroma-empty")
    assert store.similar("anything?", "nba", 3) == []


INVALID_ROWS = [
    {"sport": "", "question": "q", "sql": "SELECT 1"},
    {"sport": "nba", "question": "q", "sql": ""},
]


@pytest.mark.parametrize("bad", INVALID_ROWS)
def test_store_rejects_invalid_entries(tmp_path: Path, bad: dict) -> None:
    from app.retrieval.corpus import CorpusEntry

    with pytest.raises(ValueError):
        CorpusEntry.model_validate(bad)
