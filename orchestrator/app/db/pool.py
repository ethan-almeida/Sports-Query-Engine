from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from psycopg_pool import ConnectionPool


@dataclass
class QueryResult:
    columns: list[str]
    rows: list[list[Any]]
    row_count: int
    truncated: bool


class Database:
    def __init__(
        self,
        url: str,
        max_connections: int = 5,
        statement_timeout_ms: int = 5000,
    ) -> None:
        options = f"-c statement_timeout={statement_timeout_ms} -c default_transaction_read_only=on"
        self._pool = ConnectionPool(
            conninfo=url,
            kwargs={"options": options},
            min_size=1,
            max_size=max_connections,
        )

    def execute(self, sql: str, max_rows: int) -> QueryResult:
        with self._pool.connection() as conn:
            with conn.cursor() as cur:
                cur.execute(sql)
                fetched = cur.fetchmany(max_rows + 1)
                columns = [description.name for description in cur.description or ()]
        rows = [list(row) for row in fetched[:max_rows]]
        return QueryResult(
            columns=columns,
            rows=rows,
            row_count=len(rows),
            truncated=len(fetched) > max_rows,
        )

    def ping(self) -> bool:
        try:
            with self._pool.connection() as conn:
                with conn.cursor() as cur:
                    cur.execute("SELECT 1")
                    cur.fetchone()
        except Exception:
            return False
        return True

    def close(self) -> None:
        self._pool.close()
