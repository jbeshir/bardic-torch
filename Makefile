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

.PHONY: validate
validate: fmt-check lint lint-openapi test-short build
