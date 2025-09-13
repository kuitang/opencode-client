# File Updates via SSE Out-of-Band Swaps

## Problem Evolution

Initially, the Code tab had two separate issues:

### 1. Manual Dropdown Refresh Issues
- **Dropdown closing on refresh**: When using HTMX to replace the entire `<select>` element, it would close immediately after opening
- **Network requests not firing**: Event delegation with wrapper divs wasn't triggering properly
- **Selection not preserved**: User's current selection was lost when refreshing

### 2. Stale File/Line Counts
- File count and line count statistics remained at initial load values
- No automatic updates when files were created/modified
- Required full page refresh to see updated counts
- Performance concern: Scanning all files on every dropdown click would be expensive

## Complete Solution: SSE Out-of-Band Updates

The solution combines two techniques:

### 1. Manual Dropdown Refresh (User-Triggered)
Replace only the `<option>` elements inside the select, not the entire select element. This preserves the dropdown's open state while updating its contents. **Additionally updates file and line counters via OOB swaps**.

**Note**: Manual refresh remains necessary because:
- SSE updates only trigger when OpenCode detects file changes
- Files might be modified outside of OpenCode (e.g., manual edits)
- Provides immediate feedback when user clicks dropdown
- Ensures file list AND counters are always current when user interacts
- Updates all three elements (dropdown, file count, line count) atomically

### 2. Automatic Updates via SSE (Push-Based)
Use Server-Sent Events with HTMX Out-of-Band swaps to automatically update:
- File counts
- Line counts
- Dropdown options
- Code content placeholder (shows "Select a File" when files arrive)

## Implementation Details

### Part 1: Manual Dropdown Refresh

#### Frontend - Template Changes

**File: `templates/tabs/code.html`**
```html
<select id="file-selector"
        class="..."
        name="path"
        hx-get="/tab/code/file"
        hx-trigger="change"
        hx-target="#code-content"
        hx-swap="innerHTML"
        onfocus="htmx.ajax('GET', '/tab/code/filelist?options_only=true&current=' + encodeURIComponent(this.value), {target: this, swap: 'innerHTML'})">
    <!-- Options populated here -->
</select>
```

Key aspects:
- Uses `onfocus` event to trigger refresh when dropdown is clicked
- Calls `htmx.ajax()` programmatically with `options_only=true` parameter
- Passes current selection via `current` parameter
- Uses `swap: 'innerHTML'` to replace only the options, not the select element

#### Backend - Server Handler (Enhanced)

**File: `main.go`**
```go
func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
    currentPath := r.URL.Query().Get("current")
    optionsOnly := r.URL.Query().Get("options_only") == "true"

    files, err := s.fetchAllFiles()
    // ... error handling ...

    // Calculate line count for manual refresh
    lineCount := s.calculateLineCount()

    data := struct {
        Files       []FileNode
        FileCount   int
        LineCount   int
        CurrentPath string
    }{
        Files:       files,
        FileCount:   len(files),
        LineCount:   lineCount,
        CurrentPath: currentPath,
    }

    if optionsOnly {
        // Return options WITH OOB counter updates
        s.templates.ExecuteTemplate(w, "file-options-with-counts", data)
    } else {
        // Return the full select element
        s.templates.ExecuteTemplate(w, "file-dropdown", data)
    }
}
```

#### Template Partials

**File: `templates/tabs/code-partials.html`**
```html
{{define "file-options"}}
{{if .Files}}
    <option value="">Select a file...</option>
    {{range .Files}}
    <option value="{{.Path}}" {{if eq .Path $.CurrentPath}}selected{{end}}>{{.Path}}</option>
    {{end}}
{{else}}
    <option value="">No files generated</option>
{{end}}
{{end}}

{{define "file-options-with-counts"}}
{{if .Files}}
    <option value="">Select a file...</option>
    {{range .Files}}
    <option value="{{.Path}}" {{if eq .Path $.CurrentPath}}selected{{end}}>{{.Path}}</option>
    {{end}}
{{else}}
    <option value="">No files generated</option>
{{end}}
<!-- OOB updates for file and line counts -->
<div id="file-count-container" hx-swap-oob="innerHTML">
    <p class="text-2xl font-semibold text-gray-900">{{.FileCount}}</p>
    <p class="text-sm text-gray-600">Files</p>
</div>
<div id="line-count-container" hx-swap-oob="innerHTML">
    <p class="text-2xl font-semibold text-gray-900">{{.LineCount}}</p>
    <p class="text-sm text-gray-600">Lines of Code</p>
</div>
{{end}}
```

### Part 2: Automatic SSE Out-of-Band Updates

#### The Challenge with HTMX SSE

