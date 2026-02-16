# CLAUDE.md

## Commands

```bash
go build -o opencode-chat ./cmd/opencode-chat
go run ./cmd/opencode-chat -port 8080
go build -ldflags="-s -w" -o opencode-chat ./cmd/opencode-chat  # production build

make test-unit          # unit + property tests, no Docker
make test-integration   # requires Docker + ~/.local/share/opencode/auth.json
make test-e2e           # builds app, starts on :9876, runs Playwright-go, kills app
go test -v -run TestUnitMessageFormatting -timeout 60s ./internal/server/  # single test
go test -v -cover -timeout 60s ./...  # all with coverage
```

## Layout

```
cmd/opencode-chat/main.go       # entrypoint
internal/
  models/types.go                # API/data types
  sandbox/                       # Docker/Fly.io sandbox implementations
  auth/                          # auth types + helpers
  middleware/                    # HTTP middleware
  templates/                    # embed.FS: HTML templates + static assets
  views/                        # markdown rendering, tool output, message transforms
  sse/parts.go                  # MessagePartsManager, SSE event validation
  server/
    routes.go                   # Go 1.22+ method routing
    handlers_*.go               # grouped by domain (chat, tabs, proxy, files, auth)
    opcode_client.go            # OpenCode API helpers
    session.go                  # cookie <-> OpenCode session mapping
e2e/                            # Playwright-go browser tests
sandbox/Dockerfile              # sandbox container image
```

Package deps (no cycles): models -> stdlib; sandbox -> models; auth, middleware, templates -> stdlib; views -> models, templates; sse -> views; server -> all; cmd/main -> server, sandbox

## Architecture

Web chat interface for OpenCode. Go + HTMX 2 + SSE. All endpoints return HTML fragments, never JSON.

- **Session bridging**: browser cookies map to OpenCode session IDs, created on-demand via `/session`
- **Sandbox isolation**: Docker containers (or Fly.io) per user with own auth + port mapping
- **SSE filtering**: OpenCode streams ALL sessions on global `/event`; `handleSSE()` filters by sessionID
- **Templates**: `embed.FS` in `internal/templates/templates/`, loaded via `views.LoadTemplates()`
- **Shutdown**: SIGINT/SIGTERM -> HTTP shutdown (5s) -> sandbox cleanup -> defer for panics

Routes defined in `routes.go`: `GET /{$}`, login/logout, `POST /send`, `GET /events` (SSE), `POST /clear`, tab handlers (preview/code/terminal/deployment), file handlers, WebSocket terminal proxy, reverse preview proxy, static files.

## Tests

**IMPORTANT: Always write property-based tests using `pgregory.net/rapid`. Do NOT write traditional table-driven or single-example unit tests.** Every new test must use `rapid.Check` with generators. Property tests are strictly preferred because they explore the input space more thoroughly and catch edge cases that hand-picked examples miss. See `prop_test.go` for generator patterns (`genPlainText`, `genXSSPayload`, `genQuestionInfo`, etc.).

- `internal/server/prop_test.go` — **primary test file**: property-based tests (pgregory.net/rapid)
- `internal/server/unit_test.go` — legacy traditional tests (do not add new tests here; migrate to prop_test.go when touching)
- `internal/server/integration_test.go` — mocked HTTP/SSE + real sandbox + race/signal
- `e2e/` — Playwright-go browser tests (headless Chromium)

Key details:
- Dynamic port allocation everywhere; integration tests poll for readiness (no fixed sleeps)
- `RealSuiteServer()` starts one sandbox per test file, not per-test
- Integration tests need Docker + `~/.local/share/opencode/auth.json`
- CI populates auth.json from `OPENCODE_API_KEY` secret (falls back to dummy key)

## Dependencies

Go 1.24+, Docker (for integration/E2E tests), sandbox image built from `sandbox/Dockerfile`.

## References

- OpenCode server docs: https://opencode.ai/docs/server/
- OpenAPI spec: `http://localhost:<opencode-port>/doc` (when running)
