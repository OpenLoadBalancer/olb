.PHONY: all build test lint bench clean install build-all docker help

# Project settings
MODULE := github.com/openloadbalancer/olb
BINARY := olb
VERSION := $(shell git describe --tags --always 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
SHORT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X $(MODULE)/pkg/version.Version=$(VERSION) \
           -X $(MODULE)/pkg/version.Commit=$(COMMIT) \
           -X $(MODULE)/pkg/version.ShortCommit=$(SHORT_COMMIT) \
           -X $(MODULE)/pkg/version.Date=$(DATE)

# Build settings
CGO_ENABLED := 0
GOFLAGS := -trimpath

# Default target
all: build

## build: Build the binary for the current platform
build:
	@echo "Building $(BINARY) $(VERSION)..."
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS) -s -w" -o bin/$(BINARY) ./cmd/olb

## build-debug: Build with debug symbols
build-debug:
	@echo "Building $(BINARY) $(VERSION) (debug)..."
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-debug ./cmd/olb

## build-race: Build with race detector
build-race:
	@echo "Building $(BINARY) $(VERSION) (race)..."
	CGO_ENABLED=1 go build $(GOFLAGS) -race -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-race ./cmd/olb

## build-all: Build for multiple platforms
build-all: build-linux build-darwin build-windows build-freebsd

build-linux:
	@echo "Building for Linux..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS) -s -w" -o bin/$(BINARY)-linux-amd64 ./cmd/olb
	GOOS=linux GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS) -s -w" -o bin/$(BINARY)-linux-arm64 ./cmd/olb

build-darwin:
	@echo "Building for macOS..."
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS) -s -w" -o bin/$(BINARY)-darwin-amd64 ./cmd/olb
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS) -s -w" -o bin/$(BINARY)-darwin-arm64 ./cmd/olb

build-windows:
	@echo "Building for Windows..."
	GOOS=windows GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS) -s -w" -o bin/$(BINARY)-windows-amd64.exe ./cmd/olb
	GOOS=windows GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS) -s -w" -o bin/$(BINARY)-windows-arm64.exe ./cmd/olb

build-freebsd:
	@echo "Building for FreeBSD..."
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS) -s -w" -o bin/$(BINARY)-freebsd-amd64 ./cmd/olb

## test: Run all tests
test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...

## test-short: Run short tests only
test-short:
	@echo "Running short tests..."
	go test -v -short ./...

## coverage: Generate and display coverage report
coverage: test
	@echo "Generating coverage report..."
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## coverage-text: Display coverage in terminal
coverage-text: test
	go tool cover -func=coverage.out

## bench: Run benchmarks
bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

## bench-profile: Run benchmarks with profiling
bench-profile:
	@echo "Running benchmarks with profiling..."
	go test -bench=. -benchmem -cpuprofile=cpu.prof -memprofile=mem.prof ./...

## lint: Run linters (requires golangci-lint)
lint:
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, using go vet..."; \
		go vet ./...; \
	fi

## fmt: Format all Go files
fmt:
	@echo "Formatting..."
	go fmt ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...

## tidy: Tidy and verify module dependencies
tidy:
	@echo "Tidying modules..."
	go mod tidy
	go mod verify

## generate: Run go generate
generate:
	@echo "Running go generate..."
	go generate ./...

## install: Install binary to GOPATH/bin
install: build
	@echo "Installing to $(shell go env GOPATH)/bin..."
	cp bin/$(BINARY) $(shell go env GOPATH)/bin/$(BINARY)

## clean: Remove build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin/ coverage.out coverage.html cpu.prof mem.prof *.test

## docker: Build Docker image
docker:
	@echo "Building Docker image..."
	docker build -t openloadbalancer/olb:$(VERSION) -t openloadbalancer/olb:latest .

## docker-push: Push Docker image
docker-push: docker
	@echo "Pushing Docker image..."
	docker push openloadbalancer/olb:$(VERSION)
	docker push openloadbalancer/olb:latest

## run: Build and run locally
run: build
	./bin/$(BINARY)

## dev: Run in development mode
dev: build-debug
	./bin/$(BINARY)-debug --config configs/olb.yaml --log-level debug

## version: Show version info
version:
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Date: $(DATE)"

## check: Run all checks (fmt, vet, lint, test)
check: fmt vet lint test

## ci: CI pipeline (build, test, lint)
ci: build test lint

## help: Show this help message
help:
	@echo "OpenLoadBalancer Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
