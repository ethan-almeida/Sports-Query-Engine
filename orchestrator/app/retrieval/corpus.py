from __future__ import annotations

import json
from pathlib import Path

from pydantic import BaseModel, Field

MAX_SQL_LENGTH = 8000


class CorpusEntry(BaseModel):
    sport: str = Field(min_length=1, max_length=32)
    question: str = Field(min_length=1, max_length=2000)
    sql: str = Field(min_length=1, max_length=MAX_SQL_LENGTH)


def load_corpus(path: Path) -> list[CorpusEntry]:
    if not path.is_file():
        raise FileNotFoundError(f"corpus file missing: {path}")
    entries: list[CorpusEntry] = []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        if not line.strip():
            continue
        try:
            entries.append(CorpusEntry.model_validate(json.loads(line)))
        except (json.JSONDecodeError, ValueError) as exc:
            raise ValueError(f"corpus line {line_number} is invalid: {exc}") from exc
    if not entries:
        raise ValueError(f"corpus file has no entries: {path}")
    return entries
