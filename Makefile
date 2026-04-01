.PHONY: run build up down reset

## Start Postgres (detached)
up:
	docker compose up -d --wait

## Stop Postgres
down:
	docker compose down

## Wipe Postgres volume and restart (fresh DB)
reset:
	docker compose down -v
	docker compose up -d --wait

## Build the binary
build:
	go build -o bin/polymarket-terminal ./cmd

## Run (starts DB first, then the terminal app)
run: up build
	./bin/polymarket-terminal
