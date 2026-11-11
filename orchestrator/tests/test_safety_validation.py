from pathlib import Path

import pytest

from app.errors import SafetyViolation
from app.safety.validation import validate_query
from app.schema_ctx.manifest import Manifest, load_manifest

REPO_ROOT = Path(__file__).resolve().parents[2]
REAL_MANIFEST = REPO_ROOT / "data" / "schema" / "manifest.yaml"


@pytest.fixture(scope="module")
def manifest() -> Manifest:
    return load_manifest(REAL_MANIFEST)


ALLOWED = (
    "SELECT code FROM sports.leagues",
    "select code from leagues",  
    "SELECT * FROM sports.leagues",
    "SELECT l.name, g.game_code FROM sports.teams AS l "
    "JOIN sports.games AS g ON g.league_code = l.league_code",
    "SELECT count(*) FROM sports.player_stats",
    "WITH game AS (SELECT league_code, game_code FROM sports.games) "
    "SELECT league_code FROM game",
    "SELECT stat_value FROM sports.player_stats WHERE stat_name = 'points'",
    "SELECT sport FROM sports.leagues "
    "WHERE sport = (SELECT sport FROM sports.leagues LIMIT 1)",
    "SELECT MAX(home_score) AS best FROM sports.games WHERE status = 'final'",
)

DENIED = (
    "DELETE FROM sports.leagues",
    "INSERT INTO sports.leagues VALUES ('NBA', 'x', 'x')",
    "UPDATE sports.teams SET name = 'x'",
    "DROP TABLE sports.teams",
    "TRUNCATE TABLE sports.teams",
    "EXPLAIN SELECT code FROM sports.leagues",
    "COPY sports.leagues TO '/tmp/evil.csv'",
    "SELECT code FROM sports.leagues; SELECT sport FROM sports.leagues",
    "SELECT code FROM sports.leagues FOR UPDATE",
    "SELECT code FROM sports.leagues FOR SHARE",
    "SELECT * FROM pg_catalog.pg_tables",
    "SELECT * FROM information_schema.tables",
    "SELECT relname FROM pg_class",  
    "SELECT * FROM ingest.watermarks",
    "SELECT code FROM sports.leagues WHERE pg_sleep(10) IS NOT NULL",
    "SELECT (SELECT pg_read_file('/etc/passwd'))",
    "WITH cte AS (SELECT dblink('host=evil')) SELECT 1 FROM sports.leagues",
    "SELECT secret_sauce FROM sports.teams",
    "SELECT sport FROM sports.players",
    "SELECT password FROM sports.leagues",
    "",
    "   \n\t ",
    "sdlkfj sldfkj ((",
)


@pytest.mark.parametrize("sql", ALLOWED)
def test_gate_allows_legitimate_sql(sql: str, manifest: Manifest) -> None:
    assert validate_query(sql, manifest)


@pytest.mark.parametrize("sql", DENIED)
def test_gate_rejects_adversarial_sql(sql: str, manifest: Manifest) -> None:
    with pytest.raises(SafetyViolation):
        validate_query(sql, manifest)


@pytest.mark.parametrize("sql", ALLOWED)
def test_gate_output_is_self_stable(sql: str, manifest: Manifest) -> None:
    once = validate_query(sql, manifest)
    assert validate_query(once, manifest) == once
    assert '"sports"' in once  


def test_violation_message_never_leaks_sql(manifest: Manifest) -> None:
    with pytest.raises(SafetyViolation) as caught:
        validate_query("SELECT * FROM pg_catalog.pg_tables", manifest)
    assert str(caught.value) == "the question could not be answered safely"
    assert "pg_tables" not in caught.value.user_message
    assert "pg_tables" in caught.value.detail
