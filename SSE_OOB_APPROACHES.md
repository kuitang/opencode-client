# SSE and OOB Approaches with HTMX

## The Challenge
When using Server-Sent Events (SSE) with HTMX, we want to:
1. Initially append a message with a streaming indicator
2. Later update that same message with the final content

## Approaches

### 1. **Single Event Type with OOB** (Current Implementation)
Send both initial and update messages through the same SSE event, using `hx-swap-oob` on updates.

**How it works:**
- First message: No OOB attribute, gets appended normally
- Updates: Include `hx-swap-oob="outerHTML"`, replaces existing element

**Template:**
```html
<div id="{{.ID}}"{{if .HXSwapOOB}} hx-swap-oob="outerHTML"{{end}}>
  {{.Text}}
</div>
```

**Server code:**
```go
// Initial message
msgData := MessageData{
    ID: "assistant-123",
    Text: "Thinking...",
    IsStreaming: true,
    // No HXSwapOOB
}

// Update message  
msgData := MessageData{
    ID: "assistant-123",
    Text: "Final response",
    HXSwapOOB: true,  // Triggers replacement
}
```

**Pros:**
- Simple to implement
- Works with standard HTMX SSE

**Cons:**
- OOB in SSE has limited browser support
- May not work reliably in all scenarios

### 2. **Multiple Event Types**
Use different SSE event names for initial vs update messages.

**Index.html:**
```html
<div hx-ext="sse" 
     sse-connect="/events">
  <div sse-swap="message" hx-swap="beforeend"></div>
  <div sse-swap="update" hx-swap="none"></div>
</div>
```

**Server code:**
```go
// Initial append
fmt.Fprintf(w, "event: message\n")
fmt.Fprintf(w, "data: %s\n\n", initialHTML)

// Update with OOB
fmt.Fprintf(w, "event: update\n") 
fmt.Fprintf(w, "data: %s\n\n", oobHTML)
```

**Pros:**
- Clear separation of concerns
- Can handle different swap strategies

**Cons:**
- More complex setup
- Requires careful event routing

### 3. **Morphdom Extension**
Use the morphdom extension for intelligent DOM diffing.

**Index.html:**
```html
<script src="https://unpkg.com/htmx.org/dist/ext/morphdom-swap.js"></script>
<div hx-ext="sse,morphdom-swap" 
     sse-connect="/events"
     hx-swap="morphdom">
```

**Pros:**
- Smooth updates without flicker
- Preserves DOM state

**Cons:**
- Additional dependency
- May be overkill for simple updates

### 4. **JavaScript Bridge**
Use HTMX events to trigger custom JavaScript for updates.

**Index.html:**
```html
<div hx-ext="sse" 
     sse-connect="/events"
     hx-on::sse-message="handleSSEMessage(event)">
```

**JavaScript:**
```javascript
function handleSSEMessage(event) {
    const data = JSON.parse(event.detail.data);
    if (data.isUpdate) {
        const element = document.getElementById(data.id);
        if (element) {
            element.outerHTML = data.html;
        }
    }
}
```

**Pros:**
- Full control over update logic
- Can handle complex scenarios

**Cons:**
- Requires JavaScript
- More code to maintain

### 5. **WebSocket Alternative**
Replace SSE with WebSockets for bidirectional communication.

**Pros:**
- Full duplex communication
- Better OOB support in HTMX

**Cons:**
- More complex setup
- Requires different server implementation

## Recommendation

For the OpenCode client, **Approach 1** (Single Event with OOB) is recommended because:
- It's the simplest to implement
- Works with current HTMX version
- Minimal changes to existing code
- OOB support in SSE is improving in HTMX

If reliability issues persist, consider **Approach 2** (Multiple Event Types) as it provides better control over the update flow while still using SSE.