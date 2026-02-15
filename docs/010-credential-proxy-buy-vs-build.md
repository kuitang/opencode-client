# Credential Proxy: Buy vs Build

## What We Need

A transparent HTTPS proxy that:
1. Runs as a sidecar in the sandbox VM
2. Intercepts outbound HTTPS to specific domains (api.openai.com, api.stripe.com, etc.)
3. Injects API keys (Authorization header, custom headers, etc.) per domain
4. Passes all other traffic through untouched (no MITM)
5. App code is unmodified — just calls normal URLs

## Option Evaluation

### Option 1: `elazarl/goproxy` (BUILD on top of — RECOMMENDED)

**What it is:** Go HTTP proxy library, 5.5k stars, actively maintained.
Stripe forked it (`github.com/stripe/goproxy`). Battle-tested.

**Why it fits:**
- Selective per-domain MITM is a first-class API:
  ```go
  // Only MITM these domains, tunnel everything else
  proxy.OnRequest(goproxy.DstHostIs("api.openai.com")).HandleConnect(goproxy.AlwaysMitm)
  proxy.OnRequest(goproxy.DstHostIs("api.stripe.com")).HandleConnect(goproxy.AlwaysMitm)

  // Inject headers on intercepted requests
  proxy.OnRequest(goproxy.DstHostIs("api.openai.com")).DoFunc(
      func(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
          r.Header.Set("Authorization", "Bearer "+getKey("OPENAI_API_KEY"))
          return r, nil
      })
  ```
- Has a working transparent proxy example (`examples/goproxy-transparent/`)
  that handles iptables-redirected traffic
- CA cert generation built-in
- Handles CONNECT properly, including persistent connections

**What we'd build (~200-300 lines):**
- Config loader: read domain→header→key rules from JSON
- Key resolver: fetch keys from mounted secrets / env vars
- iptables setup in entrypoint.sh
- CA cert generation + trust store install at boot

**Effort:** 1-2 days. The hard proxy logic is done. We're gluing config to it.

**Risks:** goproxy hasn't had a major release recently (v1.7.2). Works fine,
just not actively adding features. The proxy code is stable though — HTTP
CONNECT hasn't changed.

### Option 2: `google/martian` (VIABLE ALTERNATIVE)

**What it is:** Google's HTTP/S proxy library for testing and traffic shaping.

**Why it's interesting:**
- JSON-configurable modifiers (no Go code needed to add rules):
  ```json
  {
    "url.Filter": {
      "host": "api.openai.com",
      "modifier": {
        "header.Modifier": {
          "name": "Authorization",
          "value": "Bearer sk-..."
        }
      }
    }
  }
  ```
- Compose multiple modifiers with `fifo.Group`
- Built for exactly this use case (traffic modification)

**Why not first choice:**
- Heavier dependency (Google-style, lots of packages)
- Less transparent proxy documentation
- The JSON config API is nice but we'd need to template in secret values,
  which partly defeats the purpose
- More suited to test infrastructure than production sidecars

### Option 3: `lqqyt2423/go-mitmproxy` (NOT RECOMMENDED)

Full mitmproxy reimplementation in Go. Plugin system for request modification.

**Why not:** Explicitly says "only supports setting the proxy manually in the
client, not transparent proxy mode." We need transparent (iptables-based).

### Option 4: `AdguardTeam/gomitmproxy` (VIABLE)

AdGuard's MITM proxy. Supports MITMExceptions (exclude domains from MITM).

**Why not first choice:** Designed for content filtering, not credential
injection. We'd fight the API rather than use it naturally. Less documentation
on per-domain request modification.

### Option 5: Raw `net/http` + `crypto/tls` (OVERKILL)

Eli Bendersky's blog post shows the full pattern: hijack CONNECT, forge cert,
`tls.Server()` wrap, read plaintext HTTP, modify, forward.

