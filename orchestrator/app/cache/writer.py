from __future__ import annotations

import json
from typing import Any, Protocol

import redis

from app.core.settings import Settings


class CacheWriter(Protocol):
    def put(self, key: str, payload: dict[str, Any], ttl_seconds: int) -> None: ...


class RedisCacheWriter:
    def __init__(self, url: str) -> None:
        self._client = redis.Redis.from_url(url, decode_responses=True)

    def put(self, key: str, payload: dict[str, Any], ttl_seconds: int) -> None:
        self._client.setex(key, ttl_seconds, json.dumps(payload, default=str))


class NoopCacheWriter:
    def put(self, key: str, payload: dict[str, Any], ttl_seconds: int) -> None:
        return None


def build_cache_writer(settings: Settings) -> CacheWriter:
    if settings.cache_redis_url:
        return RedisCacheWriter(settings.cache_redis_url)
    return NoopCacheWriter()
