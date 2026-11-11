-- ingest.sql
-- Schema for private ETL progress tracking


SET ROLE sports_ddl;

CREATE SCHEMA ingest;

CREATE TABLE ingest.watermarks (
    source_id  text        NOT NULL,     
    stream     text        NOT NULL,     
    cursor     jsonb       NOT NULL,     
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_id, stream)
);

GRANT USAGE ON SCHEMA ingest TO sports_writer;
GRANT SELECT, INSERT, UPDATE ON ingest.watermarks TO sports_writer;