HTMX's SSE extension historically didn't support OOB swaps natively (unlike the WebSocket extension). The solution: use multiple SSE event listeners with different event names.

#### SSE Listener Setup

**File: `templates/index.html`**
```html
<!-- Main message stream -->
<div id="messages"
     hx-ext="sse"
     sse-connect="/events"
     sse-swap="message"
     hx-swap="beforeend scroll:bottom">
</div>

<!-- Hidden listener for code tab updates -->
<div style="display:none"
     hx-ext="sse"
     sse-connect="/events"
     sse-swap="code-updates">
</div>
```

#### Combined OOB Template (Enhanced)

**File: `templates/tabs/code-oob-partials.html`**
```html
{{define "code-updates-oob"}}
<!-- File stats update -->
<div id="file-count-container" hx-swap-oob="innerHTML">
    <p class="text-2xl font-semibold text-gray-900">{{.FileCount}}</p>
    <p class="text-sm text-gray-600">Files</p>
</div>
<div id="line-count-container" hx-swap-oob="innerHTML">
    <p class="text-2xl font-semibold text-gray-900">{{.LineCount}}</p>
    <p class="text-sm text-gray-600">Lines of Code</p>
</div>
<!-- File dropdown update with selection preservation -->
<div id="file-selector-wrapper" hx-swap-oob="innerHTML">
    <script>
        // Preserve current selection before update
        const currentSelect = document.querySelector('#file-selector');
        const currentValue = currentSelect ? currentSelect.value : '';
        const hadFocus = currentSelect === document.activeElement;
    </script>
    <select id="file-selector" ...>
        {{if .Files}}
            <option value="">Select a file...</option>
            {{range .Files}}
            <option value="{{.Path}}">{{.Path}}</option>
            {{end}}
        {{else}}
            <option value="">No files generated</option>
        {{end}}
    </select>
    <script>
        // Restore selection and focus after update
        const newSelect = document.querySelector('#file-selector');
        if (newSelect && currentValue) {
            const optionExists = Array.from(newSelect.options).some(opt => opt.value === currentValue);
            if (optionExists) {
                newSelect.value = currentValue;
            }
        }
        if (hadFocus && newSelect) {
            newSelect.focus();
        }
    </script>
</div>
<!-- Code content placeholder update when files arrive but none selected -->
{{if and .Files (eq .CurrentPath "")}}
<div id="code-content" hx-swap-oob="innerHTML">
    <div class="text-center py-20">
        <svg class="w-16 h-16 mx-auto text-gray-500 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4"/>
        </svg>
        <h3 class="text-lg font-medium text-gray-300 mb-2">Select a File</h3>
        <p class="text-gray-500 max-w-md mx-auto">Select a file from the dropdown above to view its contents.</p>
    </div>
</div>
{{end}}
{{end}}
```

#### File Change Detection

**File: `main.go` - SSE Handler**
```go
// In handleSSE function, when processing message parts:
var hasFileChanges bool
for _, msgPart := range completeParts {
    if msgPart.Type == "text" {
        completeText.WriteString(msgPart.Content)
    } else if msgPart.Type == "tool" {
        completeText.WriteString("\n\n" + msgPart.Content)
        // Check if tool might have created/modified files
        if strings.Contains(msgPart.Content, "created") ||
           strings.Contains(msgPart.Content, "wrote") ||
           strings.Contains(msgPart.Content, "saved") {
            hasFileChanges = true
        }
    } else if msgPart.Type == "step-finish" {
        isStreaming = false
        hasFileChanges = true // Assume files may have changed when step completes
    }
}

// Send code tab updates if files changed and streaming finished
if hasFileChanges && !isStreaming {
    s.sendCodeTabUpdates(w, flusher, "")
}
```

#### Sending Combined Updates

**File: `main.go`**
```go
func (s *Server) sendCodeTabUpdates(w http.ResponseWriter, flusher http.Flusher, currentPath string) {
    // Fetch all files once
    files, err := s.fetchAllFiles()
    if err != nil {
        log.Printf("Failed to fetch files for code tab update: %v", err)
        return
    }

    // Calculate line count
    lineCount := s.calculateLineCount()

    // Prepare combined data
    data := struct {
        Files       []FileNode
        FileCount   int
        LineCount   int
        CurrentPath string
    }{
        Files:       files,
        FileCount:   len(files),
        LineCount:   lineCount,
        CurrentPath: currentPath,
    }

    // Render the combined OOB update template
    var buf bytes.Buffer
    if err := s.templates.ExecuteTemplate(&buf, "code-updates-oob", data); err != nil {
        log.Printf("Failed to render code updates OOB: %v", err)
        return
    }

    // Send as single SSE event
    fmt.Fprintf(w, "event: code-updates\n")
    lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
    for _, line := range lines {
        fmt.Fprintf(w, "data: %s\n", line)
    }
    fmt.Fprintf(w, "\n")
    flusher.Flush()
    log.Printf("Sent code tab update: %d files, %d lines", data.FileCount, data.LineCount)
}
```

