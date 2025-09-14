# 004-oapi-codegen-migration.md

## Overview
Migration options from manual OpenCode API calls to oapi-codegen generated client.

## Current State
- Manual HTTP calls with `http.Get`/`http.Post`
- Manual structs like `MessageInfo`, `MessagePart`
- Type assertions from `map[string]interface{}`
- ~8 endpoints used from 20+ available

## Migration Options

### Hybrid Approach (Recommended)

**What it does:**
- Generate **types only** using `oapi-codegen -generate types`
- Keep existing HTTP handling patterns
- Replace manual structs with generated types

**Command:**
```bash
oapi-codegen -generate types -package api -o internal/api/types.go http://localhost:46219/doc
```

**Code pattern:**
```go
// Keep current HTTP calls
resp, err := http.Get(fmt.Sprintf("%s/session/%s/message", baseURL, sessionID))

// But unmarshal to generated types
var messages []api.Message  // Generated type
json.Unmarshal(body, &messages)

// Type-safe access with enums
for _, msg := range messages {
    for _, part := range msg.Parts {
        switch part.Type {  // Generated enum
        case api.MessagePartTypeTool:
            // Compile-time safety
        }
    }
}
```

### Full Generated Client

**What it does:**
- Generate complete client with `oapi-codegen -generate types,client`
- Replace all HTTP calls with client method calls

**Command:**
```bash
oapi-codegen -generate types,client -package api -o internal/api/client.go http://localhost:46219/doc
```

**Code pattern:**
```go
// Generated client interface
client, err := api.NewClient("http://localhost:46219")

// Method calls instead of HTTP
ctx := context.Background()
messages, err := client.GetSessionMessage(ctx, sessionID)

// Generated parameter structs
params := &api.PostSessionParams{Directory: &directory}
session, err := client.PostSession(ctx, params)
```

## Comparison

| Aspect | Current | Hybrid Approach | Full Generated Client |
|--------|---------|-----------------|----------------------|
| **Migration Effort** | - | Low | High |
| **Type Safety** | Manual | Data structures | Complete |
| **HTTP Patterns** | Custom | Keep existing | Replace all |
| **API Coverage** | 8 endpoints | 8 endpoints | All 20+ endpoints |
| **Dependencies** | None | None | Client library |
| **Risk Level** | - | Very low | Medium |

## Session Creation Example

### Current (Manual)
```go
resp, err := http.Post(
    fmt.Sprintf("%s/session", baseURL),
    "application/json",
    bytes.NewReader([]byte("{}")),
)
var session SessionResponse  // Manual struct
json.Unmarshal(body, &session)
```

### Hybrid Approach
```go
// Same HTTP call
resp, err := http.Post(
    fmt.Sprintf("%s/session", baseURL),
    "application/json",
    bytes.NewReader([]byte("{}")),
)
// Generated type with validation & enums
var session api.Session
json.Unmarshal(body, &session)
```

### Full Generated Client
```go
client, _ := api.NewClient(baseURL)
session, err := client.PostSession(context.Background(), &api.PostSessionParams{})
// No manual HTTP, no JSON marshaling, built-in validation
```

## SSE Event Handling Changes

### Current
```go
var event map[string]interface{}
json.Unmarshal(data, &event)
if eventType, ok := event["type"].(string); ok {
    if eventType == "message.part.updated" {
        if props, ok := event["properties"].(map[string]interface{}); ok {
            // More type assertions...
        }
    }
}
```

### With Generated Types
```go
var event api.Event
json.Unmarshal(data, &event)
switch event.Type {
case "message.part.updated":
    if event.Properties.Part != nil {
        switch event.Properties.Part.Type {
        case api.MessagePartTypeTool:
            // Direct type-safe access
        }
    }
}
```

## Recommendation

**Start with Hybrid Approach:**
- Immediate type safety benefits
- Zero disruption to working patterns
- Can upgrade to full client later
- Your HTTP patterns already work well

**Consider Full Client when:**
- Need many more endpoints than current 8
- Parameter validation becomes important
- Want complete API surface immediately
- Team needs standardized patterns

**Migration path:**
1. Implement hybrid approach first
2. Test generated types thoroughly
3. Optionally migrate to full client later