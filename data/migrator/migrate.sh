#!/bin/bash
# data/migrator/migrate.sh
set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is required}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-/migrations}"

echo "migrator: bootstrapping tracking table"
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
SET ROLE sports_ddl;
CREATE SCHEMA IF NOT EXISTS migrator AUTHORIZATION sports_ddl;
CREATE TABLE IF NOT EXISTS migrator.schema_migrations (
    filename     text PRIMARY KEY,
    applied_at   timestamptz NOT NULL DEFAULT now()
);
SQL

shopt -s nullglob
files=("$MIGRATIONS_DIR"/*.sql)
if [ ${#files[@]} -eq 0 ]; then
    echo "migrator: no .sql files in $MIGRATIONS_DIR" >&2
    exit 1
fi

for file in "${files[@]}"; do
    filename=$(basename "$file")
    applied=$(psql "$DATABASE_URL" -tA -v filename="$filename" <<'SQL'
SELECT 1 FROM migrator.schema_migrations WHERE filename = :'filename';
SQL
)
    if [ "$applied" = "1" ]; then
        echo "migrator: skip  $filename"
        continue
    fi
    echo "migrator: apply $filename"
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 --single-transaction -f "$file"
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -v filename="$filename" <<'SQL'
INSERT INTO migrator.schema_migrations (filename) VALUES (:'filename');
SQL
done

echo "migrator: done"