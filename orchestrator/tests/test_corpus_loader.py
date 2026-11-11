from __future__ import annotations

import json
from pathlib import Path

import pytest

from app.retrieval.corpus import CorpusEntry, load_corpus

REPO_ROOT = Path(__file__).resolve().parents[2]


def test_loads_shipped_corpus() -> None:
    entries = load_corpus(REPO_ROOT / "data" / "corpus" / "questions.jsonl")
    assert len(entries) >= 25
    sports = {entry.sport for entry in entries}
    assert {"nba", "lol", "synthetic", "all"} <= sports
    assert len({entry.question for entry in entries}) == len(entries)


def test_empty_file_rejected(tmp_path: Path) -> None:
    path = tmp_path / "corpus.jsonl"
    path.write_text("\n\n", encoding="utf-8")
    with pytest.raises(ValueError):
        load_corpus(path)


def test_missing_file_fails(tmp_path: Path) -> None:
    with pytest.raises(FileNotFoundError):
        load_corpus(tmp_path / "nope.jsonl")


def test_invalid_line_reports_number(tmp_path: Path) -> None:
    path = tmp_path / "corpus.jsonl"
    good = json.dumps({"sport": "nba", "question": "q", "sql": "SELECT 1"})
    path.write_text(good + "\nnot json\n", encoding="utf-8")
    with pytest.raises(ValueError) as raised:
        load_corpus(path)
    assert "line 2" in str(raised.value)


def test_field_bounds_enforced(tmp_path: Path) -> None:
    path = tmp_path / "corpus.jsonl"
    long_sport = {"sport": "x" * 33, "question": "q", "sql": "SELECT 1"}
    path.write_text(json.dumps(long_sport), encoding="utf-8")
    with pytest.raises(ValueError):
        load_corpus(path)


def test_entry_model_defaults_strict() -> None:
    with pytest.raises(ValueError):
        CorpusEntry(sport="nba", question="", sql="SELECT 1")
