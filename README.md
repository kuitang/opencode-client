# OpenCode Chat Client

A web-based chat interface for OpenCode using Go and HTMX.

## Project Structure

```
.
├── main.go                          # Main server application
├── templates/                       # HTML templates
│   ├── index.html                  # Main page layout
│   └── message.html                # Reusable message bubble partial
├── static/                         # Static assets
│   └── styles.css                 # CSS styles
├── integration_test.go             # Consolidated integration suites (mocked + real sandbox)
├── test_suite_helpers.go           # Per-file suite server helper (RealSuiteServer)
├── suite_main_test.go              # Global cleanup for suite servers
├── unit_test.go                    # Consolidated unit tests
└── go.mod                          # Go module dependencies
```

## Features

- **HTMX 2 + SSE**: Real-time message streaming using Server-Sent Events
- **Cookie-based sessions**: Each browser session maps to an OpenCode session
- **Dynamic model selection**: Switch providers and models between messages
- **Template-based rendering**: Clean separation of HTML/CSS from Go code
- **Session management**: Clear and reset sessions with a button click
- **Subprocess management**: Automatically spawns and manages OpenCode server

## Architecture

- Go standard library with embedded templates
- HTMX 2 for interactivity (no custom JavaScript)
- Server-side rendering with reusable templates
- Embedded static files for easy deployment
- Automatic OpenCode server subprocess management

## Usage

```bash
# Build the application
go build -o opencode-chat main.go

# Run with default port (8080 for web, 9080 for opencode)
./opencode-chat

# Run with custom port
./opencode-chat --port 9000  # Will use 9000 for web, 10000 for opencode
```

## API Endpoints

All endpoints return HTML fragments for HTMX:

- `GET /` - Main chat interface
- `POST /send` - Send a message (returns message bubble HTML)
- `GET /events` - SSE endpoint for streaming responses
- `POST /clear` - Clear current session
- `GET /messages` - Get all messages for current session
- `GET /models?provider=X` - Get model options for provider X

## Running

```bash
# Run with default port (8080 for web, 9080 for OpenCode)
go run main.go

# Run with custom port
go run main.go -port 3000  # Will use 3000 for web, 4000 for OpenCode

# Build and run
go build -o opencode-chat
./opencode-chat -port 8080
```

## Testing

There are two categories of tests:

- Unit tests (no Docker): `unit_test.go` — rendering, server helpers, concurrency primitives
- Integration tests (Docker when required): `integration_test.go` — mocked HTTP/SSE flows, real sandbox flows, and race/signal coverage

Integration suites start exactly one real sandbox/server per file via `RealSuiteServer`, so tests within a file do not repeatedly start Docker.

### Commands

```bash
# All tests (about ~30s on a warm Docker)
go test -v ./...

# Unit tests only (no Docker; consolidated)
make test-unit

# Integration suite (real sandbox; requires Docker and ~/.local/share/opencode/auth.json)
make test-integration

# Playwright UI tests (requires Node + Playwright browsers)
make test-ui

# Race detector
make test-race

# Coverage
make cover
```

Notes:
- Real-sandbox tests require Docker and a valid OpenCode auth.json at `~/.local/share/opencode/auth.json`.
- Tests select a supported provider/model at runtime (no hard-coded model IDs).
- SSE tests are bounded by context timeouts and do not hang.
- Unit tests cover rendering, server helpers, and concurrency primitives without requiring Docker.
- Integration tests spin up one sandbox per suite file; ensure Docker and `~/.local/share/opencode/auth.json` are available.
- The CI workflow populates `~/.local/share/opencode/auth.json` from the `OPENCODE_API_KEY` environment variable (falls back to a dummy key if unset).

### Playwright UI Tests

The Playwright specs under `test/ui/*.spec.js` cover dropdown behaviour, preview/terminal flows, responsive layout, and SSE scrolling. Run them headlessly with:

```bash
make test-ui
```

This target builds the server, runs it on port `6666`, executes the Playwright suite (`CI=1 PLAYWRIGHT_BASE_URL=http://localhost:6666`), and tears the server down afterward.

## Design Decisions

1. **Template separation**: HTML templates and CSS are in separate files for maintainability
2. **Reusable message partial**: Single message template used across all rendering paths
3. **Embedded files**: Templates and static files are embedded in the binary for easy deployment
4. **Constructor pattern**: `NewServer()` ensures proper initialization of templates
5. **Test organization**: Two Go suites (`unit_test.go`, `integration_test.go`) plus Playwright UI coverage; each integration suite spins up a single sandbox per file.
6. **HTMX-only approach**: All UI updates via HTML fragments, no JSON APIs
7. **Port offset strategy**: OpenCode port = HTTP port + 1000 for predictability

## Dependencies

- Go 1.23+
- Docker (for real sandbox tests)
- OpenCode sandbox image (built by the repo’s Dockerfile in `sandbox/`)
- github.com/PuerkitoBio/goquery (for HTML parsing in tests)

## OpenCode API Documentation

- **OpenCode Server Docs**: https://opencode.ai/docs/server/
- **OpenAPI Spec**: Available at `http://localhost:<opencode-port>/doc` when OpenCode server is running
- **Important Note**: OpenCode only provides a global `/event` SSE endpoint that streams ALL sessions' events. Session-specific filtering must be done client-side.
