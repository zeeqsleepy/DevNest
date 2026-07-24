# DevNest build tasks.
#
# This is the canonical definition of every development task. Scripts in
# scripts/ are convenience wrappers around these targets, not alternatives
# to them.
#
# Works on Windows (Git Bash, WSL), Linux, and macOS.

BINARY      := devnest
MODULE      := github.com/devnest/devnest
DIST        := dist
REPORTS     := reports

VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

VERSION_PKG := $(MODULE)/internal/version
LDFLAGS     := -s -w \
	-X $(VERSION_PKG).version=$(VERSION) \
	-X $(VERSION_PKG).commit=$(COMMIT) \
	-X $(VERSION_PKG).buildDate=$(BUILD_DATE)

ifeq ($(OS),Windows_NT)
	EXT := .exe
else
	EXT :=
endif

GO       := go
GOFLAGS  := -trimpath
TESTFLAGS := -race -count=1

.DEFAULT_GOAL := help
.PHONY: help setup build run install test test-all test-integration test-e2e \
        bench cover lint fmt vet check tidy verify clean dist-all

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

setup: ## Install dependencies and development tools
	$(GO) mod download
	$(GO) mod verify
	@echo "Development tools are defined in tools/ as a separate module."

build: ## Build the binary into dist/
	@mkdir -p $(DIST)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)$(EXT) ./cmd/$(BINARY)
	@echo "built $(DIST)/$(BINARY)$(EXT) ($(VERSION))"

run: build ## Build and run; pass arguments with ARGS="scan ."
	@$(DIST)/$(BINARY)$(EXT) $(ARGS)

install: ## Install into GOPATH/bin
	$(GO) install $(GOFLAGS) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

test: ## Unit tests: fast, run this constantly
	$(GO) test $(TESTFLAGS) ./...

test-integration: ## Integration tests
	$(GO) test $(TESTFLAGS) -tags=integration ./tests/...

test-e2e: build ## End-to-end tests against the built binary
	$(GO) test $(TESTFLAGS) -count=1 -tags=e2e ./tests/...

test-all: test test-integration test-e2e ## Every test

bench: ## Run benchmarks against committed baselines
	$(GO) test -run='^$$' -bench=. -benchmem ./benchmarks/...

cover: ## Coverage report to reports/coverage.html
	@mkdir -p $(REPORTS)
	$(GO) test $(TESTFLAGS) -coverprofile=$(REPORTS)/coverage.out ./internal/... ./pkg/...
	$(GO) tool cover -html=$(REPORTS)/coverage.out -o $(REPORTS)/coverage.html
	@$(GO) tool cover -func=$(REPORTS)/coverage.out | tail -1

fmt: ## Format all Go source
	$(GO) fmt ./...

vet: ## Run go vet
	$(GO) vet ./...

lint: vet ## Formatting check, vet, and the linter
	@test -z "$$(gofmt -l . | grep -v '^vendor/')" \
		|| (echo "not gofmt-clean:"; gofmt -l . | grep -v '^vendor/'; exit 1)
	@command -v golangci-lint >/dev/null 2>&1 \
		&& golangci-lint run --config tools/golangci.yml ./... \
		|| echo "golangci-lint not installed, skipping"

check: lint test ## What CI runs: run before pushing

tidy: ## Tidy and verify the module graph
	$(GO) mod tidy
	$(GO) mod verify

verify: ## Verify dependency checksums
	$(GO) mod verify

dist-all: ## Cross-compile every release target
	@mkdir -p $(DIST)
	@for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
			-o $(DIST)/$(BINARY)-$$os-$$arch$$ext ./cmd/$(BINARY); \
	done

# GoReleaser is a build tool, not a dependency: it is fetched on demand and
# never enters the module graph. The version is pinned so that a release built
# today and the same tag rebuilt next year go through the same pipeline.
GORELEASER := go run github.com/goreleaser/goreleaser/v2@v2.17.0

release-check: ## Validate the release configuration
	$(GORELEASER) check

release-snapshot: ## Build every release artifact locally, publishing nothing
	$(GORELEASER) release --snapshot --clean

clean: ## Remove build output and reports
	rm -rf $(DIST)
	rm -f $(REPORTS)/coverage.out $(REPORTS)/coverage.html
