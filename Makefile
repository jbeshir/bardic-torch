.PHONY: fmt
fmt:
	go tool golangci-lint fmt

.PHONY: fmt-check
fmt-check:
	go tool golangci-lint fmt --diff

.PHONY: lint
lint:
	go tool golangci-lint run ./...

.PHONY: lint-openapi
lint-openapi:
	go tool vacuum lint -d --fail-severity warn -r openapi/ruleset.yaml openapi/api.yaml

.PHONY: test-short
test-short:
	go test -short ./...

.PHONY: build
build:
	go build ./...
	go build tool

MYSQL_PREFIX ?= .mysql-local

.PHONY: test-integration
test-integration:
	scripts/test-integration.sh podman

.PHONY: test-integration-local
test-integration-local:
	MYSQL_PREFIX=$(MYSQL_PREFIX) scripts/test-integration.sh local

.PHONY: test-integration-external
test-integration-external:
	scripts/test-integration.sh external

.PHONY: mysql-local-install
mysql-local-install:
	conda create -y -p $(MYSQL_PREFIX) -c conda-forge --override-channels mysql-server=8.4.2

.PHONY: generate
generate:
	go generate ./...
	go tool mockery

.PHONY: check-generated
check-generated:
	scripts/check-generated.sh

.PHONY: validate
validate: fmt-check lint lint-openapi test-short build check-generated
