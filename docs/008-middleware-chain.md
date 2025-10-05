# Middleware Chain and Auth Context

## Summary

Introduce a reusable middleware stack that composes logging, authentication context, and access control for every `net/http` handler. The design standardises handler wiring, makes auth state available through request context, and removes manual cookie checks scattered across the codebase.

## Goals

- Provide a first-class `Middleware` abstraction that mirrors the "next" pattern used by popular Go routers.
- Ensure every request automatically receives logging and authentication metadata without bespoke glue code.
- Move authentication lookup into a single `withAuth` middleware so handlers can consume state via context.
- Gate protected routes through a declarative `requireAuth` middleware rather than inline guard clauses.
- Preserve existing SSE logging semantics and route special-casing.

## Non-Goals

- Replace the standard library router with a framework (e.g., Gin). The focus is ergonomics atop `net/http`.
- Broaden the authentication model; the cookie-based session map remains unchanged.
- Introduce new authorisation rules. The document clarifies how to add them, but limits the PR to routing + plumbing.

## Background

Before this change, each handler was registered individually with hand-rolled wrappers. Auth-dependent pages (`/projects`) performed inline cookie checks, while logging duplicated the SSE special-case per handler. Adding new cross-cutting behaviour required re-wrapping every route and risked missing an endpoint. Tests exercised the logger through a local closure, not the production wrapper.

## Design

### Middleware Abstraction

```go
type Middleware func(http.Handler) http.Handler

func chainMiddleware(h http.Handler, middlewares ...Middleware) http.Handler {
    for i := len(middlewares) - 1; i >= 0; i-- {
        h = middlewares[i](h)
    }
    return h
}
```

Handlers list their dependencies in order of execution, left-to-right. The implementation walks the list in reverse so the first argument is the outermost wrapper (matching community expectations).

### Logging Middleware

`loggingMiddleware` moved from an inline closure to a standalone `func(http.Handler) http.Handler`. It preserves the SSE bypass while leveraging `LoggingResponseWriter` for status/body capture everywhere else.

### Authentication Context Middleware

- `withAuth` resolves the `auth_session` cookie once, stores the result in an `AuthContext`, and attaches it to the request via `context.WithValue`.
- `authContext(r)` is a helper that returns the struct (default zero value when absent).

Handlers such as `handleIndex`, `handleLogin`, and `handleProjects` now derive state from the context instead of calling `isAuthenticated`/`getAuthSession` directly.

### Auth Guard Middleware

`requireAuth` consumes `authContext`. When `IsAuthenticated` is false it redirects to `/login`. A legacy fallback calling `s.isAuthenticated` remains to cover edge cases where `withAuth` is omitted (defensive programming, though tests exercise the intended composition order: `withAuth` → `requireAuth`).

### Route Wiring

Every route in `main.go` is registered with the appropriate stack. Examples:

- Public page: `chainMiddleware(http.HandlerFunc(server.handleIndex), loggingMiddleware, server.withAuth)`
- Protected page: `chainMiddleware(http.HandlerFunc(server.handleProjects), loggingMiddleware, server.withAuth, server.requireAuth)`
- Terminal proxy (no logging): `chainMiddleware(http.HandlerFunc(server.handleTerminalProxy), server.withAuth)`

This centralises cross-cutting concerns and documents intent inline with the registration.

### Session vs Auth Session State

- `sessions` tracks the OpenCode chat session per user cookie. It is required for rendering chat history and sending messages upstream. Entries persist even for anonymous users.
- `authSessions` holds authentication metadata (email, timestamp) for cookies that completed login. Handlers use `AuthContext` to determine whether to show privileged UI and `requireAuth` gates access accordingly.

Both maps live behind the shared `Server.mu` and are populated by cookie value. In other words, the middleware does not conflate “chat session” with “logged-in user”; a request may have an OpenCode session with no auth session and vice versa.

### Remaining Server Fields and Isolation Strategy

- **Global by design**: `sandbox`, `workspaceSession`, `templates`, `providers`, `defaultModel`, and the singleton `codeUpdateLimiter`. These describe backend resources or caches shared by all users. If future requirements demand per-user sandboxes or per-user rate limits, the corresponding fields must migrate into the session layer.
- **Session-scoped through maps**: `sessions`, `authSessions`, `selectedFiles`. As long as these keep using cookie keys, no additional work is needed for isolation.
- **Candidates for refactoring**: `codeUpdateLimiter` currently throttles SSE-driven code-tab refreshes globally. A natural follow-up would be to store one limiter per session (e.g., inside a `sessionState` struct) so simultaneous SSE consumers do not block each other. Similarly, if `selectedFiles` gains more metadata it might make sense to encapsulate it alongside the limiter under a per-session object with its own lock.

## Testing Strategy

- `unit_test.go`
  - Validates `chainMiddleware` order (`TestChainMiddlewareOrder`).
  - Exercises `loggingMiddleware` for normal and SSE endpoints using the production wrapper.
  - Confirms `requireAuth` blocks anonymous requests and passes authenticated ones when composed with `withAuth`.
  - Verifies `withAuth` populates (or omits) context as expected.
- `go test ./...` remains the primary regression signal and runs cleanly.

## Risks & Mitigations

- **Forgotten middleware order** – Tests assert `withAuth` precedes `requireAuth`, and the fallback inside `requireAuth` covers mis-ordered stacks.
- **Context misuse** – `authContext` falls back to a zero value, so callers get predictable defaults instead of panics.
- **SSE logging regression** – The dedicated branch inside `loggingMiddleware` preserves the previous behaviour; unit tests ensure it still bypasses the response buffer.

## Future Work

- Introduce additional middlewares (rate limiting, CSRF) by extending the registration list without touching handlers.
- Refine authorisation by enriching `AuthContext` (e.g., roles, project membership) once requirements crystalise.
- Explore sharing `authContext` with integration tests to reduce duplicate cookie plumbing.
