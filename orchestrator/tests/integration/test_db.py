from __future__ import annotations

import os
from pathlib import Path

import psycopg
import pytest

from app.cache.writer import NoopCacheWriter
from app.core.settings import Settings
from app.db.pool import Database
from app.llm.fake import FakeLLM
from app.schema_ctx.manifest import load_manifest
from app.service.pipeline import Pipeline

pytestmark = pytest.mark.integration

REPO_ROOT = Path(__file__).resolve().parents[3]
DEFAULT_URL = "postgresql://orchestrator:dev_orchestrator@localhost:5433/sports"


@pytest.fixture(scope="module")
def it_database_url() -> str:
    url = os.environ.get("ORC_IT_DATABASE_URL", DEFAULT_URL)
    try:
        psycopg.connect(url, connect_timeout=2).close()
    except Exception as exc:
        pytest.skip(f"integration postgres not reachable: {exc}")
    return url


def test_pipeline_executes_against_real_postgres(it_database_url: str) -> None:
    database = Database(it_database_url, max_connections=2, statement_timeout_ms=5000)
    pipeline = Pipeline(
        Settings(_env_file=None),
        load_manifest(REPO_ROOT / "data" / "schema" / "manifest.yaml"),
        FakeLLM(["SELECT code FROM sports.leagues LIMIT 5"]),
        database,
        NoopCacheWriter(),
    )

    result = pipeline.run(question="list leagues", sport="basketball", max_rows=5)

    assert result.columns == ["code"]
    assert result.rows == []
    assert result.row_count == 0
    assert result.truncated is False
    database.close()


def test_reader_connection_enforces_read_only(it_database_url: str) -> None:
    database = Database(it_database_url, max_connections=2, statement_timeout_ms=5000)

    with pytest.raises(Exception):
        database.execute("CREATE TABLE integration_probe (id int)", 10)
    with pytest.raises(Exception):
        database.execute("DELETE FROM sports.leagues", 10)
    with pytest.raises(Exception):
        database.execute("SELECT pg_sleep(10)", 10)

    database.close()
