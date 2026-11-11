from __future__ import annotations

from app.core.settings import Settings
from app.llm.base import LLMClient
from app.llm.fake import FakeLLM
from app.llm.openai_compat import GROQ_BASE_URL, OpenAICompatibleClient


def build_llm(settings: Settings) -> LLMClient:
    if settings.llm_provider == "fake":
        return FakeLLM()
    if settings.llm_provider == "groq":
        base_url = settings.llm_base_url or GROQ_BASE_URL
        if not settings.llm_api_key:
            raise ValueError("ORC_LLM_PROVIDER=groq requires ORC_LLM_API_KEY")
        return OpenAICompatibleClient(
            base_url=base_url,
            api_key=settings.llm_api_key,
            model=settings.llm_model,
            timeout_seconds=settings.llm_timeout_seconds,
            temperature=settings.llm_temperature,
            max_tokens=settings.llm_max_tokens,
        )
    raise ValueError(f"unknown ORC_LLM_PROVIDER: {settings.llm_provider}")
