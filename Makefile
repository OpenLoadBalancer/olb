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
	go test -v -count=1 -p 4 -coverprofile=coverage.out -timeout=300s ./...

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

## coverage-check: Verify coverage meets minimum threshold (default 85%)
coverage-check: test
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	THRESHOLD=85; \
	echo "Total coverage: $${COVERAGE}%"; \
	echo "Required threshold: $${THRESHOLD}%"; \
	if [ $$(echo "$$COVERAGE < $$THRESHOLD" | bc -l) -eq 1 ]; then \
		echo "FAIL: Coverage $${COVERAGE}% is below threshold $${THRESHOLD}%"; \
		exit 1; \
	fi; \
	echo "PASS: Coverage check passed"

## coverage-check-packages: Verify per-package coverage meets minimum threshold (default 85%)
coverage-check-packages: test
	@THRESHOLD=85; \
	echo "Checking per-package coverage (threshold: $${THRESHOLD}%)..."; \
	FAIL=0; \
	go tool cover -func=coverage.out | grep -v total | \
		awk -F'/' '{pkg=""; for(i=1;i<=NF-1;i++){if(i>1)pkg=pkg"/";pkg=pkg$$i}} {print pkg, $$NF}' | \
		awk '{cov[$$1]+=$$3; cnt[$$1]++} END {for(p in cov) {printf "%s %.1f\n", p, cov[p]/cnt[p]}}' | \
		sort | \
		while read pkg pct; do \
			IPCT=$$(echo "$$pct" | sed 's/\..*//'); \
			if [ "$$IPCT" -lt "$$THRESHOLD" ] 2>/dev/null; then \
				echo "WARN: $$pkg $${pct}% < $${THRESHOLD}%"; \
			fi; \
		done; \
	echo "Per-package coverage check complete"

## bench: Run benchmarks
bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

## bench-profile: Run benchmarks with CPU and memory profiling per package
bench-profile:
	@echo "Running benchmarks with profiling..."
	@mkdir -p profiles
	@for pkg in $$(go list ./... | grep -v /vendor/); do \
		has_bench=$$(grep -rl "func Benchmark" $$(echo $$pkg | sed 's|github.com/openloadbalancer/olb/||') 2>/dev/null || true); \
		if [ -n "$$has_bench" ]; then \
			name=$$(echo $$pkg | sed 's|/|_|g'); \
			echo "  Profiling $$pkg..."; \
			go test -bench=. -benchmem -count=1 \
				-cpuprofile=profiles/$${name}.cpu.prof \
				-memprofile=profiles/$${name}.mem.prof \
				$$pkg 2>&1 | tail -5; \
		fi; \
	done
	@echo ""
	@echo "Profiles written to profiles/"
	@echo "Analyze with: go tool pprof -top profiles/<name>.cpu.prof"
	@echo "Analyze with: go tool pprof -top profiles/<name>.mem.prof"

## bench-alloc: Run benchmarks with allocation tracking
bench-alloc:
	@echo "Running benchmarks with allocation tracking..."
	go test -bench=. -benchmem -run=^$ ./... 2>&1 | grep -E "(Benchmark|ns/op|allocs)"

## bench-compare: Compare benchmarks between two commits (usage: make bench-compare BASE=main)
bench-compare:
	@echo "Comparing benchmarks against $(BASE)..."
	@which benchstat >/dev/null 2>&1 || (echo "Install benchstat: go install golang.org/x/perf/cmd/benchstat@latest" && exit 1)
	@mkdir -p profiles
	@echo "Running old benchmarks ($$BASE)..."
	@git stash -q 2>/dev/null; git checkout $(BASE) -q 2>/dev/null
	@go test -bench=. -count=5 -run=^$ ./... > profiles/old.txt 2>/dev/null || true
	@git checkout - -q 2>/dev/null; git stash pop -q 2>/dev/null
	@echo "Running new benchmarks (current)..."
	@go test -bench=. -count=5 -run=^$ ./... > profiles/new.txt 2>/dev/null || true
	@echo ""
	@benchstat profiles/old.txt profiles/new.txt

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
