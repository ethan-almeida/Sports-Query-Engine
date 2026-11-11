from __future__ import annotations

import hashlib
from pathlib import Path
from typing import Any

import chromadb

from app.core.settings import Settings
from app.retrieval.corpus import CorpusEntry

COLLECTION_NAME = "question_sql"
_SPORT_ALIASES = {
    "basketball": "nba",
    "football": "soccer",
    "esports": "lol",
}
_ID_SEPARATOR = "\x1f"


def entry_id(entry: CorpusEntry) -> str:
    return hashlib.sha256(f"{entry.sport}{_ID_SEPARATOR}{entry.question}".encode()).hexdigest()


class QuestionStore:
    def __init__(self, directory: Path) -> None:
        client = chromadb.PersistentClient(path=str(directory))
        self._collection = client.get_or_create_collection(COLLECTION_NAME)

    def ingest(self, entries: list[CorpusEntry]) -> int:
        if not entries:
            return 0
        self._collection.upsert(
            ids=[entry_id(entry) for entry in entries],
            documents=[entry.question for entry in entries],
            metadatas=[
                {"sport": entry.sport, "question": entry.question, "sql": entry.sql}
                for entry in entries
            ],
        )
        return len(entries)

    def count(self) -> int:
        return self._collection.count()

    def similar(self, question: str, sport: str, k: int) -> list[CorpusEntry]:
        if k <= 0:
            return []
        total = self._collection.count()
        if total == 0:
            return []
        limit = min(k, total)
        candidates = [sport]
        alias = _SPORT_ALIASES.get(sport.lower())
        if alias and alias != sport:
            candidates.append(alias)
        for candidate in candidates:
            results = self._collection.query(
                query_texts=[question],
                n_results=limit,
                where={"sport": {"$eq": candidate}},
            )
            entries = self._as_entries(results)
            if entries:
                return entries
        results = self._collection.query(query_texts=[question], n_results=limit)
        return self._as_entries(results)

    def _as_entries(self, results: dict[str, Any]) -> list[CorpusEntry]:
        first_ids = results["ids"][0]
        if not first_ids:
            return []
        metadatas = results["metadatas"][0]
        return [
            CorpusEntry(
                sport=metadata["sport"],
                question=metadata["question"],
                sql=metadata["sql"],
            )
            for metadata in metadatas
        ]


def build_retriever(settings: Settings) -> QuestionStore | None:
    if settings.few_shot_k <= 0:
        return None
    return QuestionStore(settings.resolve_chroma_path())