**Why not:** We'd be reimplementing what goproxy already does. ~500 lines of
tricky TLS code with edge cases (persistent connections, WebSocket upgrade,
chunked encoding). Not worth it.

### Option 6: mitmproxy (Python) as sidecar (NOT RECOMMENDED)

The original mitmproxy is mature and full-featured. Could run as a sidecar
process.

**Why not:** Adds Python to a Go project. Harder to embed in the Docker image.
Configuration is Python scripting, not Go-native. Debugging across languages.
Heavier resource footprint.

## Recommendation: BUILD with `elazarl/goproxy`

The core proxy logic is a solved problem. What we're building is thin:

```
credproxy/
├── main.go          # ~150 lines: load config, set up goproxy, start listener
├── config.go        # ~50 lines: parse rules JSON, resolve keys from env/secrets
├── setup.sh         # ~20 lines: iptables + CA cert install (runs at VM boot)
└── rules.json       # domain → header → env var mapping
```

### Architecture in the Sandbox

```
┌─────────────────────────────────────────────────┐
│  Fly.io VM                                       │
│                                                  │
│  ┌──────────┐      ┌──────────────┐              │
│  │ credproxy │◄─────│ iptables NAT │              │
│  │ :9999     │      │ redirect 443 │              │
│  │ (uid:proxy)│     └──────────────┘              │
│  └─────┬─────┘                                   │
│        │ only MITMs configured domains            │
│        │ passes everything else through            │
│        ▼                                          │
│   real internet                                   │
│                                                   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │ OpenCode  │  │ user app │  │ gotty    │       │
│  │ :8080     │  │ :5000    │  │ :8081    │       │
│  └──────────┘  └──────────┘  └──────────┘       │
│                                                   │
│  Keys: /run/secrets/cred-rules.json (root only)  │
│  CA:   /usr/local/share/ca-certificates/proxy.crt│
└─────────────────────────────────────────────────┘
```

### Key Security Properties

1. **Keys never on filesystem as plaintext** — credproxy reads from
   `/run/secrets/` (tmpfs mount) or env vars set by the host. The app
   user can't read them (different uid, mode 0600).
2. **App code is unmodified** — `fetch("https://api.stripe.com/v1/charges")`
   just works. The proxy is invisible.
3. **Selective MITM** — only configured domains get TLS-terminated. All
   other traffic is a blind tunnel. Minimizes attack surface.
4. **iptables uid exemption** — `! --uid-owner proxy` prevents the proxy's
   own outbound connections from looping back.

### Per-Service Injection Patterns

| Service | Domain | Header | Value Pattern |
|---------|--------|--------|---------------|
| OpenAI | api.openai.com | Authorization | `Bearer {{key}}` |
| Anthropic | api.anthropic.com | x-api-key | `{{key}}` |
| Stripe | api.stripe.com | Authorization | `Bearer {{key}}` |
| GitHub | api.github.com | Authorization | `Bearer {{key}}` |
| Supabase | *.supabase.co | apikey | `{{key}}` |
| Twilio | api.twilio.com | Authorization | `Basic {{base64(sid:token)}}` |
| SendGrid | api.sendgrid.com | Authorization | `Bearer {{key}}` |

**Won't work for:** AWS (SigV4 request signing — needs full request body
hashing). Punt on this. If someone needs AWS, they provide their own creds.

### What goproxy Gives Us for Free

- CONNECT handling + TLS termination with custom CA
- Per-domain conditions (`DstHostIs`, `ReqHostMatches`)
- Request/response modification hooks
- Transparent proxy mode (iptables-redirected traffic)
- Connection pooling and keep-alive

### What We Build

- Config format + loader (~50 lines)
- Key resolution from secrets/env (~30 lines)
- Main server wiring (~100 lines)
- `setup.sh`: CA generation, trust store, iptables (~20 lines)
- Integration into Dockerfile / entrypoint.sh
- Settings UI: per-provider key entry, stored server-side encrypted

**Total: ~200 lines of Go + 20 lines of shell. 1-2 days.**
