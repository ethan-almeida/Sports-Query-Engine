from __future__ import annotations

from collections.abc import Iterable

_LEAGUES_SQL = "SELECT code, name FROM sports.leagues ORDER BY code LIMIT 10"
_TEAMS_SQL = "SELECT t.code, t.name FROM sports.teams AS t ORDER BY t.name LIMIT 10"
_POINTS_SQL = (
    "SELECT s.player_code, SUM(s.stat_value) AS total FROM sports.player_stats AS s "
    "WHERE s.stat_name = 'points' GROUP BY s.player_code ORDER BY total DESC LIMIT 10"
)


class FakeLLM:
    def __init__(self, responses: Iterable[str] | None = None) -> None:
        self._responses = list(responses) if responses else []
        self.calls: list[list[dict[str, str]]] = []

    def complete(self, messages: list[dict[str, str]]) -> str:
        self.calls.append([dict(message) for message in messages])
        if self._responses:
            return self._responses.pop(0)
        return self._respond(messages)

    def _respond(self, messages: list[dict[str, str]]) -> str:
        question = next(
            (message["content"] for message in reversed(messages) if message["role"] == "user"),
            "",
        ).lower()
        if "point" in question:
            return _POINTS_SQL
        if "team" in question:
            return _TEAMS_SQL
        return _LEAGUES_SQL
