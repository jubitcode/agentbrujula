.PHONY: all build test test-unit test-integration lint clean help

# Build variables
BINARY_NAME=mcp-meta
BUILD_DIR=bin
GO=go

# Default target
all: test build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/mcp-meta

# Run all tests
test:
	@echo "Running all tests..."
	$(GO) test ./... -v

# Run unit tests only (exclude integration)
test-unit:
	@echo "Running unit tests..."
	$(GO) test ./internal/... -v

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	$(GO) test ./test/integration/... -v

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GO) test ./... -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run linter
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	rm -f $(BINARY_NAME)

# Install dependencies
deps:
	@echo "Installing dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Show help
help:
	@echo "Available targets:"
	@echo "  all              - Run tests and build"
	@echo "  build            - Build the binary"
	@echo "  test             - Run all tests"
	@echo "  test-unit        - Run unit tests only"
	@echo "  test-integration - Run integration tests"
	@echo "  test-coverage    - Run tests with coverage report"
	@echo "  lint             - Run linter"
	@echo "  clean            - Clean build artifacts"
	@echo "  deps             - Install dependencies"
	@echo "  fmt              - Format code"
	@echo "  help             - Show this help"
