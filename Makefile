# Simple helpers for development

BIN ?= opencode-chat
PKG ?= ./...

.PHONY: build run test test-verbose test-race cover fmt vet lint check clean help \
	 test-unit test-integration test-e2e

## build: Compile the binary to ./$(BIN)
build:
	go build -o $(BIN) ./cmd/opencode-chat

## run: Start the app from source
run:
	go run ./cmd/opencode-chat

## test: Run all tests
test:
	go test $(PKG)

## test-verbose: Run tests with -v
test-verbose:
	go test -v $(PKG)

## test-race: Run tests with the race detector
test-race:
	go test -race $(PKG)

## test-unit: Run unit + property tests (no Docker)
test-unit:
	go test -v -timeout 60s -run '^(TestUnit|TestProp)' ./internal/...

## test-integration: Run consolidated integration tests (requires Docker + auth.json)
test-integration:
	go test -v -timeout 300s -run '^TestIntegration' ./internal/...

## test-e2e: Build app, start it, run E2E tests, then stop it
test-e2e:
	@echo "Building app..."
	go build -o $(BIN) ./cmd/opencode-chat
	@echo "Starting app on port 9876..."
	./$(BIN) -port 9876 &
	@APP_PID=$$!; \
	sleep 2; \
	echo "Running E2E tests..."; \
	E2E_BASE_URL=http://localhost:9876 \
	PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=$${PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH:-} \
	go test -v -timeout 120s ./e2e/... ; \
	EXIT_CODE=$$?; \
	echo "Stopping app (PID $$APP_PID)..."; \
	kill $$APP_PID 2>/dev/null || true; \
	wait $$APP_PID 2>/dev/null || true; \
	exit $$EXIT_CODE

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
	@echo "  test-unit    Run unit + property tests (no Docker)"
	@echo "  test-integration Run consolidated integration tests (Docker)"
	@echo "  test-e2e     Build+launch app, run E2E Playwright tests"
	@echo "  cover        Run coverage summary"
	@echo "  fmt          Format code"
	@echo "  vet          Static analysis"
	@echo "  check        fmt + vet + race tests"
	@echo "  clean        Remove binary"
