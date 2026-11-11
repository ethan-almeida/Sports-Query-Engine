from pathlib import Path

import pytest

from app.schema_ctx.manifest import load_manifest

REPO_ROOT = Path(__file__).resolve().parents[2]
REAL_MANIFEST = REPO_ROOT / "data" / "schema" / "manifest.yaml"


def test_loads_real_manifest() -> None:
    manifest = load_manifest(REAL_MANIFEST)
    assert manifest.version == 1
    assert manifest.default_schema == "sports"
    assert len(manifest.relations) == 6
    assert manifest.qualified_table_names() == {
        ("sports", "leagues"),
        ("sports", "teams"),
        ("sports", "games"),
        ("sports", "players"),
        ("sports", "player_stats"),
    }


def test_table_and_column_lookups() -> None:
    manifest = load_manifest(REAL_MANIFEST)

    leagues = manifest.table("leagues")
    assert leagues is not None
    assert set(leagues.columns) == {"code", "name", "sport", "created_at", "updated_at"}
    assert leagues.columns["sport"].type == "text"
    assert manifest.table("does_not_exist") is None
    assert manifest.table("watermarks", schema_name="ingest") is None
    assert manifest.table("leagues", schema_name="ingest") is None


def test_column_types_mirror_migrations() -> None:
    manifest = load_manifest(REAL_MANIFEST)

    strict = [
        m
        for t in manifest.schemas["sports"].tables.values()
        for c in t.columns.values()
        for m in [c.type]
    ]
    assert "timestampz" not in strict
    assert {"timestamptz", "text", "integer", "numeric", "boolean"} >= set(strict)


def test_missing_file() -> None:
    with pytest.raises(ValueError, match="not found"):
        load_manifest("/nonexistent/manifest.yaml")


def test_invalid_yaml(tmp_path: Path) -> None:
    bad = tmp_path / "bad.yaml"
    bad.write_text("key: [unclosed\n", encoding="utf-8")

    with pytest.raises(ValueError, match="not valid YAML"):
        load_manifest(bad)


def test_unknown_key_rejected(tmp_path: Path) -> None:
    typo = tmp_path / "typo.yaml"
    typo.write_text(
        "version: 1\ndefault_schema: sports\ntables: {}\n", encoding="utf-8"
    )

    with pytest.raises(ValueError, match="failed validation"):
        load_manifest(typo) 


def test_default_schema_must_exist(tmp_path: Path) -> None:
    bad = tmp_path / "bad_default.yaml"
    bad.write_text("version: 1\ndefault_schema: nosuch\nschemas: {}\n", encoding="utf-8")

    with pytest.raises(ValueError, match="default_schema"):
        load_manifest(bad)
