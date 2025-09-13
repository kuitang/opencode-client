# Repository Guidelines

## Project Structure & Module Organization
- Root Go app: `main.go`, helpers in `*.go`.
- Tests live beside code with `*_test.go`.
- Assets: `static/` (CSS/JS/images), `templates/` (HTML), `theme_samples/` (design examples).
- Scripts/tools: `test_sse.sh`, `test_sse.js`, `analyze_sse.py` (dev utilities).
- Logs and binaries may exist locally (`server.log`, `opencode-chat`); do not commit new artifacts.
- Test suites:
  - Unit tests (no Docker): consolidated into three files
    - `unit_rendering_test.go` — templates, UI, and rendering pipeline
    - `unit_server_test.go` — logging, message parts, readiness helpers
    - `unit_race_test.go` — rate limiter and SSE duplication/concurrency
  - Integration tests:
    - `integration_http_test.go` — mocked HTTP/SSE rendering (NO Docker).
    - `integration_flow_test.go` — regular flow tests (real sandbox).
    - `integration_race_signal_test.go` — race + signal tests.

## Build, Test, and Development Commands
- Build: `go build -o opencode-chat .` — compiles the server.
- Run: `go run .` — starts the app from source.
- Tests (all): `go test ./...` (~30s across 3 suite sandboxes)
- Unit tests only: `make test-unit` (no Docker)
- Fast mocked only: `make test-fast` (no Docker)
- Flow suite: `make test-flow` (Docker + `~/.local/share/opencode/auth.json`)
- Race/Signals: `make test-race-signal` (Docker; signals build+run app)
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
- Use one sandbox/server per file via `RealSuiteServer(t, &SuiteHandle{})` (see `test_suite_helpers.go`). Do NOT start/stop Docker per test.
- `integration_http_test.go` must stay mocked (no real sandbox). Use `httptest.Server` + `StaticURLSandbox`.
- Real-sandbox tests should use `s.sandbox.OpencodeURL()` and not depend on internal fields (ports/paths).
- Select models via `GetSupportedModelCombined` (discovers a valid provider/model from sandbox).
- SSE tests must be bounded with context timeouts; do not hang.
- Names: `TestXxx` for tests, `BenchmarkXxx` for benchmarks; use `t.Run` for subcases.

## Commit & Pull Request Guidelines
- Commit style (observed): imperative present (“Add”, “Fix”, “Refactor”), concise scope. Example: `Fix session cookie creation in handleSend`.
- Group related changes; include brief rationale in body when non-trivial.
- PRs must include: summary, motivation/issue link, test coverage notes, and screenshots/logs for UI or SSE behavior.
- CI expectations: code formats (`go fmt`), vets cleanly, and `go test -race ./...` passes.

## Security & Configuration Tips
- Never commit secrets, cookies, or personal logs; prefer env vars.
- Validate and sanitize all user inputs; avoid panics in handlers.
- When testing SSE/HTTP locally, use temporary dirs for isolated state.
