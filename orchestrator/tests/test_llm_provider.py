from __future__ import annotations

import json

import httpx
import pytest

from app.core.settings import Settings
from app.errors import LLMFailure
from app.llm import build_llm
from app.llm.openai_compat import OpenAICompatibleClient

_COMPLETION = {
    "id": "chatcmpl-123",
    "object": "chat.completion",
    "created": 1700000000,
    "model": "llama-3.3-70b-versatile",
    "choices": [
        {
            "index": 0,
            "message": {"role": "assistant", "content": "SELECT 1"},
            "finish_reason": "stop",
        }
    ],
    "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
}


def transport(handler, timeout_seconds: float | None = None) -> httpx.Client:
    kwargs = {"transport": httpx.MockTransport(handler)}
    if timeout_seconds is not None:
        kwargs["timeout"] = timeout_seconds
    return httpx.Client(**kwargs)


def text_endpoint(expected_body: dict | None = None):
    def handler(request: httpx.Request) -> httpx.Response:
        if expected_body is not None:
            assert request.read() == json.dumps(expected_body).encode()
        return httpx.Response(200, json=_COMPLETION)

    return handler


def make_provider(handler, timeout_seconds: float = 5.0) -> OpenAICompatibleClient:
    return OpenAICompatibleClient(
        base_url="https://api.groq.test/openai/v1",
        api_key="test-key",
        model="llama-3.3-70b-versatile",
        timeout_seconds=timeout_seconds,
        temperature=0.0,
        max_tokens=256,
        http_client=transport(handler, timeout_seconds=timeout_seconds),
    )


def test_happy_path_returns_content() -> None:
    provider = make_provider(text_endpoint())
    assert provider.complete([{"role": "user", "content": "hi"}]) == "SELECT 1"


def test_request_shape_matches_contract() -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["path"] = request.url.path
        captured["auth"] = request.headers.get("Authorization")
        captured["body"] = json.loads(request.read())
        return httpx.Response(200, json=_COMPLETION)

    provider = make_provider(handler)
    provider.complete([{"role": "system", "content": "rules"}, {"role": "user", "content": "q"}])

    assert captured["path"].endswith("/chat/completions")
    assert captured["auth"] == "Bearer test-key"
    assert captured["body"]["model"] == "llama-3.3-70b-versatile"
    assert captured["body"]["temperature"] == 0.0
    assert captured["body"]["max_tokens"] == 256
    assert [message["role"] for message in captured["body"]["messages"]] == ["system", "user"]


def test_rate_limit_maps_to_llm_failure() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(429, json={"error": {"message": "rate limit exceeded"}})

    with pytest.raises(LLMFailure) as failure:
        make_provider(handler).complete([{"role": "user", "content": "hi"}])
    assert "429" in failure.value.detail


def test_auth_failure_maps_to_llm_failure() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(401, json={"error": {"message": "invalid api key"}})

    with pytest.raises(LLMFailure) as failure:
        make_provider(handler).complete([{"role": "user", "content": "hi"}])
    assert "401" in failure.value.detail


def test_server_error_maps_to_llm_failure() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(503, json={"error": {"message": "unavailable"}})

    with pytest.raises(LLMFailure) as failure:
        make_provider(handler).complete([{"role": "user", "content": "hi"}])
    assert "503" in failure.value.detail


def test_empty_completion_is_failure() -> None:
    empty_choice = {"index": 0, "message": {"role": "assistant", "content": ""}}
    empty = _COMPLETION | {"choices": [empty_choice | {"finish_reason": "length"}]}
    provider = make_provider(lambda request: httpx.Response(200, json=empty))
    with pytest.raises(LLMFailure):
        provider.complete([{"role": "user", "content": "hi"}])


def test_no_choices_is_failure() -> None:
    blank = _COMPLETION | {"choices": []}
    provider = make_provider(lambda request: httpx.Response(200, json=blank))
    with pytest.raises(LLMFailure):
        provider.complete([{"role": "user", "content": "hi"}])


def test_connection_refused_is_failure() -> None:
    provider = OpenAICompatibleClient(
        base_url="http://127.0.0.1:1/v1",
        api_key="k",
        model="m",
        timeout_seconds=1.0,
        temperature=0.0,
        max_tokens=16,
    )
    with pytest.raises(LLMFailure) as failure:
        provider.complete([{"role": "user", "content": "hi"}])
    assert "connection" in failure.value.detail


def test_hard_timeout_is_failure() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.TimeoutException("timed out while connecting")

    provider = make_provider(handler)
    with pytest.raises(LLMFailure) as failure:
        provider.complete([{"role": "user", "content": "hi"}])
    assert "connection" in failure.value.detail


def test_factory_rejects_groq_without_key() -> None:
    settings = Settings(llm_provider="groq", llm_api_key="")
    with pytest.raises(ValueError):
        build_llm(settings)


def test_factory_builds_groq_client() -> None:
    settings = Settings(
        llm_provider="groq",
        llm_api_key="gsk_test",
        llm_base_url="",
        llm_model="llama-3.1-8b-instant",
    )
    client = build_llm(settings)
    assert isinstance(client, OpenAICompatibleClient)


def test_factory_rejects_unknown_provider() -> None:
    with pytest.raises(ValueError):
        build_llm(Settings(llm_provider="willow"))
