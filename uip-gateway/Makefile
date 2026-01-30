# UIP Gateway Makefile

.PHONY: all build run test clean fmt lint mock

# Build variables
VERSION ?= 0.1.0
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Go commands
GO := go
GOFMT := gofmt
GOLINT := golangci-lint

# Binary name
BINARY := uip-gateway

all: build

# Build the binary
build:
	@echo "Building $(BINARY)..."
	$(GO) build $(LDFLAGS) -o bin/$(BINARY) ./cmd/gateway

# Run the gateway (production mode)
run: build
	@echo "Running $(BINARY)..."
	./bin/$(BINARY) -config config.yaml

# Run in mock mode (for testing without Clawdbot)
mock: build
	@echo "Running $(BINARY) in mock mode..."
	./bin/$(BINARY) -config config.yaml -mock

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v -race ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Lint code
lint:
	@echo "Linting code..."
	$(GOLINT) run ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin/
	$(GO) clean

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# Install development tools
tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Docker build
docker:
	@echo "Building Docker image..."
	docker build -t uip-gateway:$(VERSION) .

# Generate protocol buffer files (if needed in future)
proto:
	@echo "Generating protobuf files..."
	# protoc --go_out=. --go-grpc_out=. proto/*.proto

# Show help
help:
	@echo "UIP Gateway - Universal Interaction Protocol Gateway"
	@echo ""
	@echo "Usage:"
	@echo "  make build    - Build the binary"
	@echo "  make run      - Build and run the gateway"
	@echo "  make mock     - Run in mock mode (no Clawdbot needed)"
	@echo "  make test     - Run tests"
	@echo "  make fmt      - Format code"
	@echo "  make lint     - Lint code"
	@echo "  make clean    - Clean build artifacts"
	@echo "  make deps     - Download dependencies"
	@echo "  make docker   - Build Docker image"
	@echo "  make help     - Show this help"
