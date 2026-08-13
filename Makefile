# GOBIN - directory for locally installed Go tools, so a contributor's global
# installs and this project's pinned versions cannot drift apart.
GOBIN ?= $(PWD)/bin

# Marker prefix for progress lines.
M = ▶

# Tool versions are pinned: an unpinned linter turns an unrelated upgrade into a
# red build on an untouched branch. Keep in step with .github/workflows/ci.yml.
GOLANGCI_LINT_VERSION ?= v2.12.2
GOVULNCHECK_VERSION   ?= v1.5.0

$(shell mkdir -p $(GOBIN))

.DEFAULT_GOAL := help

# -------------------------------------
# Dependencies
# -------------------------------------

.PHONY: deps
deps: ## Download modules and tidy go.mod
	$(info $(M) downloading dependencies...)
	@go mod download
	@go mod tidy

.PHONY: bin-deps
bin-deps: ## Install pinned dev tools into ./bin
	$(info $(M) installing tools into $(GOBIN)...)
	@GOBIN=$(GOBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@GOBIN=$(GOBIN) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

# -------------------------------------
# Build and test
# -------------------------------------

.PHONY: build
build: ## Compile all packages
	$(info $(M) building...)
	@go build ./...

.PHONY: vet
vet: ## Run go vet
	$(info $(M) running go vet...)
	@go vet ./...

# -shuffle=on so an accidental dependency between tests surfaces here rather
# than as a flake in CI.
.PHONY: test
test: ## Run tests with the race detector
	$(info $(M) running tests...)
	@go test -race -shuffle=on ./...

# Scoped to ./client because that is where the tests live; widening it to ./...
# makes the run depend on the covdata tool for packages that have no tests,
# which fails on a toolchain skew for no benefit.
#
# The per-package percentage printed by `go test` is the summary. For a
# line-level view: go tool cover -html coverage.out
.PHONY: test-cov
test-cov: ## Run tests and write coverage.out
	$(info $(M) running tests with coverage...)
	@go test -race -covermode atomic -coverprofile coverage.out ./client/...

# -------------------------------------
# Quality
# -------------------------------------

.PHONY: lint
lint: ## Run golangci-lint
	$(info $(M) running linter...)
	@$(GOBIN)/golangci-lint run ./...

.PHONY: fmt
fmt: ## Format code and organize imports
	$(info $(M) formatting...)
	@$(GOBIN)/golangci-lint fmt -E gofmt -E goimports ./...

.PHONY: vuln
vuln: ## Scan for known vulnerabilities reachable from this module
	$(info $(M) running govulncheck...)
	@$(GOBIN)/govulncheck ./...

# Mirrors the CI tidy job: fails when go.mod or go.sum is out of date, which is
# the cheap way to catch a dependency added without tidying.
.PHONY: tidy-check
tidy-check: ## Verify go.mod and go.sum are tidy
	$(info $(M) checking go.mod is tidy...)
	@go mod tidy
	@git diff --exit-code go.mod go.sum

# -------------------------------------
# Aggregates
# -------------------------------------

.PHONY: all
all: deps fmt lint vet test ## Format, lint, vet and test

.PHONY: ci
ci: build vet lint test tidy-check vuln ## Everything CI runs

.PHONY: help
help: ## List available targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
