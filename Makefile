.DEFAULT_GOAL := help

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.buildDate=$(BUILD_DATE)

LDFLAGS_RELEASE := $(LDFLAGS) -s -w

EXE_SUFFIX := $(if $(filter windows,$(shell go env GOOS)),.exe,)
BINARY := middleman$(EXE_SUFFIX)
GOPATH_FIRST := $(shell go env GOPATH | sed -E 's/^([A-Za-z]:)?([^;:]*).*/\1\2/')

ROBOREV_SRC ?= $(HOME)/code/roborev
ROBOREV_REF ?= main
AIR_BIN := $(shell if command -v air >/dev/null 2>&1; then command -v air; \
	elif [ -n "$$(go env GOBIN)" ] && [ -x "$$(go env GOBIN)/air$(EXE_SUFFIX)" ]; then printf "%s" "$$(go env GOBIN)/air$(EXE_SUFFIX)"; \
	elif [ -x "$(GOPATH_FIRST)/bin/air$(EXE_SUFFIX)" ]; then printf "%s" "$(GOPATH_FIRST)/bin/air$(EXE_SUFFIX)"; \
	fi)
DEV_LOG_DIR ?= tmp/logs
DEV_BACKEND_LOG ?= $(DEV_LOG_DIR)/backend-dev.log

.PHONY: ensure-embed-dir check-air air-install build build-release install \
        frontend frontend-dev frontend-dev-bun frontend-check api-generate roborev-api-generate \
        dev test test-short test-e2e test-e2e-roborev vet lint nilaway testify-helper-check tidy svelte-skills svelte-skills-sync clean install-hooks help

# Ensure go:embed has at least one file (no-op if frontend is built)
ensure-embed-dir:
	@mkdir -p internal/web/dist
	@test -n "$$(ls internal/web/dist/ 2>/dev/null)" \
		|| echo ok > internal/web/dist/stub.html

# Build the binary (debug, with embedded frontend)
build: frontend
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/middleman

# Build with optimizations (release)
build-release: frontend
	go build -ldflags="$(LDFLAGS_RELEASE)" -trimpath -o $(BINARY) ./cmd/middleman

# Install to ~/.local/bin, $GOBIN, or $GOPATH/bin
install: build-release
	@if [ -d "$(HOME)/.local/bin" ]; then \
		echo "Installing to ~/.local/bin/$(BINARY)"; \
		cp $(BINARY) "$(HOME)/.local/bin/$(BINARY)"; \
	else \
		INSTALL_DIR="$${GOBIN:-$$(go env GOBIN)}"; \
		if [ -z "$$INSTALL_DIR" ]; then \
			INSTALL_DIR="$(GOPATH_FIRST)/bin"; \
		fi; \
		mkdir -p "$$INSTALL_DIR"; \
		echo "Installing to $$INSTALL_DIR/$(BINARY)"; \
		cp $(BINARY) "$$INSTALL_DIR/$(BINARY)"; \
	fi

# Build frontend SPA and copy into embed directory
frontend:
	cd frontend && bun install && bun run build
	rm -rf internal/web/dist
	cp -r frontend/dist internal/web/dist
	printf 'ok\n' > internal/web/dist/stub.html

# Run Vite dev server with dependencies installed (use alongside `make dev`)
frontend-dev:
	./scripts/frontend-dev.sh $(ARGS)

# Run Vite dev server with Bun (use alongside `make dev`)
frontend-dev-bun:
	cd frontend && bun install && bun run dev

# Run TypeScript/Svelte lint and type checks
frontend-check:
	cd packages/ui && bun run typecheck && bun run lint
	cd frontend && bun run typecheck && bun run lint

