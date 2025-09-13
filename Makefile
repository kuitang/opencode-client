# Simple helpers for development

BIN ?= opencode-chat
PKG ?= ./...

.PHONY: build run test test-verbose test-race cover fmt vet lint check clean help \
	 test-fast test-flow test-race-signal test-unit

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

## test-fast: Run mocked HTTP/SSE tests only (no Docker required)
# These tests use httptest + StaticURLSandbox and should be fast/deterministic.
test-fast:
	go test -v -run '^(TestHTTPEndpointWithRichContent|TestHTTPEndpointWithTodoWrite|TestParityBetweenSSEAndHTTP|TestHTTPEndpointErrorHandling|TestSSEEndpoint|TestSSEFiltersBySession|TestSSEStreamsToolOutput)$$' .

## test-unit: Run consolidated unit tests only (no Docker)
test-unit:
	go test -v -run '^(TestSSE(MessagePartNoDuplication|MultiplePartTypes|HTMLGenerationNoDuplication|RapidUpdates)|TestUpdateRateLimiter.*|TestWaitForOpencodeReady.*|TestMessageParts.*|TestNewLoggingResponseWriter|TestLogging.*|TestRenderTodoList|TestFileDropdown.*|TestStreamingCSS|TestTemplate.*|TestTransform.*|TestMessage(Formatting|TemplateIDs|Metadata|PartDataSecurity|UserAndLLM|Multiline))$$' .

## test-flow: Run regular flow tests (requires Docker + auth.json)
test-flow:
	go test -v -run '^(TestIndexPage|TestSendMessage|TestSSEStreaming|TestClearSession|TestGetMessages|TestProviderModelSelection|TestHTMXHeaders)$$' .

## test-race-signal: Run race/signal tests (requires Docker; signal tests build+run app)
test-race-signal:
	go test -v -run '^(TestConcurrentSessionCreation|TestRaceConditionDoubleCheckedLocking|TestStopOpencodeServerGoroutineCleanup|TestSSEContextCancellation|TestSSEMultipleClientDisconnects|TestSignalHandling|TestOpencodeCleanupOnSignal|TestTraceAuth)$$' .

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
	@echo "  test-fast    Run mocked HTTP/SSE tests only (no Docker)"
	@echo "  test-flow    Run regular flow tests (Docker)"
	@echo "  test-race-signal Run race/signal tests (Docker)"
	@echo "  cover        Run coverage summary"
	@echo "  fmt          Format code"
	@echo "  vet          Static analysis"
	@echo "  check        fmt + vet + race tests"
	@echo "  clean        Remove binary"
