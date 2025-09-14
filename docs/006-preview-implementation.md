# Preview Window Implementation

## Overview

The preview window feature automatically detects user-created web servers in the Docker sandbox and displays them in an iframe. When users create servers (e.g., Python HTTP servers), the Preview tab shows a live view of their application.

## Architecture

### Core Components

**Port Detection (`detectOpenPorts` in main.go:1240)**
- Uses `lsof -i -sTCP:LISTEN -P -n` to find listening TCP ports
- Filters out system ports (8080, 8081, 7681, <1024)
- Returns array of user application ports

**Preview Tab Handler (`handleTabPreview` in main.go:952)**
- Calls port detection when tab is accessed
- Renders preview template with port information
- Shows iframe if ports found, "No Application Running" otherwise

**Reverse Proxy (`handlePreviewProxy` in main.go:1107)**
- Proxies requests from `/preview/` to container applications
- Routes: `/preview/` → `http://CONTAINER_IP:PORT/`
- Strips `/preview` prefix from paths before forwarding

**Container IP Resolution (`ContainerIP` method)**
- Gets Docker container internal IP via `docker inspect`
- Enables proxy to connect from host to container services
- Falls back to localhost for non-Docker sandboxes

## Critical Implementation Details

### Issue 1: JSON Escaping in Shell Commands

**Problem:** Original lsof command failed due to awk syntax in JSON:
```go
// BROKEN: awk '{print $9}' gets mangled in JSON marshaling
command := `lsof -i -sTCP:LISTEN -P -n | awk '{print $9}' | grep -o '[0-9]*$'`
```

**Solution:** Use grep/sed instead of awk to avoid JSON escaping:
```go
// WORKING: grep -o avoids JSON escaping issues
command := `lsof -i -sTCP:LISTEN -P -n | grep -o ':[0-9]*' | sed 's/://' | sort -u`
```

### Issue 2: Container Network Isolation

**Problem:** Proxy tried connecting to `localhost:5000` but apps run inside Docker container.

**Solution:** Added `ContainerIP()` method to get container's internal IP:
```go
containerIP := s.sandbox.ContainerIP() // Returns "172.17.0.X"
targetURL := fmt.Sprintf("http://%s:%d", containerIP, port)
```

### Issue 3: Path Routing Pattern

**Problem:** Initially used `/preview` endpoint, causing path conflicts.

**Solution:** Follow terminal pattern with `/preview/` prefix:
```go
// Routes all /preview/* paths to proxy
http.HandleFunc("/preview/", loggingMiddleware(server.handlePreviewProxy))

// Strip prefix in proxy director
if strings.HasPrefix(req.URL.Path, "/preview/") {
    req.URL.Path = strings.TrimPrefix(req.URL.Path, "/preview")
    if req.URL.Path == "" {
        req.URL.Path = "/"
    }
}
```

### Issue 4: Missing Admin Tools

**Problem:** Minimal Docker environment lacked `ps`, `netstat`, `pkill`, etc.

**Solution:** Enhanced Dockerfile with essential admin tools:
```dockerfile
RUN apt-get install -y procps net-tools psmisc iproute2 socat
```

## Templates

**Preview Tab (`templates/tabs/preview.html`)**
- Uses shared Mac-style chrome component
- Conditionally shows iframe vs "No Application Running"
- Includes port indicator and refresh/open buttons

**Template Logic:**
```html
{{if .PreviewPort}}
<iframe src="/preview/" ...></iframe>
{{else}}
<div>No Application Running</div>
{{end}}
```

## Testing Patterns

**Integration Tests**
- Use shared test server from `integration_common_test.go`
- Test port detection with mock sandbox responses
- Verify proxy routing and error handling

**Playwright UI Tests**
- Test complete user flow: create server → click Preview → see content
- Verify iframe loading and content display
- Test refresh functionality

## Docker Container Network Architecture

**Port Binding Scenarios:**
1. **All Interfaces (`*:5000`)** - Accessible from container IP ✅
2. **Localhost Only (`127.0.0.1:5000`)** - NOT accessible from container IP ❌

**Container Network Layout:**
```
Host Machine (172.17.0.1)
└── Docker Container (172.17.0.3)
    ├── OpenCode Server: *:8080
    ├── Gotty Terminal: *:8081
    └── User App: *:5000 or 127.0.0.1:5000
```

## Future Work: Localhost-Only Server Support

### Problem
Apps binding to `127.0.0.1:5000` are inaccessible from host reverse proxy.

### Proposed Solutions

**Option 1: Socat Auto-Tunneling**
```bash
# Detect localhost-only binding
lsof -i -sTCP:LISTEN -P -n | grep "127.0.0.1:5000"

# Auto-create tunnel (complex - port conflicts)
socat tcp-listen:5000,fork,bind=0.0.0.0 tcp-connect:127.0.0.1:5000
```

**Option 2: Port Translation**
- Use different external port for tunnel (e.g., 5001 → 5000)
- Update proxy to handle port mapping

**Option 3: User Education**
- Encourage LLM to create servers on 0.0.0.0 by default
- Add examples in error messages

### Current Status
- **Commented out** socat tunneling code in `main.go:1333-1350`
- **Docker image ready** with socat installed
- **Framework exists** for future implementation

## Security Considerations

**Exposed Services:**
- User applications become accessible via container IP
- No authentication/authorization on proxied content
- Potential for unintended service exposure

**Mitigation Strategies:**
- Container network isolation limits exposure scope
- Preview only accessible to host machine by default
- Consider adding basic auth or rate limiting in future

## Debugging

**Key Log Patterns:**
```
findUserPorts: found user ports [5000]
Preview: Detected open port 5000
Preview proxy: forwarding to http://172.17.0.3:5000
WIRE_OUT GET /preview/ [200]: <content>
```

**Common Issues:**
- Empty lsof output → Check container is running
- 502 Bad Gateway → Check container IP and port binding
- Connection refused → App likely bound to localhost only
- EOF errors → App closing connections immediately

## File References

- `main.go:1240` - Port detection logic
- `main.go:952` - Preview tab handler
- `main.go:1107` - Reverse proxy implementation
- `sandbox_docker.go:237` - Container IP resolution
- `templates/tabs/preview.html` - Preview UI template
- `sandbox/Dockerfile:19` - Enhanced admin tools installation