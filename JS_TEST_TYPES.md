# Frontend Playwright Test Guide

All browser-facing automation now lives in the Playwright test runner. Every test file sits under `test/ui` and runs through `@playwright/test`.

## Project Layout

- `playwright.config.js` — shared configuration (Chromium desktop, serial workers by default).
- `test/ui/*.spec.js` — individual suites (preview, layout, responsive behaviour, SSE, terminal, etc.).
- `test/ui/helpers/session-helper.js` — optional helper that can be injected when a scenario needs to persist chat sessions across reloads.

## Installing Dependencies

```bash
npm install
npx playwright install
```

> The repository ships with a `package-lock.json`; re-running `npm install` keeps dependencies pinned. `npx playwright install` downloads the browsers required for headless runs.

## Running The UI Suites

```bash
npm run test:ui
# or
npx playwright test
```

Use headed mode when debugging:

```bash
npm run test:ui:headed
```

## Suite Overview

| File | Purpose | Notes |
|------|---------|-------|
| `test/ui/preview.spec.js` | End-to-end preview tab checks (first load, tab switching, kill button) | Starts an optional local Python server on port `5555` to exercise the Kill button. Override with `PREVIEW_TEST_SERVER_PORT`. |
| `test/ui/layout-flush.spec.js` | Visual alignment of header/tab bar across breakpoints | Pure DOM assertions, no external deps. |
| `test/ui/resize.spec.js` | Responsive viewport transitions, scroll preservation, resize race conditions | Injects sample messages before resizing. |
| `test/ui/dropdown-sse.spec.js` | File dropdown focus/selection stability when SSE updates arrive | Simulates SSE events with custom DOM updates. |
| `test/ui/sse-scroll.spec.js` | Message list auto-scroll behaviour (bottom stickiness, near-bottom threshold, rapid bursts) | Runs entirely in-browser using synthetic SSE events. |
| `test/ui/terminal.spec.js` | Terminal iframe presence and optional GoTTY/Code tab integration | Requires `TERMINAL_APP_URL` / `TERMINAL_GOTTY_URL` for advanced checks; otherwise only the iframe test runs. |
| `test/ui/sse-oob.spec.js` | Code tab out-of-band SSE updates triggered by sandbox file changes | Requires Docker and `PLAYWRIGHT_SANDBOX_CONTAINER` pointing at the running sandbox container. |
| `test/ui/sse-stream.spec.js` | Low-level SSE stream validation | Skipped unless `PLAYWRIGHT_SSE_PORT` (and optionally `PLAYWRIGHT_SSE_PATH`) are defined. |

## Optional Environment Variables

Some suites are guarded behind environment variables so CI and local runs can opt-in gradually:

- `PREVIEW_TEST_SERVER_PORT` — override the port used for the temporary Python server in preview tests (default `5555`).
- `TERMINAL_APP_URL` — base URL for the UI when terminal tests need a different port than the global `baseURL`.
- `TERMINAL_GOTTY_URL` — direct GoTTY endpoint for terminal interaction tests.
- `PLAYWRIGHT_SANDBOX_CONTAINER` — Docker container name used by the SSE OOB suite for creating/removing files.
- `PLAYWRIGHT_SSE_PORT` — port of the SSE event stream; enables the SSE stream spec.
- `PLAYWRIGHT_SSE_PATH` — optional path override for the SSE endpoint (defaults to `/event`).
- `PLAYWRIGHT_WORKERS` — override the single-worker default if you have isolated sandboxes per test run.

Set these variables inline when executing the suite, e.g.:

```bash
PLAYWRIGHT_SANDBOX_CONTAINER=opencode-sandbox \
TERMINAL_GOTTY_URL=http://localhost:41467 \
PLAYWRIGHT_SSE_PORT=6001 \
npx playwright test
```

## Tips

- Playwright tests assume the Go server is already running at `PLAYWRIGHT_BASE_URL` (defaults to `http://localhost:8080`).
- Keep long-running fixtures (e.g. Python servers) within `test.step`/`try...finally` blocks to guarantee teardown.
- When adding new browser helpers, export them from `test/ui/helpers/*` so they can be shared across specs.
- Prefer Playwright assertions (`expect`) over manual logging so failures surface clearly in CI.
