# Assumes a POSIX shell for file operations, so Linux and macOS run it directly
# and Windows users either run it from Git Bash or use the `go` commands listed
# in README.md.
#
# The host platform comes from the Go toolchain rather than $(OS) or uname, so
# the values are the same whether make is started from cmd, PowerShell or Git
# Bash.
#
# VERSION comes from the nearest tag and COMMIT from HEAD; DATE is left empty
# locally so that two machines building the same commit produce the same binary.

GO      ?= go
PKGS    ?= ./...
CMD     := ./cmd/qtogo

HOSTOS  := $(shell $(GO) env GOHOSTOS)
BIN     := bin/qtogo$(shell $(GO) env GOEXE)

# CGO on Windows is built with zig cc: CC="zig cc" CGO_ENABLED=1. The Go race
# detector requires a compatible Windows C toolchain/runtime, so local race
# builds are not part of the Windows workflow; race testing runs in CI.
CGO_ENABLED ?= 1
ifeq ($(HOSTOS),windows)
CC ?= zig cc
else
CC ?= cc
endif
export CGO_ENABLED
export CC

VERSION ?= $(patsubst v%,%,$(shell git describe --tags --abbrev=0 2>/dev/null || echo 0.1.0-dev))
COMMIT  ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
DATE    ?=
LDFLAGS := -s -w \
	-X github.com/nekrozis/qtogo/internal/buildinfo.Version=$(VERSION) \
	-X github.com/nekrozis/qtogo/internal/buildinfo.Commit=$(COMMIT) \
	-X github.com/nekrozis/qtogo/internal/buildinfo.Date=$(DATE)

.PHONY: all build test test-race cov vet fmt fmt-check lint vuln tidy tidy-check check release-check clean help

all: build

build: ## Build a release-shaped binary
	$(GO) build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o $(BIN) $(CMD)

test: ## Run the unit tests (no network)
	$(GO) test -count=1 $(PKGS)

test-race: ## Run the unit tests with the race detector
	$(GO) test -count=1 -race $(PKGS)

cov: ## Write a coverage profile and summarise it
	$(GO) test -count=1 -coverprofile=coverage.out $(PKGS)
	$(GO) tool cover -func=coverage.out

vet: ## Run go vet
	$(GO) vet $(PKGS)

fmt: ## Rewrite sources with gofmt
	gofmt -w .

fmt-check: ## Fail if a file is not gofmt-clean
	@test -z "$$(gofmt -l .)" || { echo "unformatted files:"; gofmt -l .; exit 1; }

lint: ## Run golangci-lint
	golangci-lint run

vuln: ## Run govulncheck
	govulncheck $(PKGS)

tidy: ## Tidy the module files
	$(GO) mod tidy

tidy-check: ## Fail if go.mod or go.sum would change
	$(GO) mod tidy
	@test -z "$$(git status --porcelain -- go.mod go.sum)" || { echo "go.mod or go.sum is not tidy:"; git status --porcelain -- go.mod go.sum; exit 1; }

check: fmt-check vet test lint ## The gate a change has to pass

release-check: check vuln build ## The gate before a release

clean: ## Remove this project's build output
	rm -rf bin coverage.out cover.html

help: ## List the targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS=":.*?## "} {printf "  %-14s %s\n", $$1, $$2}'
