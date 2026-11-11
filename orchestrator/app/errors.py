from __future__ import annotations


class OrchestratorError(Exception):
    user_message = "request failed"
    def __init__(self, detail: str) -> None:
        self.detail = detail
        super().__init__(self.user_message)


class SafetyViolation(OrchestratorError):
    user_message = "the question could not be answered safely"


class BadRequest(OrchestratorError):
    user_message = "invalid request"


class NoSafeQuery(OrchestratorError):
    user_message = "the question could not be answered safely"


class LLMFailure(OrchestratorError):
    user_message = "the question could not be answered right now"


class QueryFailed(OrchestratorError):
    user_message = "the query could not be completed"
