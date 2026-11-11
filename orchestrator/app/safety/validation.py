from __future__ import annotations

from typing import NoReturn

import sqlglot
from sqlglot import exp
from sqlglot.optimizer.qualify import qualify

from app.errors import SafetyViolation
from app.schema_ctx.manifest import Manifest

_DIALECT = "postgres"

_DENIED_FUNCTIONS = frozenset(
    {
        "pg_stat_file",
        "lo_get",
        "lo_put",
        "lo_from_bytea",
        "lo_creat",
        "lo_create",
        "lo_unlink",
        "pg_reload_conf",
    }
)
_DENIED_PREFIXES = (
    "pg_sleep",
    "pg_read",
    "pg_ls_dir",
    "dblink",
    "lo_import",
    "lo_export",
)

_DENIED_EXTRA = frozenset(
    {
        "pg_sleep",
        "pg_sleep_for",
        "pg_sleep_until",
        "pg_read_file",
        "pg_read_file_v2",
        "pg_read_binary_file",
        "pg_ls_dir",
        "pg_stat_file",
        "dblink",
        "dblink_connect",
        "dblink_connect_u",
        "dblink_exec",
        "dblink_send",
        "lo_import",
        "lo_export",
        "pg_terminate_backend",
        "pg_cancel_backend",
        "pg_reload_conf",
    }
)

_ALL_DENIED = _DENIED_FUNCTIONS | _DENIED_EXTRA


def _fail(detail: str) -> NoReturn:
    raise SafetyViolation(detail)


def _manifest_schema(manifest: Manifest) -> dict[str, dict[str, dict[str, str]]]:
    return {
        schema_name: {
            table_name: {col: meta.type for col, meta in table.columns.items()}
            for table_name, table in schema.tables.items()
        }
        for schema_name, schema in manifest.schemas.items()
    }


def _function_name(func: exp.Func) -> str:
    return (func.text("this") or "").lower()


def _check_functions(statement: exp.Expression) -> None:
    for func in statement.find_all(exp.Func):
        name = _function_name(func)
        if name in _ALL_DENIED or name.startswith(_DENIED_PREFIXES):
            _fail(f"function not allowed: {name or '<unnamed>'}")


def _check_tables(statement: exp.Expression, manifest: Manifest) -> None:
    whitelisted = {(s, t.casefold()) for s, t in manifest.qualified_table_names()}
    cte_aliases = {cte.alias_or_name.casefold() for cte in statement.find_all(exp.CTE)}
    default_schema = manifest.default_schema

    for table in statement.find_all(exp.Table):
        schema_name = (table.db or table.catalog or "").casefold() or default_schema
        table_name = table.name.casefold()

        if table_name in cte_aliases and not (table.db or table.catalog):
            continue

        if (schema_name, table_name) not in whitelisted:
            _fail(f"table not whitelisted: {schema_name}.{table_name}")


def _inject_default_schema(statement: exp.Expression, manifest: Manifest) -> None:
    cte_aliases = {cte.alias_or_name.casefold() for cte in statement.find_all(exp.CTE)}
    default_schema = manifest.default_schema

    for table in statement.find_all(exp.Table):
        if not table.db and not table.catalog:
            if table.name.casefold() not in cte_aliases:
                table.set("db", exp.to_identifier(default_schema))


def _qualify(statement: exp.Expression, manifest: Manifest) -> exp.Expression:
    return qualify(
        statement,
        schema=_manifest_schema(manifest),
        dialect=_DIALECT,
    )


def _recheck_columns(qualified: exp.Expression, manifest: Manifest) -> None:
    cte_aliases = {cte.alias_or_name.casefold() for cte in qualified.find_all(exp.CTE)}
    tables_by_name: dict[str, exp.Table] = {
        table.alias_or_name.casefold(): table for table in qualified.find_all(exp.Table)
    }

    for column in qualified.find_all(exp.Column):
        qualifier = column.table.casefold()
        if not qualifier or qualifier in cte_aliases:
            continue  

        table = tables_by_name.get(qualifier)
        if table is None:
            _fail(f"column source unresolved: {column.sql()}")

        schema_name = (table.db or manifest.default_schema).casefold()
        table_meta = manifest.table(table.name.casefold(), schema_name)
        if table_meta is None:
            _fail(f"table not whitelisted: {schema_name}.{table.name}")
        if column.name.casefold() not in table_meta.columns:
            _fail(f"column not whitelisted: {schema_name}.{table.name}.{column.name}")


def validate_query(sql_text: str, manifest: Manifest) -> str:
    if not sql_text or not sql_text.strip():
        _fail("empty query")

    try:
        parsed = sqlglot.parse(sql_text, read=_DIALECT)
    except Exception as exc:  
        _fail(f"query did not parse: {type(exc).__name__}")

    statements = [stmt for stmt in parsed if stmt is not None]
    if len(statements) != 1:
        _fail(f"expected exactly one statement, got {len(statements)}")
    statement = statements[0]

    if not isinstance(statement, exp.Select):
        _fail("only SELECT statements are allowed")
    if "locks" in statement.args:
        _fail("row locking (FOR UPDATE / FOR SHARE) is not allowed")

    _check_functions(statement)
    _check_tables(statement, manifest)
    _inject_default_schema(statement, manifest)

    try:
        qualified = _qualify(statement, manifest)
    except Exception as exc:
        _fail(f"query references something outside the whitelist: {exc}")

    _recheck_columns(qualified, manifest)

    return qualified.sql(dialect=_DIALECT)