# Regenerate the checked-in OpenAPI documents and generated clients
api-generate:
	GOCACHE="$${GOCACHE:-/tmp/middleman-gocache}" go run ./cmd/middleman-openapi -out frontend/openapi/openapi.json
	GOCACHE="$${GOCACHE:-/tmp/middleman-gocache}" go run ./cmd/middleman-openapi -out internal/apiclient/spec/openapi.json -version 3.0
	cd packages/ui && bunx openapi-typescript ../../frontend/openapi/openapi.json -o src/api/generated/schema.ts
	printf '%s\n' \
		'/**' \
		' * This file was auto-generated from frontend/openapi/openapi.json.' \
		' * Do not make direct changes to the file.' \
		' */' \
		'' \
		'import createClient, { type ClientOptions } from "openapi-fetch";' \
		'import type { paths } from "./schema";' \
		'' \
		'export function createAPIClient(baseUrl: string, options: Pick<ClientOptions, "fetch" | "querySerializer"> = {}) {' \
		'  return createClient<paths>({ baseUrl, ...options });' \
		'}' \
		> packages/ui/src/api/generated/client.ts
	GOCACHE="$${GOCACHE:-/tmp/middleman-gocache}" go generate ./internal/apiclient/generated

# Regenerate roborev TypeScript client types from checked-in OpenAPI spec
roborev-api-generate:
	cd packages/ui && bunx openapi-typescript src/api/roborev/openapi.json -o src/api/roborev/generated/schema.ts
	@echo "Roborev API types generated"

# Ensure air is installed for backend live reload
check-air:
	@if [ -z "$(AIR_BIN)" ]; then \
		echo "air not found. Install with: make air-install" >&2; \
		exit 1; \
	fi

# Install air for backend live reload
air-install:
	go install github.com/air-verse/air@latest

# Run Go server in dev mode with live reload and API artifact refresh (use alongside `make frontend-dev`)
dev: ensure-embed-dir check-air
	@mkdir -p "$(DEV_LOG_DIR)"
	@echo "backend debug log: $${MIDDLEMAN_LOG_FILE:-$(DEV_BACKEND_LOG)}"
	@echo "backend console log level: $${MIDDLEMAN_LOG_STDERR_LEVEL:-info}"
	@echo "tail with: tail -F $${MIDDLEMAN_LOG_FILE:-$(DEV_BACKEND_LOG)}"
	@if [ -n "$(MIDDLEMAN_CONFIG)" ]; then \
		MIDDLEMAN_LOG_LEVEL="$${MIDDLEMAN_LOG_LEVEL:-debug}" \
		MIDDLEMAN_LOG_FILE="$${MIDDLEMAN_LOG_FILE:-$(DEV_BACKEND_LOG)}" \
		MIDDLEMAN_LOG_STDERR_LEVEL="$${MIDDLEMAN_LOG_STDERR_LEVEL:-info}" \
		"$(AIR_BIN)" -c .air.toml -- -config "$(MIDDLEMAN_CONFIG)" $(ARGS); \
	else \
		MIDDLEMAN_LOG_LEVEL="$${MIDDLEMAN_LOG_LEVEL:-debug}" \
		MIDDLEMAN_LOG_FILE="$${MIDDLEMAN_LOG_FILE:-$(DEV_BACKEND_LOG)}" \
		MIDDLEMAN_LOG_STDERR_LEVEL="$${MIDDLEMAN_LOG_STDERR_LEVEL:-info}" \
		"$(AIR_BIN)" -c .air.toml -- $(ARGS); \
	fi

# Run tests
test: ensure-embed-dir
	go test ./... -v -shuffle=on

# Run fast tests only
test-short: ensure-embed-dir
	go test ./... -short -shuffle=on

# Run integration tests that execute real git commands (excluded from test-short)
test-integration: ensure-embed-dir
	go test -tags integration ./... -v -shuffle=on

# Run full-stack E2E tests (Playwright against real Go server, excludes roborev)
test-e2e: frontend
	GOFLAGS="$${GOFLAGS:+$$GOFLAGS }-buildvcs=false" go build -o ./cmd/e2e-server/e2e-server$(EXE_SUFFIX) ./cmd/e2e-server
	cd frontend && bun run playwright test --config=playwright-e2e.config.ts --project=chromium

