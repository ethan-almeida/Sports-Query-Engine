from __future__ import annotations

import sys

from app.core.logging import get_logger
from app.core.settings import Settings
from app.retrieval.corpus import load_corpus
from app.retrieval.store import QuestionStore
from app.safety.validation import validate_query
from app.schema_ctx.manifest import load_manifest
from app.service.pipeline import cap_rows

logger = get_logger(__name__)


def main() -> int:
    settings = Settings()
    manifest = load_manifest(settings.resolve_manifest_path())
    entries = load_corpus(settings.resolve_corpus_path())

    canonical: list[str] = []
    for entry in entries:
        rewritten = cap_rows(validate_query(entry.sql, manifest), 1000)
        if rewritten != entry.sql:
            logger.warning(
                "corpus sql was rewritten by the gate",
                extra={"question": entry.question, "detail": rewritten[:120]},
            )
        canonical.append(entry.sql)

    store = QuestionStore(settings.resolve_chroma_path())
    count = store.ingest(entries)
    logger.info(
        "corpus ingested",
        extra={"entries": count, "collection_size": store.count()},
    )
    print(f"corpus ingested: {count} entries, collection size {store.count()}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
