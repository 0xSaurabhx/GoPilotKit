.PHONY: build build-small build-dev test vet lint clean run validate

# Binary name and output directory
BINARY_NAME := gopilotkit
BIN_DIR := bin
MODULE := github.com/0xsaurabhx/GoPilotKit

# Go build settings — zero CGO, stripped, trimmed
GOFLAGS := CGO_ENABLED=0
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"
TRIMPATH := -trimpath

# Default target
all: build

## build: Compile the binary (stripped, no debug symbols)
build:
	$(GOFLAGS) go build $(LDFLAGS) $(TRIMPATH) -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/engine/

## build-small: Build and compress with UPX for minimal binary size
build-small: build
	@echo "Before UPX:"
	@ls -lh $(BIN_DIR)/$(BINARY_NAME) | awk '{print $$5}'
	@which upx > /dev/null 2>&1 && upx --best --lzma $(BIN_DIR)/$(BINARY_NAME) || echo "UPX not installed, skipping compression"
	@echo "After UPX:"
	@ls -lh $(BIN_DIR)/$(BINARY_NAME) | awk '{print $$5}'

## build-dev: Build without stripping (for debugging)
build-dev:
	CGO_ENABLED=0 go build -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/engine/

## run: Build and run the engine with default config
run: build
	./$(BIN_DIR)/$(BINARY_NAME) run --config configs/enterprise.yaml

## validate: Validate the enterprise config
validate: build
	./$(BIN_DIR)/$(BINARY_NAME) validate --config configs/enterprise.yaml

## test: Run all tests
test:
	go test -v -count=1 ./...

## test-cover: Run tests with coverage
test-cover:
	go test -v -count=1 -coverprofile=coverage.txt -covermode=atomic ./...
	go tool cover -html=coverage.txt -o coverage.html

## vet: Run go vet
vet:
	go vet ./...

## lint: Run staticcheck (if installed)
lint:
	@which staticcheck > /dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed"

## clean: Remove build artifacts
clean:
	rm -rf $(BIN_DIR) coverage.txt coverage.html

## help: Show this help message
help:
	@echo "GoPilotKit Build Targets:"
	@echo "  make build        — Build stripped binary"
	@echo "  make build-small  — Build + UPX compress (<5MB)"
	@echo "  make build-dev    — Build with debug symbols"
	@echo "  make run          — Build and run with enterprise config"
	@echo "  make validate     — Validate enterprise config"
	@echo "  make test         — Run all tests"
	@echo "  make test-cover   — Run tests with coverage report"
	@echo "  make vet          — Run go vet"
	@echo "  make lint         — Run staticcheck"
	@echo "  make clean        — Remove build artifacts"
