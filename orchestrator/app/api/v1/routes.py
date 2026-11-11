from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException

from app.api.v1 import deps
from app.api.v1.schemas import OrchestrateRequest, OrchestrateResponse
from app.db.pool import Database
from app.service.pipeline import Pipeline, PipelineResult

router = APIRouter()


@router.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}


@router.get("/readyz")
def readyz(database: Database = Depends(deps.get_database)) -> dict[str, str]:
    if not database.ping():
        raise HTTPException(status_code=503, detail="not ready")
    return {"status": "ready"}


@router.post("/v1/orchestrate", response_model=OrchestrateResponse)
def orchestrate(
    payload: OrchestrateRequest,
    pipeline: Pipeline = Depends(deps.get_pipeline),
) -> OrchestrateResponse:
    result: PipelineResult = pipeline.run(
        question=payload.question,
        sport=payload.sport,
        max_rows=payload.max_rows,
        cache_key=payload.cache_key,
    )
    return OrchestrateResponse(
        query=result.query,
        columns=result.columns,
        rows=result.rows,
        row_count=result.row_count,
        truncated=result.truncated,
        elapsed_ms=result.elapsed_ms,
    )
