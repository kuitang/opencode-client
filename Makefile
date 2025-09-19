# Simple helpers for development

BIN ?= opencode-chat
PKG ?= ./...

.PHONY: build run test test-verbose test-race cover fmt vet lint check clean help \
	 test-unit test-integration test-ui

## build: Compile the binary to ./$(BIN)
build:
	go build -o $(BIN) .

## run: Start the app from source
run:
	go run .

## test: Run all tests
test:
	go test $(PKG)

## test-verbose: Run tests with -v
test-verbose:
	go test -v $(PKG)

## test-race: Run tests with the race detector
test-race:
	go test -race $(PKG)

## test-unit: Run consolidated unit tests only (no Docker)
test-unit:
	go test -v -run '^TestUnit' ./...

## test-integration: Run consolidated integration tests (requires Docker + auth.json)
test-integration:
	go test -v -run '^TestIntegration' ./...

## test-ui: Run Playwright UI tests (requires Node + Playwright browsers)
test-ui: build
	@bash -lc 'set -euo pipefail; \
	  ./$(BIN) -port 6666 > /tmp/opencode-chat-ui.log 2>&1 & \
	  pid=$$!; trap "kill $$pid" EXIT; \
	  sleep 8; \
	  CI=1 PLAYWRIGHT_ALLOWED_PORTS=6666 PLAYWRIGHT_BASE_URL=http://localhost:6666 npx playwright test'

## cover: Run tests with coverage summary
cover:
	go test -cover $(PKG)

## fmt: Format sources via go fmt
fmt:
	go fmt $(PKG)

## vet: Static checks via go vet
vet:
	go vet $(PKG)

## lint: Alias for vet
lint: vet

## check: Format, vet, and run race tests
check: fmt vet test-race

## clean: Remove built binary
clean:
	rm -f $(BIN)

## help: Show common targets
help:
	@echo "Common targets:"
	@echo "  build        Build $(BIN)"
	@echo "  run          Run from source"
	@echo "  test         Run all tests"
	@echo "  test-verbose Run tests with -v"
	@echo "  test-race    Run tests with race detector"
	@echo "  test-unit    Run consolidated unit tests (no Docker)"
	@echo "  test-integration Run consolidated integration tests (Docker)"
	@echo "  test-ui      Run Playwright UI tests"
	@echo "  cover        Run coverage summary"
	@echo "  fmt          Format code"
	@echo "  vet          Static analysis"
	@echo "  check        fmt + vet + race tests"
	@echo "  clean        Remove binary"
