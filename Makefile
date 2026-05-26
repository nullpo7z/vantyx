SHELL := /bin/bash

GO          ?= go
GOLANGCI    ?= golangci-lint
NPM         ?= npm
DOCKER      ?= docker

# Optional / advanced targets only fire when the corresponding tool is
# on PATH. This keeps `make lint` / `make test` working in lean
# environments without surprising contributors.

.PHONY: help all fmt fmt-go fmt-fe lint lint-go lint-fe test test-go test-fe e2e coverage smoke build build-go build-fe build-docker security security-trivy security-gosec ci-all clean

help:
	@echo "Vantyx Makefile targets:"
	@echo ""
	@echo "  make fmt           Format Go and (when present) frontend sources"
	@echo "  make lint          Run golangci-lint and the web ESLint rules"
	@echo "  make test          Run Go unit tests (with coverage gates)"
	@echo "  make e2e           Run the Playwright end-to-end suite"
	@echo "  make coverage      Run Go tests and write coverage.out / coverage_core.out"
	@echo "  make smoke         Build the Docker image and run the smoke check"
	@echo "  make build         Build the Vantyx Go binaries and the SPA bundle"
	@echo "  make security      Run trivy + gosec scans (skipped when missing)"
	@echo ""
	@echo "  make ci-all        Run lint + tests + security like CI does"

all: ci-all

# ---------------------------------------------------------------------------
# Formatting
# ---------------------------------------------------------------------------

fmt: fmt-go fmt-fe

fmt-go:
	@$(GO) fmt ./...
	@$(GO) vet ./...

fmt-fe:
	@if [ -d web ]; then \
		cd web && $(NPM) run lint -- --fix || true; \
	else \
		echo "web directory not present, skipping frontend format"; \
	fi

# ---------------------------------------------------------------------------
# Linting
# ---------------------------------------------------------------------------

lint: lint-go lint-fe

lint-go:
	@$(GOLANGCI) run --config=./configs/golangci.yml --issues-exit-code=1

lint-fe:
	@if [ -d web ]; then \
		cd web && $(NPM) run lint; \
	else \
		echo "web directory not present, skipping frontend lint"; \
	fi

# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

test: test-go

test-go:
	@$(GO) test ./...

test-fe:
	@if [ -d web ]; then \
		cd web && $(NPM) run lint; \
	else \
		echo "web directory not present, skipping frontend tests"; \
	fi

e2e:
	@if [ -d e2e ]; then \
		cd e2e && $(NPM) test; \
	else \
		echo "e2e directory not present, skipping end-to-end tests"; \
	fi

# Coverage: keeps the same gates used in CI but always rebuilds the
# coverage files so contributors can iterate locally.
coverage:
	@$(GO) test ./internal/access ./internal/auth ./internal/db/sqlite ./internal/httpapi ./internal/mock ./internal/netutil ./internal/recording ./internal/secret ./internal/session ./internal/sshproxy -covermode=atomic -coverprofile=coverage.out
	@./scripts/check_coverage.sh coverage.out 78
	@$(GO) test ./internal/access ./internal/auth ./internal/db/sqlite ./internal/mock ./internal/netutil ./internal/recording ./internal/secret ./internal/session ./internal/sshproxy -covermode=atomic -coverprofile=coverage_core.out
	@./scripts/check_coverage.sh coverage_core.out 92

# Build the Docker image and run a smoke check against it. Useful before
# cutting a release tag.
smoke:
	@if [ -x scripts/smoke.sh ]; then \
		bash scripts/smoke.sh; \
	else \
		echo "scripts/smoke.sh missing; nothing to do"; \
	fi

# ---------------------------------------------------------------------------
# Builds
# ---------------------------------------------------------------------------

build: build-go build-fe

build-go:
	@$(GO) build ./...

build-fe:
	@if [ -d web ]; then \
		cd web && $(NPM) ci && $(NPM) run build; \
	else \
		echo "web directory not present, skipping frontend build"; \
	fi

build-docker:
	@$(DOCKER) build -t vantyx:dev .

# ---------------------------------------------------------------------------
# Security
# ---------------------------------------------------------------------------

security: security-trivy security-gosec

security-trivy:
	@if command -v trivy >/dev/null 2>&1; then \
		trivy fs . --exit-code 1 --severity HIGH,CRITICAL; \
	else \
		echo "trivy not installed, skipping container scan"; \
	fi

security-gosec:
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "gosec not installed, skipping SAST scan"; \
	fi

# ---------------------------------------------------------------------------
# CI bundle and clean
# ---------------------------------------------------------------------------

ci-all: lint test coverage security

clean:
	@rm -f coverage*.out cov_*.out
	@if [ -d web/dist ]; then rm -rf web/dist; fi
