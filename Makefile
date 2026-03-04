.PHONY: build run clean install test

# Build binary
build:
	go build -o kubectl-topx

# Build with optimizations
build-optimized:
	go build -ldflags="-s -w" -o kubectl-topx

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
