from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class OrchestrateRequest(BaseModel):
    sport: str = Field(min_length=1, max_length=32)
    question: str = Field(min_length=1, max_length=2000)
    max_rows: int = Field(default=50, ge=1, le=1000)
    cache_key: str | None = Field(default=None, max_length=128)


class OrchestrateResponse(BaseModel):
    query: str
    columns: list[str]
    rows: list[list[Any]]
    row_count: int
    truncated: bool
    elapsed_ms: int
