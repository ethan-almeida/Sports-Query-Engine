-- teams.sql
-- Author: Ethan D'Almeida

-- This file contains the schema for sports teams and the leagues they are in

SET ROLE sports_ddl;

CREATE SCHEMA sports;

CREATE TABLE sports.leagues (
    code text PRIMARY KEY,
    name text NOT NULL,
    sport text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sports.teams (
    league_code text NOT NULL REFERENCES sports.leagues(code),
    code text NOT NULL, 
    name text NOT NULL,
    abbreviation text,
    conference text,
    division text,
    external_id text,
    source text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(league_code, code)
);

GRANT USAGE ON SCHEMA sports TO sports_reader, sports_writer;
GRANT SELECT ON sports.leagues, sports.teams TO sports_reader;
GRANT SELECT, INSERT, UPDATE ON sports.leagues, sports.teams TO sports_writer;