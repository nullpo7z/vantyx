SHELL := /bin/bash

.PHONY: all lint-go lint-fe test-go security ci-all

all: ci-all

lint-go:
	@golangci-lint run --issues-exit-code=1

lint-fe:
	@if [ -d web ]; then \
		cd web && npm run lint; \
	else \
		echo "web directory not present, skipping frontend lint"; \
	fi

test-go:
	@go test ./...
	@go test ./internal/... -covermode=atomic -coverprofile=coverage.out
	@./scripts/check_coverage.sh coverage.out 85

security:
	@trivy fs . --exit-code 1 --severity HIGH,CRITICAL
	@gosec ./...

ci-all: lint-go lint-fe test-go security

