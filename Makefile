SHELL := /bin/bash
GO    ?= go
BIN   := bin/ui-agent

.PHONY: all build test test-coverage test-integration test-integration-up test-integration-down vet lint lint-go govulncheck lint-no-target-strings ossready release-snapshot smoke form-smoke clean

all: build test

build:
	mkdir -p bin
	$(GO) build -o $(BIN) ./cmd/ui-agent

# Fast path: short-mode unit tests only. Browser/Postgres-dependent tests
# are skipped via `skipWithoutBrowser` and the testcontainers `-short` guard.
# Goal: every package without external dependencies stays >=80%.
test:
	CI=1 $(GO) test -short -count=1 -timeout 5m ./...

# Per-package coverage report. Prints the lowest packages last so it's
# obvious which ones need attention.
test-coverage:
	CI=1 $(GO) test -short -count=1 -timeout 5m -covermode=atomic -coverprofile=coverage.out ./... | tee /tmp/uiauto-cov.log
	@echo
	@echo "=== Per-package coverage (sorted ascending) ==="
	@grep -E 'coverage:' /tmp/uiauto-cov.log | sort -k 5 -g

# Full integration suite. Brings up the docker-compose integration stack
# (Postgres + headless Chrome + OmniParser stub), runs the entire test
# suite without `-short`, and tears the stack down. Coverage from this
# run includes the browser/Postgres-dependent paths.
test-integration:
	./scripts/run-integration-tests.sh

# Bring the integration stack up without running tests. Useful when
# iterating on a single integration test from an editor.
test-integration-up:
	docker compose -f docker-compose.integration.yml up -d
	@echo "POSTGRES_URL=postgres://uiauto:uiauto@127.0.0.1:5440/uiauto_test?sslmode=disable"
	@echo "REMOTE_DEBUG_URL=http://127.0.0.1:9333"
	@echo "OMNIPARSER_URL=http://127.0.0.1:7861"

test-integration-down:
	docker compose -f docker-compose.integration.yml down -v --remove-orphans

vet:
	$(GO) vet ./...

lint: vet lint-no-target-strings

lint-go:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...; \
	fi

govulncheck:
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...; \
	fi

# CI guard: the framework MUST stay generic. Any commit that adds
# target-specific strings (e.g. company names, internal product names) to
# pkg/, cmd/, examples/, or docs/ fails this check. Such material belongs in
# a downstream scenarios repo (see plugin seams), not the framework.
lint-no-target-strings:
	@echo "Scanning for forbidden target-specific strings..."
	@MATCHES=$$(grep -ril -E 'zendesk|amazon[ -]connect|\bz3n\b|cc-nl|lastpass|\bccp\b|\bzaf\b' \
	    pkg/ cmd/ examples/ docs/ 2>/dev/null || true); \
	if [ -n "$$MATCHES" ]; then \
	    echo "ERROR: target-specific strings found in framework code:"; \
	    echo "$$MATCHES"; \
	    exit 1; \
	fi; \
	echo "OK: framework is generic."

smoke: build
	./scripts/run-example-smoke.sh

form-smoke: build
	./scripts/run-form-flow-demo.sh

ossready:
	@test -f LICENSE
	@test -f README.md
	@test -f CONTRIBUTING.md
	@test -f CODE_OF_CONDUCT.md
	@test -f SECURITY.md
	@test -f CHANGELOG.md
	@test -f .github/PULL_REQUEST_TEMPLATE.md
	@test -f .github/dependabot.yml
	@test -f .github/workflows/ci.yml
	@test -f .github/workflows/lint.yml
	@test -f .github/workflows/codeql.yml
	@test -f .github/workflows/integration.yml
	@test -f .github/workflows/release.yml
	@test -f .goreleaser.yml
	@echo "OK: OSS readiness files are present."

release-snapshot:
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --snapshot --clean; \
	else \
		echo "goreleaser is not installed; see https://goreleaser.com/install/"; \
		exit 2; \
	fi

clean:
	rm -rf bin
