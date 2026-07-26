.DEFAULT_GOAL := help
SHELL := /bin/bash
COMPOSE := docker compose

.PHONY: help dev up down restart logs ps migrate migrate-down migrate-new \
        test test-api lint lint-arch fmt psql shell-api build

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

dev: ## Start everything and tail the logs — http://localhost:5173
	$(COMPOSE) up -d postgres mailpit
	$(COMPOSE) up --build api web

up: ## Start everything in the background
	$(COMPOSE) up -d --build postgres mailpit api web

down: ## Stop everything and remove the containers
	$(COMPOSE) down

restart: down up ## Restart everything

logs: ## Tail the api and web logs
	$(COMPOSE) logs -f api web

ps: ## Show container status
	$(COMPOSE) ps

migrate: ## Apply pending migrations
	$(COMPOSE) run --rm migrate

migrate-down: ## Roll back the most recent migration
	$(COMPOSE) run --rm migrate sh -c \
	  'goose -dir ./migrations postgres "$$DATABASE_URL" down'

migrate-new: ## Create a migration. make migrate-new NAME=add_users
	@test -n "$(NAME)" || { echo "NAME is required, e.g. make migrate-new NAME=add_users"; exit 1; }
	cd api && goose -dir ./migrations create $(NAME) sql

test: test-api ## Run every test suite

test-api: ## Run the Go tests (needs Docker for testcontainers)
	cd api && go test ./... -count=1

lint: lint-arch ## Run every linter
	cd api && go vet ./...

lint-arch: ## Check the clean-architecture dependency rule
	./scripts/arch-lint.sh

fmt: ## Format the Go code
	cd api && gofmt -w .

psql: ## Open a psql shell against the development database
	$(COMPOSE) exec postgres psql -U hearth -d hearth

shell-api: ## Open a shell inside the api container
	$(COMPOSE) exec api sh

build: ## Build the production images
	docker build --target prod -t hearth-api:latest ./api
	docker build --target prod -t hearth-web:latest ./web
