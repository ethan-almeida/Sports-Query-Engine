#!/bin/bash

# Author: Ethan D'Almeida

# This script runs once at postgres container initialisation, after roles.sql has executed.
# It creates one login user per service and maps it to its group role.
# Migrator -> sports_ddl
# Orchestrator -> sports_reader
# Ingester -> sports_writer

set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
    CREATE ROLE migrator WITH LOGIN PASSWORD '${MIGRATOR_DB_PASSWORD:-dev_migrator}' IN ROLE sports_ddl;
    CREATE ROLE ingestor WITH LOGIN PASSWORD '${INGESTOR_DB_PASSWORD:-dev_ingestor}' IN ROLE sports_writer;
    CREATE ROLE orchestrator WITH LOGIN PASSWORD '${ORCHESTRATOR_DB_PASSWORD:-dev_orchestrator}' IN ROLE sports_reader;
EOSQL

