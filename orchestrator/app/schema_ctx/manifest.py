from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml
from pydantic import BaseModel, ConfigDict, ValidationError

_STRICT = ConfigDict(frozen=True, extra="forbid")


class ColumnMeta(BaseModel):
    model_config = _STRICT
    type: str
    description: str


class TableMeta(BaseModel):
    model_config = _STRICT
    description: str
    primary_key: tuple[str, ...]
    columns: dict[str, ColumnMeta]

    def column(self, name: str) -> ColumnMeta | None:
        return self.columns.get(name)


class SchemaMeta(BaseModel):
    model_config = _STRICT
    description: str
    tables: dict[str, TableMeta]


class Manifest(BaseModel):
    model_config = _STRICT
    version: int
    default_schema: str
    schemas: dict[str, SchemaMeta]
    relations: tuple[str, ...] = ()

    def table(self, name: str, schema_name: str | None = None) -> TableMeta | None:
        schema = self.schemas.get(
            self.default_schema if schema_name is None else schema_name
        )
        if schema is None:
            return None
        return schema.tables.get(name)

    def qualified_table_names(self) -> set[tuple[str, str]]:
        return {
            (schema_name, table_name)
            for schema_name, schema in self.schemas.items()
            for table_name in schema.tables
        }


def load_manifest(path: str | Path) -> Manifest:
    path = Path(path)
    try:
        raw: dict[str, Any] = yaml.safe_load(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise ValueError(f"manifest not found: {path}") from exc
    except yaml.YAMLError as exc:
        raise ValueError(f"manifest {path} is not valid YAML: {exc}") from exc

    try:
        manifest = Manifest.model_validate(raw)
    except ValidationError as exc:
        raise ValueError(f"manifest {path} failed validation: {exc}") from exc

    if manifest.default_schema not in manifest.schemas:
        raise ValueError(
            f"manifest {path}: default_schema '{manifest.default_schema}' "
            "is not present in schemas"
        )
    return manifest
