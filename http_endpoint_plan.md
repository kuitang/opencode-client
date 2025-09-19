# HTTP Endpoint Enhancement Plan
## Achieving SSE-Level Rendering Parity for the `/messages` Endpoint

### Executive Summary
The HTTP `/messages` endpoint can achieve complete feature parity with SSE rendering by leveraging the rich data already available from OpenCode's API. The implementation requires transforming the endpoint from a simple text concatenator to a full-featured message processor that uses the exact same rendering pipeline as SSE.

### Current State Analysis

#### SSE Pipeline (Rich Rendering)
- **Data Source**: OpenCode `/event` endpoint streaming `message.part.updated` events
- **Data Processing**: `MessagePartsManager` maintains chronological part ordering
- **Rendering Pipeline**:
  - Text parts: `renderText()` with markdown/autolink support
  - Tool parts: `renderToolDetails()` with collapsible HTML and templates
  - Reasoning parts: Simple text with emoji prefix
  - Step parts: Badge-style HTML rendering
  - Todo parts: `renderTodoList()` with structured template
- **Output**: Full `MessageData` structure rendered via `message.html` template
- **Features**: Real-time updates, rich HTML, part-level granularity

#### HTTP Endpoint (Basic Rendering)
- **Data Source**: OpenCode `/session/{id}/message` endpoint
- **Data Processing**: Simple loop extracting only text parts
- **Rendering**: Basic text concatenation, no rich formatting
- **Output**: Simple message bubbles without part structure
- **Missing**: Tool details, reasoning blocks, step badges, markdown rendering

#### OpenCode API Data Structure
The `/session/{id}/message` endpoint returns complete message objects with:
```
{
  "info": {
    "id", "role", "sessionID", "time", "cost", "tokens",
    "modelID", "providerID", "system", "mode", "path"
  },
  "parts": [
    {
      "id", "messageID", "sessionID", "type",
      "text" (for text parts),
      "tool", "callID", "state" (for tool parts with input/output/status/metadata),
      "time" (start/end timestamps)
    }
  ]
}
```

### Key Finding: Complete Data Availability
**The HTTP endpoint already has access to ALL the data needed for rich rendering.** The OpenCode API provides the same detailed part information that SSE events contain, including:
- Full tool execution states with input/output/metadata
- Part IDs for proper tracking
- Timing information
- Step tracking with token usage
- All part types (text, tool, reasoning, file, snapshot, patch, agent, step-start, step-finish)

### Implementation Strategy

#### Phase 1: Data Structure Alignment
1. **Preserve Part Structure**: Stop concatenating text parts; maintain the full parts array
2. **Type Mapping**: Map OpenCode part types to the existing MessagePartData structure
3. **Part ID Preservation**: Keep part IDs for proper identification
4. **Metadata Extraction**: Pull timing, costs, tokens from the response

#### Phase 2: Rendering Function Reuse
The existing rendering functions are FULLY REUSABLE without modification:

1. **Text Rendering**: Direct reuse of `renderText()` function
   - Already handles markdown detection via `hasMarkdownElements()`
   - Applies Blackfriday markdown rendering with autolink
   - Sanitizes with Bluemonday for security
   - Returns `template.HTML` ready for template insertion

2. **Tool Rendering**: Direct reuse of `renderToolDetails()` function
   - Accepts tool name, status, input map, output string
   - Returns complete HTML with collapsible details
   - Special handling for todowrite via `renderTodoList()`
   - Template-based rendering via `tool.html`

3. **Template Reuse**: The `message.html` template already handles all part types
   - Iterates through `.Parts` array
   - Renders each part based on type
   - Applies appropriate styling and structure

#### Phase 3: Message Processing Pipeline
Transform `handleMessages()` to mirror SSE processing:

1. **Fetch Messages**: Get from `/session/{id}/message` (existing)
2. **Process Each Message**:
   ```
   For each message in response:
     - Extract info (role, provider, model, etc.)
     - Process parts array:
       For each part:
         - Determine part type
         - Apply appropriate rendering:
           * text → renderText()
           * tool → renderToolDetails()
           * reasoning → format with emoji
           * step-start/finish → badge HTML
         - Build MessagePartData with RenderedHTML
     - Create MessageData structure
     - Render via message.html template
   ```

3. **Key Processing Details**:
   - Tool parts: Extract `state.input`, `state.output`, `state.status`
   - Text parts: Pass through `renderText()` for markdown/autolink
   - Maintain chronological order (already in API response)
   - Set alignment based on role (user=right, assistant=left)

#### Phase 4: Exact SSE Parity Mapping

##### Data Shape Comparison
**SSE Event Structure** (from message.part.updated):
```
{
  "type": "message.part.updated",
  "properties": {
    "part": {
      "id": "prt_xxx",
      "messageID": "msg_xxx",
      "sessionID": "ses_xxx",
      "type": "text|tool|reasoning|...",
      "text": "...",  // for text parts
      "tool": "name", // for tool parts
      "state": {...}  // for tool parts
    }
  }
}
```

