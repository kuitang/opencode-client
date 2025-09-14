# Terminal Implementation Design Document

## Overview

This document describes the implementation of the web-based terminal feature for the OpenCode chat interface. The terminal provides users with direct shell access to the Docker sandbox environment through a browser interface.

## Architecture

### Components

1. **GoTTY v1.6.0** - Web terminal emulator that exposes a shell over WebSocket
2. **Docker Sandbox** - Isolated container running both OpenCode and GoTTY
3. **Go Reverse Proxy** - HTTP/WebSocket proxy for routing terminal traffic
4. **HTMX Frontend** - Terminal tab with iframe embedding

### Technology Stack

- **GoTTY**: sorenisanerd/gotty v1.6.0 (actively maintained fork)
- **WebSocket**: For real-time bidirectional communication
- **httputil.ReverseProxy**: Go standard library proxy
- **Docker**: Container orchestration with multi-process management

## Implementation Details

### 1. Docker Container Setup

The Docker container runs two processes simultaneously:
- OpenCode server on port 8080
- GoTTY terminal on port 8081

**Key decisions:**
- Used GoTTY v1.6.0 from sorenisanerd (not the original yudai/gotty v1.0.1)
- Implemented proper entrypoint.sh script with signal handling
- Used `--init` Docker flag instead of explicit tini for process management

### 2. Multi-Process Management

The entrypoint.sh script manages both processes:
```bash
# Start both processes in background
gotty ... &
GOTTY_PID=$!
opencode ... &
OPENCODE_PID=$!

# Wait for both processes
wait
```

**Important:** Using `wait` without arguments waits for ALL background processes, not just one.

### 3. WebSocket Proxy Implementation

The terminal proxy in Go handles path rewriting and WebSocket upgrades:
```go
proxy := httputil.NewSingleHostReverseProxy(target)
proxy.Director = func(req *http.Request) {
    // Strip "/terminal" prefix for gotty
    if strings.HasPrefix(originalPath, "/terminal") {
        newPath := strings.TrimPrefix(originalPath, "/terminal")
        req.URL.Path = newPath
    }
    // Set Origin header to match gotty's expectation
    req.Header.Set("Origin", fmt.Sprintf("http://%s", target.Host))
}
```

## Critical Issues and Solutions

### Issue 1: GoTTY Version Incompatibility

**Problem:** Initial attempt used yudai/gotty v1.0.1 which lacked proper WebSocket origin control.

**Solution:** Upgraded to sorenisanerd/gotty v1.6.0 which includes:
- `--ws-origin` flag for CORS control
- Better WebSocket handling
- Active maintenance and bug fixes

### Issue 2: WebSocket 403 Forbidden Error

**Problem:** GoTTY rejected WebSocket connections with 403 Forbidden.

**Root Cause:** GoTTY validates that the Origin header matches the Host header to prevent CSRF attacks.

**Solutions Applied:**
1. Added `--ws-origin ".*"` flag to GoTTY to accept any origin
2. Proxy sets Origin header to match GoTTY's host:
   ```go
   req.Header.Set("Origin", fmt.Sprintf("http://%s", target.Host))
   ```

### Issue 3: Protocol Switch Error

**Error:** `can't switch protocols using non-Hijacker ResponseWriter type *main.LoggingResponseWriter`

**Root Cause:** The logging middleware wrapped the ResponseWriter, preventing WebSocket hijacking.

**Solution:** Remove logging middleware from terminal proxy route:
```go
// Before (broken):
http.HandleFunc("/terminal/", loggingMiddleware(server.handleTerminalProxy))

// After (working):
http.HandleFunc("/terminal/", server.handleTerminalProxy)
```

**Why this matters:** WebSocket upgrades require "hijacking" the underlying TCP connection. Middleware that wraps ResponseWriter breaks this capability unless it implements the Hijacker interface.

### Issue 4: Container Exit Issues

**Problem:** Container would exit immediately when using certain entrypoint configurations.

**Failed Attempts:**
- `wait -n`: Only waits for ONE process to exit
- `exec opencode`: Replaces shell process, can't manage multiple processes

**Solution:** Use simple `wait` command to wait for ALL background processes.

### Issue 5: Path Rewriting

**Problem:** GoTTY expects requests at root path (`/`, `/ws`), but proxy serves at `/terminal/`.

**Solution:** Strip `/terminal` prefix in proxy Director:
```go
if strings.HasPrefix(originalPath, "/terminal") {
    newPath := strings.TrimPrefix(originalPath, "/terminal")
    if newPath == "" {
        newPath = "/"
    }
    req.URL.Path = newPath
}
```

## Security Considerations

1. **CORS/Origin Validation**: Using `--ws-origin ".*"` accepts all origins. In production, this should be restricted to specific domains.

2. **Authentication**: Currently relies on OpenCode's session management. GoTTY's `--credential` flag could add an extra layer.

3. **Container Isolation**: Terminal access is limited to the Docker container, not the host system.

4. **Write Permissions**: `--permit-write` flag allows terminal input. Without it, terminal would be read-only.

## Testing Performed

1. **WebSocket Connection**: Verified successful WebSocket upgrade and bidirectional communication
2. **File Operations**: Created and listed files through terminal
3. **Interactive Applications**: Tested htop for real-time updates and keyboard interaction
4. **Process Management**: Verified both OpenCode and GoTTY run concurrently
5. **Graceful Shutdown**: Confirmed proper cleanup on container stop

## Frontend Integration

The terminal is embedded using an iframe:
```html
<iframe
    id="terminal-iframe"
    src="/terminal/"
    class="w-full h-full"
    style="border: none; background-color: #000;"
    title="Terminal Interface"
></iframe>
```

The `/terminal/` path is proxied to GoTTY, which serves:
1. HTML page with terminal div
2. JavaScript files (hterm.js, gotty.js)
3. WebSocket endpoint at `/terminal/ws`

## Lessons Learned

1. **Middleware and WebSockets Don't Mix**: Any middleware that wraps ResponseWriter must implement Hijacker interface for WebSocket support.

2. **Origin Validation Matters**: Modern WebSocket servers validate Origin headers as CSRF protection. Proxies must handle this correctly.

3. **Process Management in Docker**: Using bash with `wait` is simpler and more reliable than complex process managers for simple multi-process containers.

4. **Version Selection**: Choose actively maintained forks (sorenisanerd/gotty) over abandoned projects (yudai/gotty).

5. **Path Rewriting Complexity**: When proxying applications that use absolute paths, careful path rewriting is essential.

## Future Improvements

1. **Authentication**: Add GoTTY credential authentication for extra security
2. **Session Persistence**: Implement tmux/screen for persistent terminal sessions
3. **Multiple Terminals**: Support multiple terminal instances/tabs
4. **Custom Themes**: Allow terminal color scheme customization
5. **File Upload/Download**: Direct file transfer through terminal interface
6. **Audit Logging**: Log all terminal commands for security/debugging