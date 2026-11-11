from __future__ import annotations

from collections.abc import Callable

from app.errors import NoSafeQuery, SafetyViolation
from app.llm.base import LLMClient

_REPAIR_PROMPT = (
    "That query was rejected by the safety validator: {detail}. "
    "Return a corrected single SELECT query that answers the original question."
)


def attempt_with_repair(
    messages: list[dict[str, str]],
    llm: LLMClient,
    guard: Callable[[str], str],
    max_attempts: int,
) -> str:
    last_detail = ""
    for _ in range(max(1, max_attempts)):
        raw = llm.complete(messages)
        try:
            return guard(raw)
        except SafetyViolation as violation:
            last_detail = violation.detail
            messages.extend(
                [
                    {"role": "assistant", "content": raw},
                    {"role": "user", "content": _REPAIR_PROMPT.format(detail=last_detail)},
                ]
            )
    raise NoSafeQuery(last_detail)
