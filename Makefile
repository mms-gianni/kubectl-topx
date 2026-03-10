.PHONY: build run clean install test

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Build variables
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

# Build binary
build:
	go build -ldflags="$(LDFLAGS)" -o kubectl-topx ./cmd/kubectl-topx

# Build with optimizations
build-optimized:
	go build -ldflags="$(LDFLAGS) -s -w" -o kubectl-topx ./cmd/kubectl-topx

# Run the application
run: build
	./kubectl-topx

# Clean build artifacts
clean:
	rm -f kubectl-topx
	go clean

# Install dependencies
install:
	go mod download
	go mod tidy

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

# Show help
help:
	@echo "Available targets:"
	@echo "  build            - Build the binary"
	@echo "  build-optimized  - Build with size optimizations"
	@echo "  run              - Build and run the application"
	@echo "  clean            - Remove build artifacts"
	@echo "  install          - Install dependencies"
	@echo "  test             - Run tests"
	@echo "  fmt              - Format code"
	@echo "  lint             - Lint code"
	@echo "  help             - Show this help message"