**HTTP Response Structure** (from /session/{id}/message):
```
{
  "parts": [
    {
      "id": "prt_xxx",        // Same as SSE part.id
      "messageID": "msg_xxx",  // Same as SSE part.messageID
      "sessionID": "ses_xxx",  // Same as SSE part.sessionID
      "type": "text|tool|...", // Same as SSE part.type
      "text": "...",          // Same as SSE part.text
      "tool": "name",         // Same as SSE part.tool
      "state": {...}          // Same as SSE part.state
    }
  ]
}
```

**The structures are IDENTICAL** - HTTP responses contain the exact same part data as SSE events!

##### Rendering Function Mapping
| Part Type | SSE Rendering | HTTP Rendering (Proposed) |
|-----------|--------------|---------------------------|
| text | `renderText(part["text"])` | `renderText(part.Text)` |
| tool | `renderToolDetails(toolName, status, input, output)` | `renderToolDetails(part.Tool, part.State.Status, part.State.Input, part.State.Output)` |
| reasoning | String formatting with 🤔 | Same string formatting |
| step-start | Badge HTML template | Same badge HTML |
| step-finish | Badge HTML template | Same badge HTML |
| file | Simple content display | Same content display |
| snapshot | Simple content display | Same content display |
| patch | Simple content display | Same content display |
| agent | Simple content display | Same content display |

### Test Coverage Strategy

#### Existing Test Coverage
- **Rendering Functions**: Well-tested in `unit_test.go`
  - `renderText()`: Markdown detection, XSS prevention, autolink
  - Plain text vs markdown differentiation
  - Security sanitization
- **Todo Rendering**: Tested in `unit_test.go`
- **SSE Part Management**: Tested in `unit_test.go`
- **Basic HTTP Endpoint**: Integration test in `integration_main_test.go`

#### Required New Tests
1. **HTTP Part Processing Tests**:
   - Test each part type renders correctly
   - Verify tool state extraction
   - Confirm markdown rendering applies
   - Check template execution

2. **Parity Tests**:
   - Compare HTTP and SSE output for identical input
   - Verify HTML structure matches
   - Confirm all part types render identically

3. **Integration Tests**:
   - Full message with mixed part types
   - Tool execution with input/output
   - Reasoning and step tracking
   - Todo list rendering

#### Test Implementation Approach
1. **Create Test Helper**: Function to create mock OpenCode message responses
2. **Part-Level Tests**: Unit tests for each part type transformation
3. **Message-Level Tests**: Full message rendering validation
4. **Comparison Tests**: Side-by-side SSE vs HTTP output verification

### Implementation Checklist

#### Core Changes
- [ ] Modify `handleMessages()` to process full parts array
- [ ] Add part type detection and routing
- [ ] Integrate existing rendering functions
- [ ] Build proper MessageData structures
- [ ] Use message.html template for rendering

#### Rendering Integration
- [ ] Text parts: Call `renderText()` for each text part
- [ ] Tool parts: Extract state, call `renderToolDetails()`
- [ ] Reasoning parts: Format with emoji prefix
- [ ] Step parts: Apply badge HTML
- [ ] Todo parts: Detect todowrite, call `renderTodoList()`

#### Data Transformation
- [ ] Map OpenCode response to MessageResponse struct
- [ ] Transform parts to MessagePartData array
- [ ] Preserve part IDs and metadata
- [ ] Maintain chronological ordering

#### Testing
- [ ] Add unit tests for part processing
- [ ] Create integration tests for full messages
- [ ] Implement parity verification tests
- [ ] Test all part types individually

### Performance Considerations
1. **Rendering Overhead**: Minimal - same functions used by SSE
2. **Memory Usage**: Slightly higher due to full part storage (negligible)
3. **Processing Time**: Comparable to SSE per-message processing
4. **Template Execution**: Already optimized and cached

### Security Considerations
1. **XSS Protection**: Already handled by `renderText()` with Bluemonday
2. **HTML Sanitization**: Existing sanitization applies automatically
3. **Template Security**: Go templates provide automatic escaping
4. **No New Attack Vectors**: Reusing proven, tested rendering functions

### Migration Path
1. **Backward Compatibility**: Maintain existing endpoint behavior initially
2. **Feature Flag**: Add query parameter to enable rich rendering
3. **Gradual Rollout**: Test with subset of sessions
4. **Full Migration**: Switch to rich rendering by default
5. **Deprecation**: Remove old simple rendering path

### Conclusion
The HTTP `/messages` endpoint can achieve complete parity with SSE rendering by:
1. Using the already-available rich data from OpenCode's API
2. Reusing ALL existing rendering functions without modification
3. Following the exact same processing pipeline as SSE
4. Leveraging the same templates and HTML generation

This is not a reimplementation but rather a **reconnection** of existing components that are currently underutilized. The data is there, the rendering functions exist, and the templates are ready - they just need to be connected properly.
