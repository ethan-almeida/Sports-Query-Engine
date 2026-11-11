from __future__ import annotations

from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict


def resolve_data_path(*parts: str) -> Path:
    cwd_candidate = Path.cwd().joinpath(*parts)
    if cwd_candidate.exists():
        return cwd_candidate
    return Path(__file__).resolve().parents[3].joinpath(*parts)


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="ORC_", env_file=".env", extra="ignore")

    app_name: str = "sports-orchestrator"
    log_level: str = "INFO"
    llm_provider: str = "fake"
    llm_timeout_seconds: float = 30.0
    max_repair_attempts: int = 2
    llm_base_url: str = ""
    llm_api_key: str = ""
    llm_model: str = "llama-3.3-70b-versatile"
    llm_temperature: float = 0.0
    llm_max_tokens: int = 1024
    few_shot_k: int = 0
    retrieval_chroma_dir: str = ""
    corpus_path: str = ""
    database_url: str = "postgresql://orchestrator:dev_orchestrator@localhost:5432/sports"
    db_max_connections: int = 5
    db_statement_timeout_ms: int = 5000
    manifest_path: str = ""
    cache_redis_url: str = ""
    cache_ttl_seconds: int = 300

    def resolve_manifest_path(self) -> Path:
        if self.manifest_path:
            return Path(self.manifest_path)
        return resolve_data_path("data", "schema", "manifest.yaml")

    def resolve_chroma_path(self) -> Path:
        if self.retrieval_chroma_dir:
            return Path(self.retrieval_chroma_dir)
        return resolve_data_path("data", "chroma")

    def resolve_corpus_path(self) -> Path:
        if self.corpus_path:
            return Path(self.corpus_path)
        return resolve_data_path("data", "corpus", "questions.jsonl")
