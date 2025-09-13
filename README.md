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
├── integration_http_test.go        # Mocked HTTP/SSE rendering tests (no Docker)
├── integration_flow_test.go        # Regular flow tests (one sandbox/server per file)
├── integration_race_signal_test.go # Race condition + signal tests (one sandbox/server per file)
├── test_suite_helpers.go           # Per-file suite server helper (RealSuiteServer)
├── suite_main_test.go              # Global cleanup for suite servers
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

- Unit tests (no Docker): consolidated into three files
  - `unit_rendering_test.go` — templates, UI, and rendering pipeline
  - `unit_server_test.go` — logging, message parts, readiness helpers
  - `unit_race_test.go` — rate limiter and SSE duplication/concurrency
- Integration tests:
  - Mocked HTTP/SSE (fast; no Docker): `integration_http_test.go`
  - Flow (real sandbox; Docker + auth.json): `integration_flow_test.go`
  - Race/Signals (real sandbox; signals + full app): `integration_race_signal_test.go`

Integration suites start exactly one real sandbox/server per file via `RealSuiteServer`, so tests within a file do not repeatedly start Docker.

### Commands

```bash
# All tests (about ~30s on a warm Docker)
go test -v ./...

# Fast mocked tests only (no Docker)
make test-fast

# Unit tests only (no Docker; consolidated)
make test-unit

# Flow suite (real sandbox; requires Docker and ~/.local/share/opencode/auth.json)
make test-flow

# Race + Signals (real sandbox; signals build and run the app)
make test-race-signal

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

### Playwright UI Tests

The project includes comprehensive Playwright tests in `test_resize_scenarios.js` for validating UI stability, scroll preservation, and responsive behavior. These tests cover:

- **Scroll Position Preservation**: Ensures scroll positions are maintained across mobile/desktop transitions
- **Resize Debouncing**: Validates that rapid viewport changes don't cause UI flickering
- **Input Protection**: Verifies chat doesn't minimize when user has active text input
- **Race Condition Handling**: Tests complex interactions like scrolling while resizing

To run Playwright tests:

```bash
# 1. Start the server
go build -o opencode-chat *.go && ./opencode-chat -port 8080

# 2. Run tests with Playwright (requires Playwright installed)
# Either use Playwright directly or Claude Code's Playwright MCP:
# - Navigate to http://localhost:8080
# - Execute test functions from test_resize_scenarios.js

# Key test functions:
# - testScrollPositionPreservation() - Critical scroll preservation test
# - testRapidResizeTransitions() - Tests debounce mechanism
# - testComplexInteractions() - Tests race conditions
# - runAllTests() - Runs complete test suite
```

## Design Decisions

1. **Template separation**: HTML templates and CSS are in separate files for maintainability
2. **Reusable message partial**: Single message template used across all rendering paths
3. **Embedded files**: Templates and static files are embedded in the binary for easy deployment
4. **Constructor pattern**: `NewServer()` ensures proper initialization of templates
5. **Test organization**: Three suites (mocked HTTP, flow, race/signals) with one sandbox per file
6. **HTMX-only approach**: All UI updates via HTML fragments, no JSON APIs
7. **Port offset strategy**: OpenCode port = HTTP port + 1000 for predictability

## Dependencies

- Go 1.19+
- Docker (for real sandbox tests)
- OpenCode sandbox image (built by the repo’s Dockerfile in `sandbox/`)
- github.com/PuerkitoBio/goquery (for HTML parsing in tests)

## OpenCode API Documentation

- **OpenCode Server Docs**: https://opencode.ai/docs/server/
- **OpenAPI Spec**: Available at `http://localhost:<opencode-port>/doc` when OpenCode server is running
- **Important Note**: OpenCode only provides a global `/event` SSE endpoint that streams ALL sessions' events. Session-specific filtering must be done client-side.
