# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

```bash
# Build and run
go build -o opencode-chat main.go message_parts.go
./opencode-chat -port 8080

# Development with live reload
go run main.go message_parts.go -port 8080

# Run all tests (organized by prefix)
go test -v -timeout 60s

# Run specific test categories
go test -v -run "unit_" -timeout 60s     # Unit tests only
go test -v -run "integration_" -timeout 60s  # Integration tests only

# Run single test
go test -v -run TestServerStartup -timeout 60s

# Test with coverage
go test -v -cover -timeout 60s

# Build for deployment
go build -ldflags="-s -w" -o opencode-chat main.go message_parts.go
```

## Architecture Overview

This is a **web-based chat interface for OpenCode** built with Go, HTMX 2, and Server-Sent Events. The architecture centers around **subprocess management**, **session bridging**, and **real-time streaming**.

### Core Components

**`main.go`** - Primary server containing:
- `Server` struct: Manages OpenCode subprocess, sessions, providers, templates
- HTTP handlers: `/`, `/send`, `/events` (SSE), `/clear`, `/messages`, `/models`
- OpenCode subprocess lifecycle: `startOpencodeServer()`, `stopOpencodeServer()`, `verifyOpencodeIsolation()`
- Security isolation: Each OpenCode instance runs in isolated temporary directory
- Graceful shutdown: Signal handling with proper cleanup

**`message_parts.go`** - SSE streaming utilities:
- `MessagePartsManager`: Manages chronological ordering of message parts during streaming
- `ValidateAndExtractMessagePart()`: Parses OpenCode SSE events for session-specific filtering
- Single-goroutine design: Each SSE connection gets its own manager instance

### Critical Architecture Patterns

**Session Bridging**: Browser cookies map to OpenCode session IDs. The server maintains this mapping in `sessions map[string]string` and creates OpenCode sessions on-demand via `/session` endpoint.

**Process Isolation**: Each server instance spawns OpenCode in an isolated temporary directory (`/tmp/opencode-chat-pid{PID}-*`) for security. The `verifyOpencodeIsolation()` function ensures OpenCode cannot access the parent directory.

**SSE Event Filtering**: OpenCode provides a global `/event` endpoint streaming ALL sessions. The client-side filtering happens in `handleSSE()` by parsing each event's `sessionID` and only forwarding relevant events.

**Template Architecture**: Uses `embed.FS` for templates and static files. The `NewServer()` constructor parses templates with helper functions. Two key templates:
- `index.html`: Main page with HTMX-enabled form and SSE connection
- `message.html`: Reusable partial for message bubbles (used in streaming and static rendering)

**Port Strategy**: HTTP port + 1000 = OpenCode port (predictable, avoids conflicts)

## Key Implementation Details

**Graceful Shutdown**: The server handles SIGINT/SIGTERM by:
1. Shutting down HTTP server with 5s timeout
2. Sending SIGINT to OpenCode subprocess  
3. Waiting 2s for graceful exit, then force-kill
4. Cleaning up temporary directory
5. Using defer cleanup to handle panics

**HTMX Integration**: All endpoints return HTML fragments (never JSON):
- `POST /send`: Returns message bubble HTML for immediate UI update
- `GET /models`: Returns `<option>` elements for dynamic model dropdown
- `GET /events`: SSE endpoint that streams HTML fragments with `hx-swap-oob` for live updates

**Test Organization**: Tests are prefixed for clear categorization:
- `unit_*_test.go`: Pure Go logic testing (server, sessions, message parts)
- `integration_*_test.go`: Full-stack tests with OpenCode subprocess (HTML parsing, SSE, isolation)
- `integration_common_test.go`: Shared test utilities with polling helpers (`WaitForOpencodeReady`, `WaitForMessageProcessed`)

**Error Handling**: Uses polling instead of fixed waits. The `waitForOpencodeReady()` function polls `/session` endpoint until OpenCode is responsive or timeout occurs.

## Security Considerations

- OpenCode runs in isolated temporary directories with PID in name for uniqueness
- `verifyOpencodeIsolation()` confirms OpenCode's working directory matches expected isolation
- Session cookies are HttpOnly and path-scoped
- No client-side secrets or API keys (OpenCode handles authentication)
- Graceful cleanup prevents temporary directory leakage

## Testing Guidelines

All tests use dynamic port allocation (`GetTestPort()`) to avoid conflicts. Integration tests properly wait for OpenCode readiness using polling instead of fixed sleeps. The `StartTestServer()` helper handles the full OpenCode startup sequence for integration tests.

Critical for testing: OpenCode subprocess must be properly cleaned up in test cleanup to prevent orphaned processes. Use `defer server.stopOpencodeServer()` in all integration tests.