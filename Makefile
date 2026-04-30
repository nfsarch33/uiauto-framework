SHELL := /bin/bash
GO    ?= go
BIN   := bin/ui-agent

.PHONY: all build test vet lint lint-no-target-strings smoke clean

all: build test

build:
	mkdir -p bin
	$(GO) build -o $(BIN) ./cmd/ui-agent

test:
	CI=1 $(GO) test -short -count=1 -timeout 5m ./...

test-integration:
	$(GO) test -count=1 -timeout 15m ./...

vet:
	$(GO) vet ./...

lint: vet lint-no-target-strings

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

clean:
	rm -rf bin
