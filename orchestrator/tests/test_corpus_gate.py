from __future__ import annotations

from pathlib import Path

import pytest

from app.core.settings import Settings
from app.retrieval.corpus import load_corpus
from app.safety.validation import validate_query
from app.schema_ctx.manifest import Manifest, load_manifest
from app.service.pipeline import cap_rows

REPO_ROOT = Path(__file__).resolve().parents[2]


@pytest.fixture(scope="module")
def manifest() -> Manifest:
    return load_manifest(Settings().resolve_manifest_path())


@pytest.mark.parametrize(
    "entry",
    load_corpus(REPO_ROOT / "data" / "corpus" / "questions.jsonl"),
    ids=lambda e: e.question[:40],
)
def test_shipped_corpus_passes_the_gate(entry, manifest: Manifest) -> None:
    rewritten = validate_query(entry.sql, manifest)
    capped = cap_rows(rewritten, 1000)
    assert rewritten.strip()
    assert capped.strip()
