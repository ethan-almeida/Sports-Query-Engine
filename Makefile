-include .env

GOOSE := go run github.com/pressly/goose/v3/cmd/goose@v3.30.0 -dir migrations
PGURL := postgres://$(or $(POSTGRES_USER),postgres):$(or $(POSTGRES_PASSWORD),postgres)@localhost:5432/$(or $(POSTGRES_DB),sports)

.PHONY: up down migrate ingest run eval test

up:
	docker compose up -d --wait

down:
	docker compose down

migrate:
	$(GOOSE) postgres "$(PGURL)" up

ingest:
	go run ./cmd/ingest $(INGEST_ARGS)

run:
	go run ./cmd/api

eval:
	@echo "eval: not implemented yet (lands in M7)"

test:
	gofmt -l .
	go vet ./...
	go test ./...
