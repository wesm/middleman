.DEFAULT_GOAL := help

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.buildDate=$(BUILD_DATE)

LDFLAGS_RELEASE := $(LDFLAGS) -s -w
AIR_BIN := $(shell if command -v air >/dev/null 2>&1; then command -v air; \
	elif [ -n "$$(go env GOBIN)" ] && [ -x "$$(go env GOBIN)/air" ]; then printf "%s" "$$(go env GOBIN)/air"; \
	elif [ -x "$$(go env GOPATH | cut -d: -f1)/bin/air" ]; then printf "%s" "$$(go env GOPATH | cut -d: -f1)/bin/air"; \
	fi)

.PHONY: ensure-embed-dir check-air air-install build build-release install \
        frontend frontend-dev frontend-dev-bun dev \
        test test-short vet lint tidy clean install-hooks help

# Ensure go:embed has at least one file (no-op if frontend is built)
ensure-embed-dir:
	@mkdir -p internal/web/dist
	@test -n "$$(ls internal/web/dist/ 2>/dev/null)" \
		|| echo ok > internal/web/dist/stub.html

# Build the binary (debug, with embedded frontend)
build: frontend
	go build -ldflags="$(LDFLAGS)" -o middleman ./cmd/middleman
	@chmod +x middleman

# Build with optimizations (release)
build-release: frontend
	go build -ldflags="$(LDFLAGS_RELEASE)" -trimpath -o middleman ./cmd/middleman
	@chmod +x middleman

# Install to ~/.local/bin, $GOBIN, or $GOPATH/bin
install: build-release
	@if [ -d "$(HOME)/.local/bin" ]; then \
		echo "Installing to ~/.local/bin/middleman"; \
		cp middleman "$(HOME)/.local/bin/middleman"; \
	else \
		INSTALL_DIR="$${GOBIN:-$$(go env GOBIN)}"; \
		if [ -z "$$INSTALL_DIR" ]; then \
			GOPATH_FIRST="$$(go env GOPATH | cut -d: -f1)"; \
			INSTALL_DIR="$$GOPATH_FIRST/bin"; \
		fi; \
		mkdir -p "$$INSTALL_DIR"; \
		echo "Installing to $$INSTALL_DIR/middleman"; \
		cp middleman "$$INSTALL_DIR/middleman"; \
	fi

# Build frontend SPA and copy into embed directory
frontend:
	cd frontend && npm install && npm run build
	rm -rf internal/web/dist
	cp -r frontend/dist internal/web/dist

# Run Vite dev server (use alongside `make dev`)
frontend-dev:
	cd frontend && npm run dev

# Run Vite dev server with Bun (use alongside `make dev`)
frontend-dev-bun:
	cd frontend && bun install && bun run dev

# Ensure air is installed for backend live reload
check-air:
	@if [ -z "$(AIR_BIN)" ]; then \
		echo "air not found. Install with: make air-install" >&2; \
		exit 1; \
	fi

# Install air for backend live reload
air-install:
	go install github.com/air-verse/air@latest

# Run Go server in dev mode with live reload (use alongside `make frontend-dev`)
dev: ensure-embed-dir check-air
	"$(AIR_BIN)" -c .air.toml -- $(ARGS)

# Run tests
test: ensure-embed-dir
	go test ./... -v -count=1

# Run fast tests only
test-short: ensure-embed-dir
	go test ./... -short -count=1

# Vet
vet: ensure-embed-dir
	go vet ./...

# Lint Go code and auto-fix where possible
lint: ensure-embed-dir
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint not found. Install with: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest" >&2; \
		exit 1; \
	fi
	golangci-lint run --fix ./...

# Tidy dependencies
tidy:
	go mod tidy

# Install pre-commit hooks via prek
install-hooks:
	@if ! command -v prek >/dev/null 2>&1; then \
		echo "prek not found. Install with: brew install prek" >&2; \
		exit 1; \
	fi
	prek install -f

# Clean build artifacts
clean:
	rm -f middleman
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
	@echo "  dev            - Run Go server with air live reload (use with frontend-dev)"
	@echo "  frontend       - Build frontend SPA"
	@echo "  frontend-dev   - Run Vite dev server"
	@echo "  frontend-dev-bun - Install deps with Bun and run Vite dev server"
	@echo ""
	@echo "  test           - Run all tests"
	@echo "  test-short     - Run fast tests only"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run golangci-lint (auto-fix)"
	@echo "  tidy           - Tidy go.mod"
	@echo ""
	@echo "  install-hooks  - Install pre-commit hooks (prek)"
	@echo "  clean          - Remove build artifacts"
