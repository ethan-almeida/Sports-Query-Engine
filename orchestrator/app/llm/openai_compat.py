from __future__ import annotations

from typing import Any

import httpx
from openai import (
    APIConnectionError,
    APIStatusError,
    OpenAI,
    OpenAIError,
)

from app.errors import LLMFailure

GROQ_BASE_URL = "https://api.groq.com/openai/v1"


class OpenAICompatibleClient:
    def __init__(
        self,
        base_url: str,
        api_key: str,
        model: str,
        timeout_seconds: float,
        temperature: float,
        max_tokens: int,
        http_client: httpx.Client | None = None,
    ) -> None:
        self._model = model
        self._temperature = temperature
        self._max_tokens = max_tokens
        client_kwargs: dict[str, Any] = {
            "base_url": base_url,
            "api_key": api_key,
            "timeout": timeout_seconds,
            "max_retries": 0,
        }
        if http_client is not None:
            client_kwargs["http_client"] = http_client
        self._client = OpenAI(**client_kwargs)

    def complete(self, messages: list[dict[str, str]]) -> str:
        try:
            response = self._client.chat.completions.create(
                model=self._model,
                messages=messages,
                temperature=self._temperature,
                max_tokens=self._max_tokens,
            )
        except APIConnectionError as exc:
            raise LLMFailure(f"llm connection failed: {type(exc).__name__}") from exc
        except APIStatusError as exc:
            raise LLMFailure(f"llm http {exc.status_code}") from exc
        except OpenAIError as exc:
            raise LLMFailure(f"llm call failed: {type(exc).__name__}") from exc

        content = ""
        if response.choices:
            content = response.choices[0].message.content or ""
        if not content.strip():
            raise LLMFailure("llm returned an empty completion")
        return content
