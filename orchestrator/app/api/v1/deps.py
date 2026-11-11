from __future__ import annotations

from fastapi import Request

from app.core.settings import Settings
from app.db.pool import Database
from app.service.pipeline import Pipeline


def get_settings(request: Request) -> Settings:
    return request.app.state.settings


def get_pipeline(request: Request) -> Pipeline:
    return request.app.state.pipeline


def get_database(request: Request) -> Database:
    return request.app.state.database
