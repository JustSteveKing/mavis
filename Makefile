# mavis: see `make help`.
#
# Targets carrying a `##` comment are the ones meant to be run by hand; help
# lists exactly those, so a new target documents itself or stays out of the way.

BIN     := bin/mavis
PKG     := .

# Stamped into main.version, so a binary says which build it is rather than
# "dev". The leading "v" is dropped so a local build reads like a release.
VERSION := $(patsubst v%,%,$(shell git describe --tags --always --dirty 2>/dev/null || echo dev))
LDFLAGS := -X main.version=$(VERSION)
GO      ?= go

# The sandbox is a throwaway records folder with its own config and cache,
# so trying mavis never touches your real config or your vault.
SANDBOX ?= $(or $(TMPDIR),/tmp)/mavis-sandbox
SANDBOX_ENV := XDG_CONFIG_HOME=$(SANDBOX)/xdg XDG_CACHE_HOME=$(SANDBOX)/xdg/cache MAVIS_ROOT=$(SANDBOX)/records

.DEFAULT_GOAL := help

.PHONY: build
build: ## Build the binary into bin/
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

.PHONY: install
install: ## Install mavis into GOBIN
	$(GO) install -ldflags '$(LDFLAGS)' $(PKG)

.PHONY: check
check: fmt-check tidy-check vet test-ci ## Everything CI runs

.PHONY: fmt
fmt: ## Format the code
	gofmt -w .

# Checked rather than applied: a check should tell you a file is unformatted,
# not quietly rewrite it underneath you.
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi

# go.mod drifting from the imports is the classic green-locally-red-in-CI
# failure, so it is checked rather than trusted.
.PHONY: tidy-check
tidy-check:
	@cp go.mod go.mod.bak && cp go.sum go.sum.bak
	@$(GO) mod tidy
	@if ! cmp -s go.mod go.mod.bak || ! cmp -s go.sum go.sum.bak; then \
		mv go.mod.bak go.mod; mv go.sum.bak go.sum; \
		echo "go.mod or go.sum is not tidy; run 'go mod tidy'"; exit 1; \
	fi
	@rm -f go.mod.bak go.sum.bak

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: test
test: ## Run the tests
	$(GO) test ./...

.PHONY: test-race
test-race: ## Run the tests under the race detector
	$(GO) test -race ./...

# As CI runs them: the UBL tests must validate against the XSDs with
# xmllint, not skip because it is missing.
.PHONY: test-ci
test-ci:
	MAVIS_REQUIRE_XMLLINT=1 $(GO) test -race ./...

.PHONY: sandbox
sandbox: build ## Seed a throwaway records folder to try mavis in (SANDBOX=path)
	@rm -rf $(SANDBOX)
	@mkdir -p $(SANDBOX)/xdg/mavis
	@printf '%s\n' \
		'business:' \
		'  name: Example Consulting Ltd' \
		'  address: [1 Example Street, Leeds LS1 1AA]' \
		'  vat_number: GB999999973' \
		> $(SANDBOX)/xdg/mavis/config.yaml
	@$(SANDBOX_ENV) $(BIN) init $(SANDBOX)/records >/dev/null
	@$(SANDBOX_ENV) $(BIN) client add acme --name "Acme Ltd" --contact "Jo Bloggs" --email jo@acme.test >/dev/null
	@$(SANDBOX_ENV) $(BIN) client set acme --address "1 High Street" --address "Manchester M1 1AA" >/dev/null
	@$(SANDBOX_ENV) $(BIN) client add globex --name "Globex Corporation" --status warm >/dev/null
	@$(SANDBOX_ENV) $(BIN) client add initech --name "Initech" --status prospect >/dev/null
	@$(SANDBOX_ENV) $(BIN) engagement add acme reporting --title "Reporting module" --basis day --rate 650 --start 2026-01-05 >/dev/null
	@$(SANDBOX_ENV) $(BIN) engagement add acme fixes --title "Bug fixes" --basis hourly --rate 90 --start 2026-01-05 >/dev/null
	@$(SANDBOX_ENV) $(BIN) engagement add globex care --title "Care plan" --basis retainer --rate 800 --start 2026-08-01 >/dev/null
	@$(SANDBOX_ENV) $(BIN) log call acme "Scoped the reporting module; exports by month end." --with "Jo Bloggs" -e acme-reporting -f "Send estimate" --due +3d -f "Share staging access" --due +7d >/dev/null
	@$(SANDBOX_ENV) $(BIN) note globex "Happy with the care plan; may want a redesign next year." >/dev/null
	@$(SANDBOX_ENV) $(BIN) time acme-reporting 1d "Report filters" --date 2026-01-06 >/dev/null
	@$(SANDBOX_ENV) $(BIN) time acme-reporting 1d "Export endpoint" --date 2026-01-07 >/dev/null
	@$(SANDBOX_ENV) $(BIN) time acme-fixes 2h45m "Login bug" --date 2026-01-08 >/dev/null
	@$(SANDBOX_ENV) $(BIN) invoice new acme --month 2026-01 >/dev/null
	@$(SANDBOX_ENV) $(BIN) invoice issue acme-2026-01 --date 2026-02-01 >/dev/null
	@$(SANDBOX_ENV) $(BIN) quote new initech --title "Reporting rebuild" --scope "A rebuilt reporting module." --line "Build=10 x 650 day" >/dev/null
	@$(SANDBOX_ENV) $(BIN) quote send initech >/dev/null
	@echo "Sandbox ready at $(SANDBOX). Try it with:"
	@echo
	@echo "  export $(SANDBOX_ENV)"
	@echo "  $(BIN) today"

.PHONY: snapshot
snapshot: ## Build every release target locally, without publishing
	$(GO) run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean

.PHONY: clean
clean: ## Remove build output and the sandbox
	rm -rf bin/ dist/ $(SANDBOX)

.PHONY: help
help: ## Show this help
	@echo "mavis $(VERSION)"
	@echo
	@awk 'BEGIN {FS = ":.*?## "} \
		/^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
