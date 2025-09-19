# UI Test Guide for OpenCode Client

The JavaScript-based UI test suite is now fully managed by Playwright. Every browser automation script lives under `test/ui` and is executed through the Playwright runner.

## Prerequisites

1. Start the Go server on a non-conflicting port (we reserve `6666` for UI test runs):

   ```bash
   ./opencode-chat -port 6666
   ```
2. Install Node.js 18+.
3. Install the test dependencies and browser binaries:

```bash
npm install
npx playwright install
```

> The repository includes `package-lock.json`; re-running `npm install` keeps versions consistent.

## Running The Suite

```bash
# Headless run against the server on port 6666
CI=1 PLAYWRIGHT_BASE_URL=http://localhost:6666 npx playwright test

# Headed/debug mode (Chromium window)
PLAYWRIGHT_BASE_URL=http://localhost:6666 npm run test:ui:headed

# Filter specific specs
CI=1 PLAYWRIGHT_BASE_URL=http://localhost:6666 npx playwright test test/ui/preview.spec.js
```

The Playwright config (`playwright.config.js`) pins the test directory to `test/ui`, uses Chromium by default, and limits execution to a single worker to avoid cross-test interference with the shared sandbox.

## Test Suite Reference

| Spec | Coverage Highlights | Opt-in Variables |
|------|---------------------|------------------|
| `test/ui/preview.spec.js` | First-load preview content, tab switching, Kill button on a disposable Python server | `PREVIEW_TEST_SERVER_PORT` (defaults to `5555`) |
| `test/ui/layout-flush.spec.js` | Header/tab bar alignment across breakpoints | — |
| `test/ui/resize.spec.js` | Responsive behaviour, scroll preservation, resize race guards | — |
| `test/ui/dropdown-sse.spec.js` | File dropdown focus/selection during synthetic SSE updates | — |
| `test/ui/sse-scroll.spec.js` | Auto-scroll stickiness for the message list | — |
| `test/ui/terminal.spec.js` | Terminal iframe rendering, optional GoTTY and Code sync checks | `TERMINAL_APP_URL`, `TERMINAL_GOTTY_URL` |
| `test/ui/sse-oob.spec.js` | File stats + dropdown refresh when sandbox files change | `PLAYWRIGHT_SANDBOX_CONTAINER` (Docker container name) |
| `test/ui/sse-stream.spec.js` | Smoke test for the SSE event feed | `PLAYWRIGHT_SSE_PORT`, `PLAYWRIGHT_SSE_PATH` |

## Optional Environment Variables

Use these to enable advanced scenarios or non-default ports:

- `PLAYWRIGHT_BASE_URL` — overrides the default `http://localhost:8080` base URL.
- `PREVIEW_TEST_SERVER_PORT` — port used by the preview spec when launching its temporary Python server.
- `TERMINAL_APP_URL` — alternate app URL for terminal tests when the UI runs on a custom port.
- `TERMINAL_GOTTY_URL` — direct GoTTY endpoint (e.g., `http://localhost:41467`).
- `PLAYWRIGHT_SANDBOX_CONTAINER` — Docker container where files should be created/removed during SSE OOB checks.
- `PLAYWRIGHT_SSE_PORT` / `PLAYWRIGHT_SSE_PATH` — SSE endpoint coordinates for the stream spec.
- `PLAYWRIGHT_WORKERS` — override worker count (default `1`).

Example run enabling the optional suites:

```bash
PLAYWRIGHT_BASE_URL=http://localhost:8090 \
TERMINAL_GOTTY_URL=http://localhost:41467 \
PLAYWRIGHT_SANDBOX_CONTAINER=opencode-sandbox \
PLAYWRIGHT_SSE_PORT=6001 \
npm run test:ui
```

## Adding New UI Tests

1. Create a new `.spec.js` file under `test/ui`.
2. Import Playwright helpers: `const { test, expect } = require('@playwright/test');`.
3. Prefer `test.describe` blocks with focused fixtures/`beforeEach` to keep setup scoped.
4. Share common utilities by exporting helpers from `test/ui/helpers/*`.
5. Use Playwright assertions (`expect`) instead of manual logging so CI failures are explicit.
6. Guard environment-specific behaviour with `test.skip`/`test.fail` based on feature flags.

## Troubleshooting

- **Server not running**: The suite assumes the Go server is already started. Launch it via `go run .` or `./opencode-chat -port 8080` before executing tests.
- **Playwright binaries missing**: Re-run `npx playwright install` if browsers were removed or if the OS changed.
- **Docker unavailable**: Specs that rely on sandbox file manipulation (`sse-oob.spec.js`) will skip themselves unless `PLAYWRIGHT_SANDBOX_CONTAINER` is set.
- **Session-dependent flows**: Inject the helper from `test/ui/helpers/session-helper.js` using `await injectSessionHelper(page)` in the relevant spec.

With everything consolidated under Playwright, there is no longer a need to paste scripts into the browser console—the suite can be run headlessly or interactively in one command.
