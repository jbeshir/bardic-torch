CONTAINER_ENGINE ?= podman
IMAGE ?= bardic-torch-api
MYSQL_PREFIX ?= .mysql-local

.DEFAULT_GOAL := help

.PHONY: help
help: ## List the make targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-26s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: fmt
fmt: ## Format the code (gofmt + goimports through golangci-lint)
	go tool golangci-lint fmt

.PHONY: fmt-check
fmt-check: ## Fail if any file is not formatted
	go tool golangci-lint fmt --diff

.PHONY: lint
lint: ## Run golangci-lint
	go tool golangci-lint run ./...

.PHONY: lint-openapi
lint-openapi: ## Lint openapi/api.yaml with vacuum
	go tool vacuum lint -d --fail-severity warn -r openapi/ruleset.yaml openapi/api.yaml

.PHONY: test-short
test-short: ## Run the unit tests
	go test -short ./...

.PHONY: vulncheck
vulncheck: ## Report known vulnerabilities reachable from the code (needs network access to vuln.go.dev)
	go tool govulncheck ./...

.PHONY: build
build: ## Build every package and the pinned tools
	go build ./...
	go build tool

.PHONY: test-integration
test-integration: ## Run the integration tests against a throwaway podman mysql:9.7
	scripts/test-integration.sh podman

.PHONY: test-integration-local
test-integration-local: ## Run the integration tests against a throwaway mysqld from MYSQL_PREFIX
	MYSQL_PREFIX=$(MYSQL_PREFIX) scripts/test-integration.sh local

.PHONY: test-integration-external
test-integration-external: ## Run the integration tests against the MySQL at TEST_MYSQL_ADMIN_URL
	scripts/test-integration.sh external

.PHONY: mysql-local-install
mysql-local-install: ## Install mysql-server 9.7 into MYSQL_PREFIX with conda
	conda create -y -p $(MYSQL_PREFIX) -c conda-forge --override-channels mysql-server=9.7.4

.PHONY: generate
generate: ## Regenerate sqlc queries and mockery mocks
	go generate ./...
	go tool mockery

.PHONY: check-generated
check-generated: ## Fail if generated code is out of date
	scripts/check-generated.sh

.PHONY: validate
validate: fmt-check lint lint-openapi test-short build check-generated ## Run every check that needs only Go

.PHONY: db-up
db-up: ## Start the development MySQL that .env.dist points at and wait until it is ready
	CONTAINER_ENGINE=$(CONTAINER_ENGINE) scripts/dev-db.sh up

.PHONY: db-down
db-down: ## Remove the development MySQL and its data
	CONTAINER_ENGINE=$(CONTAINER_ENGINE) scripts/dev-db.sh down

# Both commands load .env themselves, so it must exist; copy it from .env.dist first.
.PHONY: run
run: ## Apply the migrations and run the API with the settings in .env
	@test -f .env || { echo "run: .env is missing; create it with cp .env.dist .env" >&2; exit 1; }
	go run ./cmd/migrate up
	go run ./cmd/app

.PHONY: docker-build
docker-build: ## Build the production image with CONTAINER_ENGINE
	$(CONTAINER_ENGINE) build -t $(IMAGE) .
