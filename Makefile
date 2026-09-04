.DEFAULT_GOAL := help
SHELL := /bin/bash
COMPOSE := docker compose

.PHONY: help dev dev-local up down restart logs ps urls migrate migrate-down migrate-new \
        test test-api test-web lint lint-arch lint-web typecheck fmt psql shell-api build sqlc \
        seed reset-password unlock-household

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

dev: ## Start everything and tail the logs — app http://localhost:5173, mail http://localhost:8025
	$(COMPOSE) up -d postgres mailpit
	@$(MAKE) --no-print-directory urls
	$(COMPOSE) up --build api web

dev-local: ## Run api and web natively, infra in Docker (Ctrl-C stops both)
	@command -v air >/dev/null 2>&1 || { echo "air is not on PATH; run: go install github.com/air-verse/air@v1.66.1"; exit 1; }
	@test -f .env || { echo ".env is required for dev-local; copy .env.example to .env first"; exit 1; }
	$(COMPOSE) up -d postgres mailpit
	$(MAKE) migrate
	@$(MAKE) --no-print-directory urls
	@set -a; . ./.env; set +a; \
	 trap 'kill 0' EXIT INT TERM; \
	 (cd api && air -c .air.toml 2>&1 | sed 's/^/[api] /') & \
	 (cd web && npm run dev 2>&1 | sed 's/^/[web] /') & \
	 wait

up: ## Start everything in the background
	$(COMPOSE) up -d --build postgres mailpit api web
	@$(MAKE) --no-print-directory urls

# These are host-side Compose port mappings, so they live here rather than in
# the API's own startup log: the api process only knows the addresses it uses on
# the Compose network (it logs "sending mail" with smtp_addr=mailpit:1025), and
# cannot know which host port, if any, Mailpit's UI is published on.
urls: ## Print the local URLs for a running stack
	@echo "  app   http://localhost:5173"
	@echo "  mail  http://localhost:8025   (Mailpit — every outbound mail lands here)"

down: ## Stop everything and remove the containers
	$(COMPOSE) down

restart: down up ## Restart everything

logs: ## Tail the api and web logs
	$(COMPOSE) logs -f api web

ps: ## Show container status
	$(COMPOSE) ps

migrate: ## Apply pending migrations
	$(COMPOSE) run --rm migrate
	$(COMPOSE) run --rm readonly-role

migrate-down: ## Roll back the most recent migration
	$(COMPOSE) run --rm migrate sh -c \
	  'goose -dir ./migrations postgres "$$DATABASE_URL" down'

migrate-new: ## Create a migration. make migrate-new NAME=add_users
	@test -n "$(NAME)" || { echo "NAME is required, e.g. make migrate-new NAME=add_users"; exit 1; }
	$(COMPOSE) run --rm migrate goose -dir ./migrations create $(NAME) sql

sqlc: ## Regenerate the typed queries from SQL
	cd api && go tool sqlc generate

test: test-api test-web ## Run every test suite

test-api: ## Run the Go tests (needs Docker for testcontainers)
	cd api && go test ./... -count=1 -timeout=5m

# A bare `npm install` in web/ needs --legacy-peer-deps (an optional peer
# conflict in @hookform/resolvers); `npm ci` does not need it and is clean.
test-web: ## Run the frontend tests
	cd web && npx vitest run

lint: lint-arch typecheck lint-web ## Run every linter
	cd api && go vet ./...

lint-arch: ## Check the clean-architecture dependency rule
	./scripts/arch-lint.sh

lint-web: ## Lint the frontend
	cd web && npm run lint

typecheck: ## Type-check the frontend, tests included
	cd web && npx tsc --noEmit

fmt: ## Format the Go code
	cd api && gofmt -w . && cd ../web && npx prettier --write src

psql: ## Open a psql shell against the development database
	$(COMPOSE) exec postgres psql -U hearth -d hearth

shell-api: ## Open a shell inside the api container
	$(COMPOSE) exec api sh

seed: ## Seed the design's household and print Christine's invite URL
	$(COMPOSE) exec api go run ./cmd/adminctl seed

reset-password: ## Set a member's password. make reset-password EMAIL=you@example.com
	@test -n "$(EMAIL)" || { echo "EMAIL is required"; exit 1; }
	$(COMPOSE) exec api go run ./cmd/adminctl reset-password --email=$(EMAIL)

unlock-household: ## Clear the seeded household's lock immediately
	$(COMPOSE) exec api go run ./cmd/adminctl unlock-household

build: ## Build the production images
	docker build --target prod -t hearth-api:latest ./api
	docker build --target prod -t hearth-web:latest ./web
