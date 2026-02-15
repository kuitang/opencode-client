# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

```bash
# Build and run
go build -o opencode-chat ./cmd/opencode-chat
./opencode-chat -port 8080

# Development with live reload
go run ./cmd/opencode-chat -port 8080

# Run all tests
go test -v -timeout 60s ./...

# Run specific test categories
make test-unit             # Unit + property tests (no Docker)
make test-integration      # Integration tests (Docker + auth)
make test-e2e              # Build app, launch, run Playwright E2E tests

# Run single test
go test -v -run TestUnitMessageFormatting -timeout 60s ./internal/server/

# Test with coverage
go test -v -cover -timeout 60s ./...

# Build for deployment
go build -ldflags="-s -w" -o opencode-chat ./cmd/opencode-chat
```

## Architecture Overview

This is a **web-based chat interface for OpenCode** built with Go, HTMX 2, and Server-Sent Events. The architecture centers around **sandbox management**, **session bridging**, and **real-time streaming**.

### Project Layout

```
cmd/opencode-chat/main.go          # Entrypoint: flag parsing, sandbox init, signal handling
internal/
  models/types.go                   # Shared API/data types (Provider, MessagePart, etc.)
  sandbox/                          # Sandbox interface + implementations (Docker, Fly.io, test stub)
  auth/auth.go                      # Auth types (AuthSession, AuthContext) + helpers
  middleware/middleware.go           # HTTP middleware (logging, chaining)
  templates/embed.go                # embed.FS for HTML templates + static assets
  views/views.go                    # Rendering: markdown, tool details, message transforms
  sse/parts.go                      # MessagePartsManager + SSE event validation
  server/                           # HTTP server, routes, all handlers
    server.go                       # Server struct, NewServer, rate limiter
    routes.go                       # RegisterRoutes (Go 1.22+ method routing)
    handlers_chat.go                # handleIndex, handleSend, handleSSE, handleClear
    handlers_tabs.go                # Tab handlers (preview, code, terminal, deployment)
    handlers_proxy.go               # Reverse proxies (terminal, preview)
    handlers_files.go               # File content, file list, download, ports
    handlers_auth.go                # Login/logout, auth session management
    opcode_client.go                # OpenCode API client helpers
    session.go                      # Session cookie + workspace session management
```

### Package Dependency Graph (no cycles)

```
models      → (stdlib only)
sandbox     → models
auth        → (stdlib only)
middleware  → (stdlib only)
templates   → (stdlib only, embed)
views       → models, templates
sse         → views
server      → models, sandbox, auth, middleware, views, sse, templates
cmd/main    → server, sandbox
```

### Core Components

**`internal/server/`** - HTTP server containing:
- `Server` struct: Manages OpenCode sandbox, sessions, providers, templates
- HTTP handlers with Go 1.22+ method routing (`"GET /{$}"`, `"POST /send"`, etc.)
- Sandbox lifecycle via `Sandbox` interface (Docker, Fly.io)
- Graceful shutdown: Signal handling with proper cleanup

**`internal/sse/parts.go`** - SSE streaming utilities:
- `MessagePartsManager`: Manages chronological ordering of message parts during streaming
- `ValidateAndExtractMessagePart()`: Parses OpenCode SSE events for session-specific filtering

**`internal/views/views.go`** - Rendering pipeline:
- Markdown rendering with XSS sanitization (blackfriday + bluemonday)
- Tool output rendering with collapsible details
- Message part transformation from API types to rendered HTML

### Critical Architecture Patterns

**Session Bridging**: Browser cookies map to OpenCode session IDs. The server maintains this mapping and creates OpenCode sessions on-demand via `/session` endpoint.

**Sandbox Isolation**: Uses Docker containers (or Fly.io machines) to run OpenCode in isolation. Each container gets its own auth config and port mapping.

**SSE Event Filtering**: OpenCode provides a global `/event` endpoint streaming ALL sessions. The server-side filtering happens in `handleSSE()` by parsing each event's `sessionID` and only forwarding relevant events.

**Template Architecture**: Uses `embed.FS` for templates and static files. Templates live in `internal/templates/templates/` and are loaded via `views.LoadTemplates()`.

## Key Implementation Details

**Graceful Shutdown**: The server handles SIGINT/SIGTERM by:
1. Shutting down HTTP server with 5s timeout
2. Stopping sandbox (Docker container cleanup)
3. Using defer cleanup to handle panics

**HTMX Integration**: All endpoints return HTML fragments (never JSON):
- `POST /send`: Returns message bubble HTML for immediate UI update
- `GET /events`: SSE endpoint that streams HTML fragments with `hx-swap-oob` for live updates

**Test Organization**: Tests live in `internal/server/` and `e2e/`:
- `unit_test.go`: Pure Go logic (rendering, server helpers, concurrency utilities).
- `prop_test.go`: Property-based tests using `pgregory.net/rapid`.
- `integration_test.go`: Full-stack coverage (mocked HTTP/SSE flows, real sandbox flows, race/signal scenarios).
- `e2e/`: Browser-based E2E tests using `playwright-go` (resize, preview, terminal, SSE, layout).

**Error Handling**: Uses polling instead of fixed waits. The `sandbox.WaitForOpencodeReady()` function polls `/session` endpoint until OpenCode is responsive or timeout occurs.

## Security Considerations

- OpenCode runs in isolated Docker containers
- Session cookies are HttpOnly and path-scoped
- No client-side secrets or API keys (OpenCode handles authentication)
- Graceful cleanup prevents container leakage

## Testing Guidelines

### Go Tests

All tests use dynamic port allocation to avoid conflicts. Integration tests properly wait for OpenCode readiness using polling instead of fixed sleeps. The `RealSuiteServer()` helper handles the full sandbox startup sequence for integration tests.

Critical for testing: Sandbox must be properly cleaned up in test cleanup to prevent orphaned containers.
