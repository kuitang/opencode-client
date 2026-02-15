# Repository Guidelines

## Project Structure & Module Organization
- Entrypoint: `cmd/opencode-chat/main.go`.
- Application code in `internal/` packages:
  - `models/` — shared API/data types
  - `sandbox/` — sandbox interface + Docker/Fly.io implementations
  - `auth/` — authentication types and helpers
  - `middleware/` — HTTP middleware (logging, chaining)
  - `templates/` — embedded HTML templates + static assets
  - `views/` — rendering pipeline (markdown, tool details, message transforms)
  - `sse/` — SSE message parts manager + event validation
  - `server/` — HTTP server, routes, handlers
- Assets: `internal/templates/static/` (CSS/JS), `internal/templates/templates/` (HTML).
- Logs and binaries may exist locally (`server.log`, `opencode-chat`); do not commit new artifacts.
- Test suites (in `internal/server/`):
  - Unit tests (no Docker): `unit_test.go` — rendering, server helpers, concurrency primitives.
  - Property tests: `prop_test.go` — rapid property-based tests (XSS, middleware order, part ordering).
  - Integration tests: `integration_test.go` — mocked HTTP/SSE rendering, real sandbox flows, and race/signal coverage.
- E2E tests (`e2e/`): Browser-based Playwright-go tests (resize, preview, terminal, SSE, layout).

## Build, Test, and Development Commands
- Build: `go build -o opencode-chat ./cmd/opencode-chat` — compiles the server.
- Run: `go run ./cmd/opencode-chat` — starts the app from source.
- Tests (all): `go test ./...` (~30s across 3 suite sandboxes)
- Unit + property tests: `make test-unit` (no Docker)
- Integration suite: `make test-integration` (Docker + `~/.local/share/opencode/auth.json`)
- E2E tests: `make test-e2e` (builds app, launches, runs Playwright browser tests)
- Verbose + race: `go test -v -race ./...`
- Coverage: `go test -cover ./...`
- Format: `go fmt ./...`  Lint: `go vet ./...`

## Coding Style & Naming Conventions
- Go style: exported `CamelCase`, unexported `lowerCamelCase`; file names lowercase with underscores.
- Keep functions small, prefer pure helpers, use table-driven tests.
- Error handling: wrap with context; return early; no panics in request paths.
- Formatting is enforced with `go fmt`; run `go vet` before PRs.

## Testing Guidelines
- Framework: standard `testing` package.
- Use one sandbox/server per file via `RealSuiteServer(t, &SuiteHandle{})` (see `test_suite_helpers_test.go`). Do NOT start/stop Docker per test.
- `integration_test.go` includes a mocked section (no real sandbox) and the real-sandbox suites. Use the existing helpers (`httptest.Server`, `sandbox.NewStaticURLSandbox`, `RealSuiteServer`) as appropriate.
- Real-sandbox tests should use `s.Sandbox.OpencodeURL()` and not depend on internal fields (ports/paths).
- Select models via `GetSupportedModelCombined` (discovers a valid provider/model from sandbox).
- SSE tests must be bounded with context timeouts; do not hang.
- Names: `TestXxx` for tests, `BenchmarkXxx` for benchmarks; use `t.Run` for subcases.

## Commit & Pull Request Guidelines
- Commit style (observed): imperative present ("Add", "Fix", "Refactor"), concise scope. Example: `Fix session cookie creation in handleSend`.
- Group related changes; include brief rationale in body when non-trivial.
- PRs must include: summary, motivation/issue link, test coverage notes, and screenshots/logs for UI or SSE behavior.
- CI expectations: code formats (`go fmt`), vets cleanly, and `go test -race ./...` passes.

## Security & Configuration Tips
- Never commit secrets, cookies, or personal logs; prefer env vars.
- Validate and sanitize all user inputs; avoid panics in handlers.
- When testing SSE/HTTP locally, use temporary dirs for isolated state.
