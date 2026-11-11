-- roles.sql
-- Author: Ethan D'Almeida

-- This file runs only once i.e., when the postgres container has intitialized an empty data volume
-- These roles are not to be confused with user roles which are created in users.sh file.

-- For use by the migration sub-system. Owns every object created by migrations.
CREATE ROLE sports_ddl WITH NOLOGIN;

-- For use by the ingestion sub-system. Write path for sports tables and ingest state.
CREATE ROLE sports_writer WITH NOLOGIN;

-- For use by the orchestrator sub-system. Read-only analytics.
CREATE ROLE sports_reader WITH NOLOGIN;

GRANT CREATE ON DATABASE sports to sports_ddl;