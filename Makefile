PY := $(abspath orchestrator/.venv/bin/python)
ASK := $(abspath orchestrator/scripts/ask.py)
SOURCES_FILE := $(or $(INGESTOR_SOURCES_FILE),$(shell test -f .env && grep -E '^INGESTOR_SOURCES_FILE=' .env | cut -d= -f2-))
SOURCES_FILE := $(if $(SOURCES_FILE),$(SOURCES_FILE),sources-espn.yaml)

.PHONY: launch up down wipe-data ask seasons forget ingest ingest-once seed test-py test-go test-integration lint verify-schema

launch:
	@test -f .env || (cp .env.example .env && echo "created .env from .env.example (dev defaults, edit as needed)")
	docker compose up -d --build --wait
	@echo ""
	@echo "gateway UI:  http://localhost:8080"
	@echo "ask cli:     make ask Q=\"which EPL teams won last saturday\""
	@echo "status:      make seasons"
	@echo ""
	@echo "first ingest runs in the background (ESPN seasons take ~10 min on the 350ms pacer); tail it with:"
	@echo "  docker compose logs -f ingestor"

wipe-data:
	docker compose exec -T postgres sh -lc 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -c "TRUNCATE sports.player_stats, sports.players, sports.games, sports.teams, sports.leagues, ingest.watermarks CASCADE;"'
	@echo "sports.* + ingest.watermarks truncated; the ingestor re-upserts the configured sources on its next cycle (or: make seed)"

ask:
	@test -n "$(Q)" || (echo "usage: make ask Q=\"question\" [SPORT=soccer|basketball|all] [ROWS=50] [API_KEY=...]"; exit 1)
	@Q="$(value Q)" SPORT="$(SPORT)" ROWS="$(ROWS)" API_KEY="$(API_KEY)" $(PY) $(ASK)

seasons:
	@echo "sources file: $(SOURCES_FILE)"
	@grep -E "^  - id:" ingestor/configs/$(SOURCES_FILE) || true
	@docker compose exec -T postgres sh -lc 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -c "SELECT source_id, stream, cursor::text, updated_at FROM ingest.watermarks ORDER BY source_id, stream;"' || true

up:
	docker compose up -d --wait

down:
	docker compose down

ingest:
	docker compose build orchestrator >/dev/null
	docker compose run --rm orchestrator python -m app.retrieval.ingest

forget:
	docker compose exec -T redis redis-cli FLUSHALL

ingest-once:
	docker compose build ingestor >/dev/null
	docker compose run --rm ingestor once

seed:
	docker compose build ingestor >/dev/null
	docker compose run --rm ingestor seed

test-py:
	cd orchestrator && .venv/bin/ruff check . && .venv/bin/python -m pytest -q

test-go:
	cd gateway && gofmt -l . && go vet ./... && go test ./...
	cd ingestor && gofmt -l . && go vet ./... && go test ./...

test-integration:
	docker compose -f docker-compose.test.yml up -d
	docker compose -f docker-compose.test.yml wait migrator
	cd orchestrator && .venv/bin/python -m pytest -q

lint:
	cd orchestrator && .venv/bin/ruff check .
	cd gateway && golangci-lint run ./...
	cd ingestor && golangci-lint run ./...

verify-schema:
	docker compose -f docker-compose.test.yml down -v
	docker compose -f docker-compose.test.yml up -d
	docker compose -f docker-compose.test.yml wait migrator
	@docker compose -f docker-compose.test.yml exec -T postgres psql -U postgres -d sports -tAc "SELECT CASE WHEN count(*) = 5 THEN 'verify-schema: OK' ELSE 'verify-schema: MISSING TABLES' END FROM information_schema.tables WHERE table_schema = 'sports' AND table_name IN ('leagues', 'teams', 'players', 'games', 'player_stats');"
	@docker compose -f docker-compose.test.yml exec -T postgres psql -U postgres -d sports -tAc "SELECT CASE WHEN count(*) = 1 THEN 'ingest.watermarks: OK' ELSE 'ingest.watermarks: MISSING' END FROM information_schema.tables WHERE table_schema = 'ingest' AND table_name = 'watermarks';"
	@docker compose -f docker-compose.test.yml exec -T postgres psql -U postgres -d sports -tAc "SELECT CASE WHEN has_table_privilege('sports_writer', 'sports.games', 'INSERT,UPDATE') AND has_table_privilege('sports_reader', 'sports.games', 'SELECT') AND NOT has_table_privilege('sports_reader', 'sports.games', 'INSERT') THEN 'role privileges: OK' ELSE 'role privileges: MISMATCH' END;"
