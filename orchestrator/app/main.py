from __future__ import annotations

from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.responses import JSONResponse

from app.api.v1.routes import router
from app.cache.writer import CacheWriter, build_cache_writer
from app.core.logging import configure_logging, get_logger
from app.core.settings import Settings
from app.db.pool import Database
from app.errors import BadRequest, LLMFailure, NoSafeQuery, OrchestratorError, QueryFailed
from app.llm import build_llm
from app.llm.base import LLMClient
from app.retrieval.store import QuestionStore, build_retriever
from app.schema_ctx.manifest import Manifest, load_manifest
from app.service.pipeline import Pipeline

logger = get_logger(__name__)

_ERROR_TABLE: tuple[tuple[type[OrchestratorError], str, int], ...] = (
    (BadRequest, "bad_request", 400),
    (NoSafeQuery, "safety_violation", 422),
    (LLMFailure, "llm_unavailable", 502),
    (QueryFailed, "query_failed", 500),
)


def classify(error: OrchestratorError) -> tuple[str, int]:
    for error_type, code, status in _ERROR_TABLE:
        if isinstance(error, error_type):
            return code, status
    return "internal_error", 500


def create_app(
    settings: Settings | None = None,
    *,
    manifest: Manifest | None = None,
    llm: LLMClient | None = None,
    database: Database | None = None,
    cache: CacheWriter | None = None,
    retriever: QuestionStore | None = None,
) -> FastAPI:
    settings = settings or Settings()
    configure_logging(settings.log_level)
    logger.warning(
        "orchestrator config",
        extra={
            "provider": settings.llm_provider,
            "model": settings.llm_model,
            "few_shot_k": settings.few_shot_k,
            "provider_warning": (
                "running with the deterministic FakeLLM - SQL will NOT come from a real model"
                if settings.llm_provider == "fake"
                else None
            ),
        },
    )
    loaded_manifest = manifest or load_manifest(settings.resolve_manifest_path())
    loaded_llm = llm or build_llm(settings)
    loaded_database = database or Database(
        settings.database_url,
        settings.db_max_connections,
        settings.db_statement_timeout_ms,
    )
    loaded_cache = cache or build_cache_writer(settings)
    loaded_retriever = retriever if retriever is not None else build_retriever(settings)
    pipeline = Pipeline(
        settings,
        loaded_manifest,
        loaded_llm,
        loaded_database,
        loaded_cache,
        loaded_retriever,
    )

    @asynccontextmanager
    async def lifespan(app: FastAPI) -> AsyncIterator[None]:
        yield
        if database is None:
            loaded_database.close()

    app = FastAPI(title=settings.app_name, version="0.1.0", lifespan=lifespan)
    app.state.settings = settings
    app.state.manifest = loaded_manifest
    app.state.pipeline = pipeline
    app.state.database = loaded_database

    @app.exception_handler(OrchestratorError)
    def handle_orchestrator_error(request, error: OrchestratorError) -> JSONResponse:
        code, status = classify(error)
        logger.warning("orchestrator error", extra={"detail": error.detail})
        return JSONResponse(
            status_code=status,
            content={"error": {"code": code, "message": error.user_message}},
        )

    app.include_router(router)
    return app
