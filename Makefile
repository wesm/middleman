.DEFAULT_GOAL := help

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.buildDate=$(BUILD_DATE)

LDFLAGS_RELEASE := $(LDFLAGS) -s -w

.PHONY: ensure-embed-dir build build-release install frontend frontend-dev dev \
        test test-short vet lint tidy clean help

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
	cd frontend && bun install && bun run build
	rm -rf internal/web/dist
	cp -r frontend/dist internal/web/dist

# Run Vite dev server (use alongside `make dev`)
frontend-dev:
	cd frontend && bun run dev

# Run Go server in dev mode (no embedded frontend)
dev: ensure-embed-dir
	go run -ldflags="$(LDFLAGS)" ./cmd/middleman $(ARGS)

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
	@echo ""
	@echo "  dev            - Run Go server (use with frontend-dev)"
	@echo "  frontend       - Build frontend SPA"
	@echo "  frontend-dev   - Run Vite dev server"
	@echo ""
	@echo "  test           - Run all tests"
	@echo "  test-short     - Run fast tests only"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run golangci-lint (auto-fix)"
	@echo "  tidy           - Tidy go.mod"
	@echo ""
	@echo "  clean          - Remove build artifacts"