# Run roborev e2e tests with Docker (ROBOREV_SRC, ROBOREV_REF, ROBOREV_PORT configurable)
test-e2e-roborev:
	ROBOREV_SRC="$(ROBOREV_SRC)" ROBOREV_REF="$(ROBOREV_REF)" \
		./scripts/run-roborev-e2e.sh

# Vet
vet: ensure-embed-dir
	go vet ./...

# Enforce testify helper usage for assertion-heavy tests
testify-helper-check: ensure-embed-dir
	GOFLAGS="$${GOFLAGS:+$$GOFLAGS }-buildvcs=false" go run ./cmd/testify-helper-check ./...

# Lint Go code and auto-fix where possible
lint: ensure-embed-dir
	@if ! command -v mise >/dev/null 2>&1; then \
		echo "mise not found. Install with: brew install mise" >&2; \
		exit 1; \
	fi
	GOCACHE="$${GOCACHE:-/tmp/middleman-gocache}" mise exec -- golangci-lint run --fix
	GOFLAGS="$${GOFLAGS:+$$GOFLAGS }-buildvcs=false" go run ./cmd/testify-helper-check ./...

# Run NilAway against first-party Go packages
nilaway: ensure-embed-dir
	@if ! command -v nilaway >/dev/null 2>&1; then \
		echo "nilaway not found. Install with:" >&2; \
		echo "go install go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260318203545-ad240b12fb4c" >&2; \
		exit 1; \
	fi
	@module_path="$$(go list -m)" || { \
		echo "failed to determine module path" >&2; \
		exit 1; \
	}; \
		nilaway -include-pkgs="$$module_path" -test=false ./...

# Tidy dependencies
tidy:
	go mod tidy

# Install or update repo-local Svelte AI skills and per-agent symlinks
svelte-skills:
	python3 scripts/update-svelte-skills.py $(ARGS)

# Alias for explicit sync wording
svelte-skills-sync: svelte-skills

# Install pre-commit and pre-push hooks via prek
install-hooks:
	@if ! command -v prek >/dev/null 2>&1; then \
		echo "prek not found. Install with: brew install prek" >&2; \
		exit 1; \
	fi
	prek install -f

# Clean build artifacts
clean:
	rm -f middleman middleman.exe
	rm -rf internal/web/dist dist/

# Show help
help:
	@echo "middleman build targets:"
	@echo ""
	@echo "  build          - Build with embedded frontend"
	@echo "  build-release  - Release build (optimized, stripped)"
	@echo "  install        - Build and install to ~/.local/bin or GOPATH"
	@echo "  air-install    - Install air live reload tool"
	@echo ""
	@echo "  dev            - Run Go server with air live reload, debug file logs, and info-level console logs"
	@echo "  frontend       - Build frontend SPA"
	@echo "  frontend-dev   - Install deps and run Vite dev server, logging to tmp/logs/frontend-dev.log (honors MIDDLEMAN_CONFIG)"
	@echo "  frontend-dev-bun - Install deps with Bun and run Vite dev server (honors MIDDLEMAN_CONFIG)"
	@echo "  frontend-check - Run TS/Svelte lint and typecheck for frontend and packages/ui"
	@echo "  api-generate   - Regenerate checked-in OpenAPI and TS schema"
	@echo ""
	@echo "  test           - Run all tests"
	@echo "  test-short     - Run fast tests only"
	@echo "  test-e2e       - Run full-stack E2E Playwright tests"
	@echo "  test-e2e-roborev - Run roborev e2e tests with Docker (ROBOREV_SRC, ROBOREV_REF)"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run mise-managed golangci-lint (auto-fix)"
	@echo "  nilaway        - Run NilAway against first-party Go packages"
	@echo "  testify-helper-check - Enforce Assert.New(t) in assertion-heavy Go tests"
	@echo "  tidy           - Tidy go.mod"
	@echo "  svelte-skills  - Sync repo-local Svelte AI skills and per-agent symlinks"
	@echo "  svelte-skills-sync - Alias for svelte-skills"
	@echo ""
	@echo "  install-hooks  - Install pre-commit and pre-push hooks (prek)"
	@echo "  clean          - Remove build artifacts"
