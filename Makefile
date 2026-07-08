# diskcli Makefile

APP      = diskcli
GO       = go
BUILD_DIR = ./dist

# Version info (passed via ldflags at build time).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

.PHONY: all
all: build

.PHONY: build
build:
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(APP) ./cmd/$(APP)

# Cross-compile for multiple platforms.
.PHONY: build-all
build-all:
	GOOS=linux   GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(APP)-linux-amd64   ./cmd/$(APP)
	GOOS=linux   GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(APP)-linux-arm64   ./cmd/$(APP)
	GOOS=darwin  GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(APP)-darwin-amd64  ./cmd/$(APP)
	GOOS=darwin  GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(APP)-darwin-arm64  ./cmd/$(APP)
	GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(APP)-windows-amd64.exe ./cmd/$(APP)
	@echo "✓ All binaries built in $(BUILD_DIR)/"

# ---------------------------------------------------------------------------
# Test & lint
# ---------------------------------------------------------------------------

.PHONY: test
test:
	$(GO) test ./... -count=1

.PHONY: test-cover
test-cover:
	$(GO) test ./... -coverprofile=coverage.out

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: check
check: vet lint test
	@echo "✓ All checks passed"

# ---------------------------------------------------------------------------
# Docker
# ---------------------------------------------------------------------------

.PHONY: docker-build
docker-build:
	docker build -t diskcli .

.PHONY: docker-buildx
docker-buildx:
	docker buildx build --platform linux/amd64,linux/arm64 -t diskcli .

# ---------------------------------------------------------------------------
# Clean
# ---------------------------------------------------------------------------

.PHONY: clean
clean:
	rm -rf $(APP) $(BUILD_DIR) coverage.out

# ---------------------------------------------------------------------------
# Install
# ---------------------------------------------------------------------------

.PHONY: install
install:
	$(GO) install -ldflags="$(LDFLAGS)" ./cmd/$(APP)

.DEFAULT_GOAL := all
