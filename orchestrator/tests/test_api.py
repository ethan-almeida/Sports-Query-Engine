from __future__ import annotations

from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from app.cache.writer import NoopCacheWriter
from app.core.settings import Settings
from app.db.pool import QueryResult
from app.llm.fake import FakeLLM
from app.main import create_app
from app.schema_ctx.manifest import Manifest, load_manifest

REPO_ROOT = Path(__file__).resolve().parents[2]
REAL_MANIFEST = REPO_ROOT / "data" / "schema" / "manifest.yaml"


class StubDatabase:
    def __init__(self, up: bool = True) -> None:
        self.up = up

    def ping(self) -> bool:
        return self.up

    def execute(self, sql: str, max_rows: int) -> QueryResult:
        return QueryResult(columns=["code"], rows=[["NBA"]], row_count=1, truncated=False)

    def close(self) -> None:
        return None


@pytest.fixture(scope="module")
def manifest() -> Manifest:
    return load_manifest(REAL_MANIFEST)


def make_client(
    manifest: Manifest,
    responses: list[str] | None = None,
    database: StubDatabase | None = None,
) -> TestClient:
    app = create_app(
        Settings(_env_file=None),
        manifest=manifest,
        llm=FakeLLM(responses or ["SELECT code FROM sports.leagues"]),
        database=database or StubDatabase(),
        cache=NoopCacheWriter(),
    )
    return TestClient(app)


def test_healthz(manifest: Manifest) -> None:
    client = make_client(manifest)

    response = client.get("/healthz")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_readyz_up(manifest: Manifest) -> None:
    client = make_client(manifest)

    response = client.get("/readyz")

    assert response.status_code == 200
    assert response.json() == {"status": "ready"}


def test_readyz_down_returns_503(manifest: Manifest) -> None:
    client = make_client(manifest, database=StubDatabase(up=False))

    response = client.get("/readyz")

    assert response.status_code == 503


def test_orchestrate_happy_path_shape(manifest: Manifest) -> None:
    client = make_client(manifest)

    response = client.post(
        "/v1/orchestrate",
        json={
            "sport": "basketball",
            "question": "list leagues",
            "max_rows": 10,
            "cache_key": "k123",
        },
    )

    assert response.status_code == 200
    body = response.json()
    assert set(body) == {"query", "columns", "rows", "row_count", "truncated", "elapsed_ms"}
    assert body["rows"] == [["NBA"]]
    assert body["row_count"] == 1
    assert '"sports"."leagues"' in body["query"]


def test_orchestrate_safety_envelope_has_no_leak(manifest: Manifest) -> None:
    client = make_client(
        manifest,
        responses=["DELETE FROM sports.leagues", "TRUNCATE sports.teams"],
    )

    response = client.post(
        "/v1/orchestrate",
        json={"sport": "basketball", "question": "list leagues", "max_rows": 10},
    )

    assert response.status_code == 422
    assert response.json() == {
        "error": {
            "code": "safety_violation",
            "message": "the question could not be answered safely",
        }
    }
    assert "TRUNCATE" not in response.text
    assert "DELETE" not in response.text


def test_orchestrate_validates_request(manifest: Manifest) -> None:
    client = make_client(manifest)

    response = client.post("/v1/orchestrate", json={"sport": "basketball"})

    assert response.status_code == 422
    assert "detail" in response.json()


def test_orchestrate_rejects_oversized_max_rows(manifest: Manifest) -> None:
    client = make_client(manifest)

    response = client.post(
        "/v1/orchestrate",
        json={"sport": "basketball", "question": "list leagues", "max_rows": 99999},
    )

    assert response.status_code == 422
