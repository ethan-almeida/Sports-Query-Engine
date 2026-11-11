from __future__ import annotations

import time
from dataclasses import dataclass
from typing import Any

import sqlglot
from sqlglot import exp

from app.cache.writer import CacheWriter
from app.core.logging import get_logger
from app.core.settings import Settings
from app.db.pool import Database
from app.errors import LLMFailure, NoSafeQuery, OrchestratorError, QueryFailed, SafetyViolation
from app.llm.base import LLMClient
from app.retrieval.corpus import CorpusEntry
from app.retrieval.store import QuestionStore
from app.safety.validation import validate_query
from app.schema_ctx.manifest import Manifest
from app.schema_ctx.prompt import build_messages
from app.service.retry import attempt_with_repair

logger = get_logger(__name__)


@dataclass
class PipelineResult:
    query: str
    columns: list[str]
    rows: list[list[Any]]
    row_count: int
    truncated: bool
    elapsed_ms: int


def cap_rows(sql: str, max_rows: int) -> str:
    statement = sqlglot.parse_one(sql, read="postgres")
    limit = statement.args.get("limit")
    if isinstance(limit, exp.Limit) and isinstance(limit.expression, exp.Literal):
        try:
            if int(str(limit.expression.this)) <= max_rows:
                return sql
        except ValueError:
            pass
    statement.set("limit", exp.Limit(expression=exp.Literal.number(max_rows)))
    return statement.sql(dialect="postgres")


class Pipeline:
    def __init__(
        self,
        settings: Settings,
        manifest: Manifest,
        llm: LLMClient,
        database: Database,
        cache: CacheWriter,
        retriever: QuestionStore | None = None,
    ) -> None:
        self.settings = settings
        self.manifest = manifest
        self.llm = llm
        self.database = database
        self.cache = cache
        self.retriever = retriever

    def run(
        self,
        question: str,
        sport: str,
        max_rows: int,
        cache_key: str | None = None,
    ) -> PipelineResult:
        started = time.perf_counter()
        messages = build_messages(
            question,
            sport,
            self.manifest,
            self._retrieve_exemplars(question, sport),
        )
        try:
            query = attempt_with_repair(
                messages,
                self.llm,
                lambda sql_text: self._guard(sql_text, max_rows),
                self.settings.max_repair_attempts,
            )
        except NoSafeQuery:
            raise
        except SafetyViolation as violation:
            raise NoSafeQuery(violation.detail) from violation
        except OrchestratorError:
            raise
        except Exception as exc:
            raise LLMFailure(f"llm call failed: {type(exc).__name__}: {exc}") from exc

        try:
            result = self.database.execute(query, max_rows)
        except Exception as exc:
            raise QueryFailed(f"query execution failed: {type(exc).__name__}: {exc}") from exc

        payload = {
            "query": query,
            "columns": result.columns,
            "rows": result.rows,
            "row_count": result.row_count,
            "truncated": result.truncated,
            "elapsed_ms": int((time.perf_counter() - started) * 1000),
        }
        if cache_key and result.row_count > 0:
            try:
                self.cache.put(cache_key, payload, self.settings.cache_ttl_seconds)
            except Exception as exc:
                logger.warning("cache write failed", extra={"detail": str(exc)})
        return PipelineResult(**payload)

    def _retrieve_exemplars(self, question: str, sport: str) -> list[CorpusEntry]:
        if self.retriever is None or self.settings.few_shot_k <= 0:
            return []
        try:
            exemplars = self.retriever.similar(question, sport, self.settings.few_shot_k)
        except Exception as exc:
            logger.warning("few-shot retrieval failed", extra={"detail": str(exc)})
            return []
        return list(exemplars)

    def _guard(self, sql_text: str, max_rows: int) -> str:
        return cap_rows(validate_query(sql_text, self.manifest), max_rows)
