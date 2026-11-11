# M0 — Phase 0 audit report

Read-only audit of the repository against the v1 target in `docs/plan.md`. No code changed.

## 1. KEEP / MODIFY / DELETE table

| Path | Verdict | Reason |
|---|---|---|
| `docs/plan.md` | KEEP | The spec. |
| `docker-compose.yml` | MODIFY | Shrink to Postgres + Redis only; drop migrator, ingestor, orchestrator, gateway services. |
| `docker-compose.test.yml` | DELETE | Exists only to run the Python orchestrator integration tests. |
| `Makefile` | MODIFY (rewrite) | New targets: `up`, `migrate`, `ingest`, `run`, `eval` (+ `test`). |
| `.env` / `.env.example` | MODIFY (rewrite) | Replace ~40 vars with the 8 target vars (`LLM_*`, `DATABASE_URL_RO/RW`, `REDIS_URL`, plus `POSTGRES_*` for compose). |
| `.gitignore` | MODIFY | Drop Python/Chroma entries; add Go build artifacts. |
| `README.md` | MODIFY | Currently empty; write a v1 quickstart (suggested for M7). |
| `contract/openapi.orc.yaml` | DELETE | Documents the Go→Python surface that disappears. |
| `gateway/` (whole dir) | DELETE (rebuild) | Becomes `cmd/api` + `internal/*`. Selected pieces are ported, see §2. |
| `ingestor/` (whole dir) | DELETE (rebuild) | Becomes `cmd/ingest` + `internal/source/espn`. Selected pieces ported, see §2. |
| `orchestrator/` (whole dir) | DELETE | Python/FastAPI, ChromaDB, sqlglot, FakeLLM, venv, pyproject — all out of scope. |
| `data/chroma/` | DELETE | Chroma state (~550 KB, gitignored). |
| `data/corpus/questions.jsonl` | DELETE | 36 examples (17 nba / 8 soccer / 4 lol / 4 synthetic / 3 all). ~8 NBA examples become static Go source in the M3 prompt; the file goes away. |
| `data/migrations/0001–0004` | DELETE | Replaced by goose `migrations/`. Shapes inform the new schema, see §3. |
| `data/migrator/migrate.sh` | DELETE | Replaced by goose. |
| `data/postgres-init/roles.sql`, `user.sh` | DELETE | Roles/logins move into goose migration 0002, run as the Postgres owner. |
| `data/schema/manifest.yaml` | DELETE | The prompt gets a compact static schema block in Go instead of a YAML manifest. |

Note: "DELETE (rebuild)" for `gateway/`/`ingestor/` means the *directories* are removed in M1; the reusable pieces in §2 are copied into the new layout before/while doing so.

## 2. Reusable Go code

**Reuse (small and clean):**
- `ingestor/sources/espn/client.go` — HTTP GET with 350 ms pacing, 20 s 429 penalty, body limits. Becomes the ESPN client in `internal/source/espn`.
- `ingestor/sources/espn/codec.go` — `mapStatus`, `parseStartsAt`, `parseScore`, `slug`, `teamCode`, `positionName`, `dayFloor`, `scoreboardDate`. Well covered by `codec_test.go`.
- `ingestor/sources/espn/models.go` — payload structs for team list, roster, scoreboard, season. Extend with box-score structs (none exist yet — see risk R1).
- `ingestor/sources/espn/source.go` — only the leaf calls: `listTeams`, `listRoster`, `listScoreboard`, and the team-id/code resolution (`registerTeamsLocked`, `eventTeamCode`). The stream/cursor/stateful `Source` framework around them is dropped.
- `ingestor/store/upsert.go` — the `pgx.Batch` + `ON CONFLICT DO UPDATE` pattern and the transaction wrapper. The SQL itself is rewritten for the new tables.
- `gateway/server/server.go` — `connectRedis` (parse URL, ping, nil-when-unset), graceful shutdown, signal handling.
- `gateway/middleware/logging.go`, `requestid.go`, `recovery.go` — small, clean, keep as-is per the plan ("if trivial").
- `gateway/respond/respond.go` — JSON/error helpers.
- `gateway/ui/ui.go` — the `go:embed` handler pattern.
- `gateway/cache/cache.go` — `Get`/`NoopStore` shape (the sha256 `BuildKey` is replaced by the normalized `ask:<date>:<question>` key).
- `gateway/config/config.go` + `main.go` — the `env()` helper and slog-JSON logger setup are a good pattern for the new single module.
- `ingestor/main.go` — `connect` (pool + ping + timeout) and `redactCredentials` helpers.

