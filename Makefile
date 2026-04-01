.PHONY: run build up down reset clean migrate

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

# Cleans the binary
clean:
	rm -rf bin

## Apply SQL migrations to the running Postgres container
migrate: up
	@for file in $$(ls migrations/*.sql | sort); do \
		echo "applying $$file"; \
		docker compose exec -T postgres psql -U poly -d polymarket -v ON_ERROR_STOP=1 -f "/docker-entrypoint-initdb.d/$$(basename $$file)"; \
	done

## Build the binary
build:
	go build -o bin/polymarket-terminal ./cmd

## Run (starts DB first, then the terminal app)
run: migrate build
	./bin/polymarket-terminal