## Why This Solution Works

### Manual Refresh Benefits
1. **Preserves dropdown state**: By only replacing innerHTML, the `<select>` element itself remains unchanged, keeping its open/closed state intact.
2. **Selection preservation**: The server checks `CurrentPath` and adds `selected` attribute to the matching option.
3. **Clean HTMX integration**: Uses HTMX's JavaScript API (`htmx.ajax()`) for programmatic requests.

### SSE OOB Benefits
1. **Push-based updates**: Server notifies client immediately when files change, no polling needed.
2. **Efficient batching**: Single SSE event updates all three UI elements (file count, line count, dropdown).
3. **No interference**: Different event names (`message` vs `code-updates`) ensure chat and code updates don't conflict.
4. **Automatic timing**: Updates trigger only when assistant completes work (`step-finish`), not during intermediate steps.
5. **Performance optimized**: File scanning happens only once per update, results shared across all UI elements.

## Testing

### Manual Testing with Playwright
```javascript
// Test dropdown refresh preserves selection
await page.click('#file-selector');  // Open dropdown
// Verify options update while dropdown stays open
await page.waitForFunction(() =>
  document.querySelector('#file-selector option[value="newfile.txt"]')
);

// Test SSE updates
await page.fill('#message-input', 'Create a file test.txt');
await page.click('button[type="submit"]');
// Wait for automatic updates
await page.waitForFunction(
  () => document.querySelector('#file-count-container p').textContent !== '0'
);
```

### Verified Behavior
- ✅ File count updates automatically when files are created
- ✅ Line count updates in sync with file changes
- ✅ Dropdown gets new files without manual refresh
- ✅ Selection preserved during SSE updates (via JavaScript preservation)
- ✅ Focus state preserved during SSE updates
- ✅ Code placeholder updates to show "Select a File" when files arrive
- ✅ No interference with chat message streaming
- ✅ Updates only trigger after assistant completes work

### Edge Cases Handled

1. **Simultaneous Dropdown Selection and Message Sending**
   - User's selection is preserved when SSE updates arrive
   - Dropdown focus state is maintained
   - JavaScript preservation ensures selected file remains selected

2. **Rapid Dropdown Interactions During SSE**
   - Manual refresh (onfocus) and SSE updates don't conflict
   - Both update mechanisms can coexist safely

3. **File Deletion Handling**
   - If selected file is deleted, selection resets to placeholder
   - Graceful fallback when option no longer exists

## Alternative Approaches Considered

1. **Polling**: Regular interval updates - wasteful and creates unnecessary load
2. **Single SSE event**: Tried to piggyback on message events - caused coupling issues
3. **WebSocket**: Overkill for one-way server-to-client updates
4. **Manual refresh only**: Required user action, poor UX

## Analysis: Can Manual Refresh Be Removed?

After careful analysis and testing, **manual refresh should be retained** for the following reasons:

1. **Immediate User Feedback**: Manual refresh provides instant response when user clicks dropdown
2. **External File Changes**: Files modified outside OpenCode (e.g., manual edits, git operations) need manual refresh to appear
3. **Network Reliability**: SSE connection might be interrupted; manual refresh ensures fallback
4. **User Control**: Users expect dropdowns to refresh on interaction
5. **No Performance Impact**: Manual refresh only triggers on user action (onfocus), not automatically

The dual approach (manual + SSE) provides the best UX:
- SSE handles automatic updates when files change via OpenCode
- Manual refresh ensures current state on user interaction
- Both mechanisms complement rather than conflict

## Lessons Learned

1. **HTMX SSE Limitations**: SSE extension doesn't natively support OOB like WebSocket extension does. Workaround: use multiple listeners with different event names.

2. **Event Timing Matters**: Detecting "step-finish" ensures updates happen after work completes, not during intermediate processing.

3. **Template Reuse**: Combining all updates in one template (`code-updates-oob`) simplifies maintenance and ensures consistency.

4. **Performance Considerations**: Batching all updates into one SSE event reduces network overhead and DOM manipulation.

5. **Selection Preservation**: JavaScript-based preservation is more reliable than server-side tracking for client-side state.

6. **Dual Update Strategy**: Manual and automatic updates serve different purposes and should coexist.

## Future Improvements

1. **Debouncing**: Could add debouncing if multiple file operations happen in quick succession
2. **Selective Updates**: Track which files changed to update only affected elements
3. **Progress Indicators**: Show loading state during file operations
4. **Error Handling**: Add retry logic for failed SSE connections