# Simple helpers for development

BIN ?= opencode-chat
PKG ?= ./...

.PHONY: build run test test-verbose test-race cover fmt vet lint check clean help

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
	@echo "  cover        Run coverage summary"
	@echo "  fmt          Format code"
	@echo "  vet          Static analysis"
	@echo "  check        fmt + vet + race tests"
	@echo "  clean        Remove binary"
