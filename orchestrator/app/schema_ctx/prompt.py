from __future__ import annotations

from collections.abc import Sequence

from app.retrieval.corpus import CorpusEntry
from app.schema_ctx.manifest import Manifest

_SYSTEM_RULES = (
    "You generate a single PostgreSQL SELECT query that answers the user's question. "
    "Only use the tables and columns listed below. "
    "Rules: one statement, SELECT only, no writes, no locking, explicit column names, "
    "and a LIMIT unless the question demands every row. "
    "Only use stat_name values that actually exist in the data "
    "(points, rebounds, assists, steals, kills, deaths); never invent new ones. "
    "If the user asks for exactly one of those stat_names and it is unavailable, do not "
    "substitute a different stat; return an empty result set instead. "
    "That escape hatch applies only to unavailable stat_names, never to questions that "
    "are derivable: there is no standings table, so champions, league tables, streaks "
    "and results are computed from final games. "
    "For such aggregates each team must be matched as home_team_code OR away_team_code, "
    "e.g. an OR in the join condition with counts and SUM CASE over the scores. "
    "Soccer uses 3 points per win and 1 per draw: a league winner or champion is the "
    "team with the most points in that league and season; in basketball rank by wins. "
    "Season values store the ESPN season year: a user saying '2023-24' maps to "
    "season '2023' for soccer and '2024' for basketball. "
    "Be flexible with wording: infer the league from the sport when it is not named; "
    "treat vague dates like 'recent', 'last week', or 'this season' with now() "
    "or the latest season value; "
    "use ILIKE for partial name matches; aggregate with SUM/COUNT/AVG when the "
    "question asks for totals or leaders; "
    "join teams to games on both league_code and team code; and default to "
    "returning a reasonable number of rows."
)


def build_schema_slice(manifest: Manifest) -> str:
    lines: list[str] = []
    for schema_name, schema in manifest.schemas.items():
        for table_name, table in schema.tables.items():
            lines.append(f"{schema_name}.{table_name} ({table.description})")
            for column_name, column in table.columns.items():
                lines.append(f"  {column_name} {column.type} - {column.description}")
    lines.append("joins:")
    lines.extend(f"  {relation}" for relation in manifest.relations)
    return "\n".join(lines)


def build_messages(
    question: str,
    sport: str,
    manifest: Manifest,
    exemplars: Sequence[CorpusEntry] = (),
) -> list[dict[str, str]]:
    system = f"{_SYSTEM_RULES}\n\n{build_schema_slice(manifest)}"
    messages: list[dict[str, str]] = [{"role": "system", "content": system}]
    for exemplar in exemplars:
        messages.append(
            {
                "role": "user",
                "content": f"sport: {exemplar.sport}\nquestion: {exemplar.question}",
            }
        )
        messages.append({"role": "assistant", "content": exemplar.sql})
    messages.append({"role": "user", "content": f"sport: {sport}\nquestion: {question}"})
    return messages