**Not reusable (delete):**
- `gateway/orchestrator/client.go`, `gateway/model/` (sport/max_rows DTOs), `middleware/authn.go`, `middleware/ratelimit.go`, `handler/query.go` (proxy flow), `ui/index.html` (sport selector, API-key field, max-rows field, no answer area — rewrite for M6; the CSS is serviceable as a starting point).
- All of `ingestor/extract/` (SourceDef/Batch/cursor framework), `ingest/runner.go` (watermark loop), `watermark/`, `sources/synthetic.go`, `sources/tsdb/` (entire adapter), `config/` YAML sources config, `serve` daemon loop.
- All Python: `orchestrator/` including its tests, FakeLLM, retrieval, and the `scripts/ask.py` CLI client.

## 3. Existing DB schema vs. target

Current (migrations 0001–0004, schema `sports`, plus `ingest.watermarks`):

| Current object | Target object | Delta |
|---|---|---|
| `sports.leagues(code, name, sport)` | — | Dropped. Single sport (NBA), single league. |
| `sports.teams(league_code, code, name, abbreviation, conference, division, external_id, source)` PK(league_code, code) | `teams` | Drop `league_code`, `external_id`, `source`. PK → `code`. Keep `conference`/`division`? (open question Q3) |
| `sports.players(league_code, code, full_name, team_code, position, active, external_id, source)` PK(league_code, code) | `players` | Drop `league_code`, `active`, `external_id`, `source`. PK → `code`. FK `team_code`. |
| `sports.games(league_code, game_code, season, starts_at, status, home/away_team_code, home/away_score, venue)` PK(league_code, game_code) | `games` | Drop `league_code`. Add `game_date date` (plan's ingest logic uses `max(game_date)`); keep `season text` derived at ingest (October rule). Keep the `status IN (...)` and non-negative score CHECKs and a date index — worth reusing verbatim. |
| `sports.player_stats(league_code, game_code, player_code, stat_name, stat_value)` — EAV, 6 stat names, no 3PT | `player_game_stats` — wide, one row per (game, player): minutes, points, rebounds, assists, steals, blocks, turnovers, fg/3p/ft made+att (exact set = open question Q2). PK(game_code, player_code) | Entirely new shape; the EAV upsert SQL is **not** reusable (only the batch pattern). |
| — | `meta` | New. Holds `data_as_of`. |
| — | view `player_season_totals` | New (M1). |
| `ingest.watermarks` | — | Dropped. |
| Roles: `sports_ddl`, `sports_writer`, `sports_reader` (NOLOGIN groups) + logins `migrator`, `ingestor`, `orchestrator` | Roles: `api_ro` (SELECT on the 5 objects only), `ingest_rw` (SELECT/INSERT/UPDATE), migrations run as the Postgres owner | Collapse 3+3 → 2. Created in goose migration 0002 with `DO $$` guards so `make migrate` is idempotent. |
| Migration runner: bash script + `migrator.schema_migrations` filename table | goose (`goose_db_version` table) | Script deleted. Suggestion: `make migrate` runs `go run github.com/pressly/goose/v5/cmd/goose` against `postgres://postgres:...@localhost:5432/sports` (no new runtime dependency, no separate binary to install — open question Q5). |

Worth reusing from the existing SQL: the `games` CHECK constraints and `(date)` index idea, the `ON CONFLICT DO UPDATE` upserts, and the "roles get grants explicitly, owner runs DDL" separation (now simpler: owner = `POSTGRES_USER`).

Also: the old Docker `pgdata` volume contains the old schema/roles; M1 must start from a fresh volume (`docker compose down -v`) or the entrypoint init/grants won't match.

## 4. Risks and surprises

- **R1 (biggest): no player-stat ingestion exists in the real-data path.** The ESPN and TheSportsDB adapters fetch leagues/teams/rosters/games only. `player_stats` is filled *only* by the synthetic source. There is no box-score code anywhere in the repo (no `fg3m`, no 3p fields, nothing). So "reuse the ESPN parsing" covers scoreboard/teams/rosters, and **M2 must write the ESPN box-score fetch + stat mapping from scratch**. The plan already assumed "fetch games and box scores per date", but this is net-new work, not reuse. Also note the current stat vocabulary is EAV with 6 names (points, rebounds, assists, steals, kills, deaths) — the wide NBA schema changes it completely.
- **R2: this is not a git repository.** The plan says "the `pre-refactor` git tag preserves the code", but there is no `.git`. M1's mass deletion would be irreversible. **Recommendation: `git init` + initial commit before any deletion** (I can do this as the first step of M1, or now with your OK).
- **R3: `.env` drift.** It points `INGESTOR_SOURCES_CONFIG=configs/sources-multisport.yaml` (file does not exist), sets an unused `INGESTOR_APISPORTS_BASE_URL`, and uses model `qwen/qwen3.8-27b` (code/compose default is `llama-3.3-70b-versatile`). Confirms your instinct to never hardcode a model; the new `.env.example` starts clean.
- **R4: corpus is 36 entries, not 28** (17 of them NBA). The Chroma store at `data/chroma/` has one collection built from it. All deleted; ~8 NBA-flavored examples are rewritten by hand for the new wide schema (the old SQL references `sports.leagues` and EAV joins and cannot be reused verbatim).
- **R5: cache key mismatch.** Gateway keys = `sha256(sport|question|max_rows)` — no date, no normalization, includes `max_rows`. Target = `ask:<YYYY-MM-DD>:<normalized question>`, API is sole owner. Entirely new code (the plan accounts for this).
- **R6: the LLM client is the Python `openai` SDK**, single model, `max_retries=0`, **no 429/`retry-after` handling, no token-usage logging at all**. The Go client is written new per spec (timeouts, 429-aware, usage logged, two models).
- **R7: no "answer" step anywhere** — confirmed. The pipeline ends at rows; nothing converts rows to a sentence. (Your summary was right; the gap is real and is the M3/M4 work.)
- **R8: two separate Go modules** (`gateway`, `ingestor`) with different dep sets, each `go 1.27.2` (toolchain installed: 1.27.2 ✓). M1 merges into one module. New deps for the merged module: `pgx/v5` (already a dep of ingestor), `go-redis/v9` (already a dep of gateway) — so no genuinely *new* third-party runtime deps beyond those two; `gopkg.in/yaml.v3` only for the M7 eval runner (already used by the ingestor — Q6).
- **R9: season semantics change.** Today `games.season` is a text label from ESPN config params ("2025", "2026-spring"). Target: derived from the game date (October rule), "current season" = latest season with data. New derivation code in the ingestor; no existing code to borrow.
- **R10: DNP / minutes handling does not exist yet** — no minutes column anywhere, no DNP filter. New in M1 (schema) + M2 (ingest) per the plan's `minutes` NULL rule.
- **R11: `docker-entrypoint-initdb.d` only runs on an empty volume** — the current role/login split between `roles.sql` and `user.sh` is fragile for exactly this reason; moving roles into goose migrations removes the footgun.
- **R12: minor dead config.** `Makefile` `ask` defaults to `SPORT=soccer`; `seasons` target queries `ingest.watermarks`; `launch` advertises a background ingest daemon; `forget` flushes Redis. All vanish in the rewrite. `ingestor/configs/sources.yaml` (synthetic-only) and `sources-thesportsdb.yaml` go away with the dirs. `README.md` is 0 bytes.
- **R13: target layout name.** The plan shows `sportsql/` as the new root. Assumption: the repo root itself becomes the single Go module (module path `sportsql`), i.e. `sports-analytics/` contains `cmd/`, `internal/`, `migrations/`, `eval/` directly. Confirm (Q1).

## 5. Proposed ordered plan (adjusted to findings)

**M0** — this report. *(done, awaiting approval)*

**M1 — Cleanup + skeleton** (adjusted):
1. `git init`, commit current state as baseline (restores the `pre-refactor` safety net the plan assumed; tag it `pre-refactor`).
2. Delete: `orchestrator/`, `contract/`, `gateway/`, `ingestor/`, `data/` (entire tree), `docker-compose.test.yml`, old `Makefile`/`.env.example`.
3. Create the single-module layout at the repo root: `go.mod` (module `sportsql`), `cmd/api/` (boots, serves placeholder UI + `POST /ask` → 501 "not yet"), `cmd/ingest/` (boots, prints usage), `internal/{cache,llm,prompt,sqlguard,store,source/espn}` stubs, `migrations/{0001_schema.sql,0002_roles.sql,0003_views.sql}` (new tables incl. wide `player_game_stats`, `meta`, `api_ro`/`ingest_rw`, `player_season_totals` view), `eval/` (empty).
4. New `docker-compose.yml`: postgres:17 + redis:8 only, fresh `pgdata` volume. New `Makefile`: `up`, `down`, `migrate`, `ingest`, `run`, `eval`, `test`. New `.env.example` with the 8 target vars.
5. Port the reusable §2 pieces as we create each package (don't port anything unused yet).
   *Verify:* `make up && make migrate`, `psql` role/privilege spot-checks (including "api_ro cannot INSERT"), `go build ./... && go vet ./... && go test ./...`, `make run` boots and serves `/`.

**M2 — Ingest CLI + ESPN source** (adjusted: this is where the real new work is, per R1):
1. `internal/source/espn`: interface `GamesOn(date)`, `BoxScore(gameID)` over the ESPN site/core APIs; wide stat mapping (incl. 3p made/att); season derivation (October rule); minutes NULL for DNP.
2. `cmd/ingest`: `--from`/`--to` (default `max(game_date) - 2 days`), idempotent upserts, `meta.data_as_of` update, short sleep between requests, exit.
3. Verify per plan: ~2 weeks of data, raw-SQL "most 3PT attempts this season", spot-check stat lines against a public page, and the historical-date check (one Feb 2022 box score).

**M3 — `/ask` end to end, no cache**: prompt builder (static, ~8 examples), LLM client (two models, 429-aware, usage logging), guard (initially minimal), executor (ro pool, READ ONLY tx, 5 s statement timeout, outer LIMIT 200), answer step, empty-result short-circuit. Must work from `curl`.

**M4 — SQL guard + single repair retry**: full keyword/function blocklist, table-driven tests with hostile inputs, one retry feeding the error back.

**M5 — Redis cache**: normalization, `ask:<date>:<q>` key, ~6 h TTL, `cached` flag, no LLM call on hit.

**M6 — Embedded HTML UI**: question box, answer area, collapsible SQL + rows.

**M7 — Eval + tuning**: `eval/questions.yaml` (~10 questions with expected answers), throttled runner with `-n`; widen ingest to the full current season; prompt tuning.

### Open questions (answer before/during M1)

- **Q1.** Confirm the repo root becomes the `sportsql` module root (plan's target layout), not a subdirectory.
- **Q2.** `player_game_stats` column set — proposal: `minutes, points, rebounds, assists, steals, blocks, turnovers, fg_made, fg_att, three_made, three_att, ft_made, ft_att`. Anything else?
- **Q3.** Keep `conference`/`division` on `teams` (useful for questions; ESPN team listing can carry them) or drop for minimalism?
- **Q4.** OK to `git init` + commit the current tree as the `pre-refactor` baseline before M1 deletes anything?
- **Q5.** `make migrate` via `go run github.com/pressly/goose/v5/cmd/goose` (no goose binary install, no runtime dep) — OK?
- **Q6.** Eval runner may parse `questions.yaml` with `gopkg.in/yaml.v3` (already a dep of the old ingestor). OK, or do you want JSON instead?
