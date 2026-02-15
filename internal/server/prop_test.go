package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"opencode-chat/internal/auth"
	"opencode-chat/internal/middleware"
	"opencode-chat/internal/models"
	"opencode-chat/internal/sandbox"
	"opencode-chat/internal/sse"
	"opencode-chat/internal/views"
)

// ===================== Generators =====================

// genXSSPayload generates potentially malicious HTML/JS strings.
func genXSSPayload() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.Just("<script>alert('XSS')</script>"),
		rapid.Just("<img src=x onerror=alert(1)>"),
		rapid.Just("<svg onload=alert(1)>"),
		rapid.Just("<body onload=alert(1)>"),
		rapid.Just("<a href='javascript:alert(1)'>click</a>"),
		rapid.Just("<div style='background:url(javascript:alert(1))'>"),
		rapid.Just("<input onfocus=alert(1) autofocus>"),
		rapid.Just("<iframe src='javascript:alert(1)'></iframe>"),
		rapid.Just("<form action='javascript:alert(1)'><input></form>"),
		rapid.Just("<a href='data:text/html,<script>alert(1)</script>'>click</a>"),
		rapid.Custom(func(t *rapid.T) string {
			tag := rapid.SampledFrom([]string{"div", "span", "img", "a", "body", "svg", "input"}).Draw(t, "tag")
			handler := rapid.SampledFrom([]string{"onclick", "onerror", "onload", "onmouseover", "onfocus", "onblur"}).Draw(t, "handler")
			return fmt.Sprintf("<%s %s=alert(1)>test</%s>", tag, handler, tag)
		}),
		rapid.Custom(func(t *rapid.T) string {
			text := rapid.StringOf(rapid.RuneFrom([]rune("abcde "))).Draw(t, "filler")
			return "<script>" + text + "</script>"
		}),
	)
}

// genAlphanumText generates text with no leading/trailing spaces (for markdown inline formatting).
func genAlphanumText() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		length := rapid.IntRange(1, 50).Draw(t, "length")
		var sb strings.Builder
		for i := 0; i < length; i++ {
			idx := rapid.IntRange(0, len(chars)-1).Draw(t, fmt.Sprintf("char%d", i))
			sb.WriteByte(chars[idx])
		}
		return sb.String()
	})
}

// genMarkdownText generates valid markdown text.
func genMarkdownText() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.Custom(func(t *rapid.T) string {
			level := rapid.IntRange(1, 6).Draw(t, "headerLevel")
			text := genAlphanumText().Draw(t, "headerText")
			return strings.Repeat("#", level) + " " + text
		}),
		rapid.Custom(func(t *rapid.T) string {
			text := genAlphanumText().Draw(t, "boldText")
			return "**" + text + "**"
		}),
		rapid.Custom(func(t *rapid.T) string {
			text := genAlphanumText().Draw(t, "italicText")
			return "*" + text + "*"
		}),
		rapid.Custom(func(t *rapid.T) string {
			n := rapid.IntRange(1, 5).Draw(t, "listLen")
			var items []string
			for i := 0; i < n; i++ {
				item := genPlainText().Draw(t, fmt.Sprintf("listItem%d", i))
				items = append(items, "- "+item)
			}
			return strings.Join(items, "\n")
		}),
		rapid.Custom(func(t *rapid.T) string {
			code := genPlainText().Draw(t, "codeContent")
			return "```\n" + code + "\n```"
		}),
		rapid.Custom(func(t *rapid.T) string {
			text := genAlphanumText().Draw(t, "inlineCode")
			return "Use `" + text + "` here"
		}),
		rapid.Custom(func(t *rapid.T) string {
			text := genPlainText().Draw(t, "linkText")
			return "[" + text + "](https://example.com)"
		}),
	)
}

// genPlainText generates safe alphanumeric text (always non-empty).
func genPlainText() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
		length := rapid.IntRange(1, 100).Draw(t, "length")
		var sb strings.Builder
		for i := 0; i < length; i++ {
			idx := rapid.IntRange(0, len(chars)-1).Draw(t, fmt.Sprintf("char%d", i))
			sb.WriteByte(chars[idx])
		}
		return sb.String()
	})
}

// genPartID generates valid part IDs (non-empty strings).
func genPartID() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		suffix := rapid.IntRange(1, 999999).Draw(t, "partSuffix")
		return fmt.Sprintf("prt_%06d", suffix)
	})
}

// genMessageID generates valid message IDs (non-empty strings).
func genMessageID() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		suffix := rapid.IntRange(1, 999999).Draw(t, "msgSuffix")
		return fmt.Sprintf("msg_%06d", suffix)
	})
}

// genSessionID generates valid session IDs.
func genSessionID() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		suffix := rapid.IntRange(1, 999999).Draw(t, "sesSuffix")
		return fmt.Sprintf("ses_%06d", suffix)
	})
}

// genPartType generates valid part types.
func genPartType() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{"text", "tool", "reasoning", "step-start", "step-finish", "file", "snapshot", "patch", "agent"})
}

// genMessagePartData generates MessagePartData with random but valid fields.
func genMessagePartData() *rapid.Generator[views.MessagePartData] {
	return rapid.Custom(func(t *rapid.T) views.MessagePartData {
		partType := genPartType().Draw(t, "partType")
		content := genPlainText().Draw(t, "partContent")
		partID := genPartID().Draw(t, "partID")
		return views.MessagePartData{
			Type:    partType,
			Content: content,
			PartID:  partID,
		}
	})
}

// ===================== Property Tests =====================

// TestPropUserAndLLMRenderingSame verifies that for any content, rendering as
// user vs LLM produces the same textual content (only alignment differs).
func TestPropUserAndLLMRenderingSame(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		content := genPlainText().Draw(t, "content")
		rendered := views.RenderText(content)

		userMsg := views.MessageData{
			Alignment: "right",
			Parts:     []views.MessagePartData{{Type: "text", Content: content, RenderedHTML: rendered}},
		}
		llmMsg := views.MessageData{
			Alignment: "left",
			Parts:     []views.MessagePartData{{Type: "text", Content: content, RenderedHTML: rendered}},
		}

		userHTML, err := views.RenderMessage(tmpl, userMsg)
		if err != nil {
			t.Fatalf("User message rendering failed: %v", err)
		}
		llmHTML, err := views.RenderMessage(tmpl, llmMsg)
		if err != nil {
			t.Fatalf("LLM message rendering failed: %v", err)
		}

		// The rendered HTML for the same content should produce identical output
		// except for alignment classes. Extract the rendered content portion
		// (the RenderedHTML is identical, so the inner content must match).
		// Property: trimmed content should survive in both renderings.
		trimmed := strings.TrimSpace(content)
		if trimmed != "" {
			if !strings.Contains(userHTML, trimmed) {
				t.Fatalf("User HTML missing trimmed content %q", trimmed)
			}
			if !strings.Contains(llmHTML, trimmed) {
				t.Fatalf("LLM HTML missing trimmed content %q", trimmed)
			}
		}

		// Alignment should differ: user=right=justify-end, llm=left=justify-start
		if strings.Contains(userHTML, "justify-start") {
			t.Fatalf("User message should not have justify-start")
		}
		if strings.Contains(llmHTML, "justify-end") {
			t.Fatalf("LLM message should not have justify-end")
		}

		// Both should render successfully (no panics, no errors above)
		if userHTML == "" || llmHTML == "" {
			t.Fatalf("Rendering should produce non-empty output")
		}
	})
}

// TestPropChainMiddlewareOrder verifies that for N middlewares, they execute
// in the correct nested order: [1-before, 2-before, ..., handler, ..., 2-after, 1-after].
func TestPropChainMiddlewareOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 10).Draw(t, "numMiddlewares")
		var order []int

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, 0) // 0 represents the handler
		})

		var middlewares []middleware.Middleware
		for i := 1; i <= n; i++ {
			idx := i // capture loop variable
			mw := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					order = append(order, idx)     // before
					next.ServeHTTP(w, r)
					order = append(order, -idx)    // after (negative = after)
				})
			}
			middlewares = append(middlewares, mw)
		}

		req := httptest.NewRequest("GET", "/", nil)
		recorder := httptest.NewRecorder()
		middleware.ChainMiddleware(handler, middlewares...).ServeHTTP(recorder, req)

		// Expected: [1, 2, ..., N, 0, -N, ..., -2, -1]
		expectedLen := 2*n + 1
		if len(order) != expectedLen {
			t.Fatalf("expected %d entries, got %d: %v", expectedLen, len(order), order)
		}
		// Before: 1..N
		for i := 0; i < n; i++ {
			if order[i] != i+1 {
				t.Fatalf("before[%d]: expected %d, got %d (order=%v)", i, i+1, order[i], order)
			}
		}
		// Handler
		if order[n] != 0 {
			t.Fatalf("handler position: expected 0, got %d (order=%v)", order[n], order)
		}
		// After: -N..-1
		for i := 0; i < n; i++ {
			expected := -(n - i)
			if order[n+1+i] != expected {
				t.Fatalf("after[%d]: expected %d, got %d (order=%v)", i, expected, order[n+1+i], order)
			}
		}
	})
}

// TestPropMessagePartsOrdering verifies that parts maintain first-seen insertion
// order for any sequence of updates.
func TestPropMessagePartsOrdering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numParts := rapid.IntRange(1, 20).Draw(t, "numParts")
		manager := sse.NewMessagePartsManager()
		msgID := "test-msg"

		// Insert parts in order
		partIDs := make([]string, numParts)
		for i := 0; i < numParts; i++ {
			partIDs[i] = fmt.Sprintf("part_%d", i)
			err := manager.UpdatePart(msgID, partIDs[i], views.MessagePartData{
				Type:    "text",
				Content: fmt.Sprintf("initial-%d", i),
			})
			if err != nil {
				t.Fatalf("initial insert failed: %v", err)
			}
		}

		// Now update parts in random order
		numUpdates := rapid.IntRange(0, 50).Draw(t, "numUpdates")
		for j := 0; j < numUpdates; j++ {
			idx := rapid.IntRange(0, numParts-1).Draw(t, fmt.Sprintf("updateIdx%d", j))
			content := fmt.Sprintf("updated-%d-%d", idx, j)
			err := manager.UpdatePart(msgID, partIDs[idx], views.MessagePartData{
				Type:    "text",
				Content: content,
			})
			if err != nil {
				t.Fatalf("update failed: %v", err)
			}
		}

		// Verify order is always the original insertion order
		parts := manager.GetParts(msgID)
		if len(parts) != numParts {
			t.Fatalf("expected %d parts, got %d", numParts, len(parts))
		}
		for i, part := range parts {
			if part.PartID != partIDs[i] {
				t.Fatalf("order violated at position %d: expected %s, got %s", i, partIDs[i], part.PartID)
			}
		}
	})
}

// TestPropMessagePartsValidation verifies that empty message IDs or part IDs
// always produce errors, while non-empty IDs always succeed.
func TestPropMessagePartsValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		manager := sse.NewMessagePartsManager()

		// Generate a choice: 0=both valid, 1=empty msgID, 2=empty partID, 3=both empty
		choice := rapid.IntRange(0, 3).Draw(t, "emptyChoice")

		var msgID, partID string
		var expectErr bool

		switch choice {
		case 0:
			msgID = genMessageID().Draw(t, "validMsgID")
			partID = genPartID().Draw(t, "validPartID")
			expectErr = false
		case 1:
			msgID = ""
			partID = genPartID().Draw(t, "validPartID")
			expectErr = true
		case 2:
			msgID = genMessageID().Draw(t, "validMsgID")
			partID = ""
			expectErr = true
		case 3:
			msgID = ""
			partID = ""
			expectErr = true
		}

		err := manager.UpdatePart(msgID, partID, views.MessagePartData{
			Type:    "text",
			Content: "test",
		})

		if (err != nil) != expectErr {
			t.Fatalf("choice=%d msgID=%q partID=%q: expected error=%v, got error=%v",
				choice, msgID, partID, expectErr, err)
		}
	})
}

// TestPropValidateAndExtractMessagePart verifies that validity depends on
// correct type + matching session + non-empty IDs.
func TestPropValidateAndExtractMessagePart(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		targetSession := genSessionID().Draw(t, "targetSession")

		// Pick which invariant to test
		scenario := rapid.IntRange(0, 4).Draw(t, "scenario")

		var event map[string]any
		var expectErr bool
		var errSubstring string

		switch scenario {
		case 0:
			// Valid event: correct type, matching session, non-empty IDs
			msgID := genMessageID().Draw(t, "msgID")
			partID := genPartID().Draw(t, "partID")
			event = map[string]any{
				"type": "message.part.updated",
				"properties": map[string]any{
					"part": map[string]any{
						"sessionID": targetSession,
						"messageID": msgID,
						"id":        partID,
					},
				},
			}
			expectErr = false

		case 1:
			// Wrong event type
			event = map[string]any{
				"type":       "some.other.event",
				"properties": map[string]any{},
			}
			expectErr = true
			errSubstring = "not a message.part.updated event"

		case 2:
			// Session mismatch
			otherSession := genSessionID().Draw(t, "otherSession")
			// Ensure it actually differs
			if otherSession == targetSession {
				otherSession = otherSession + "-other"
			}
			event = map[string]any{
				"type": "message.part.updated",
				"properties": map[string]any{
					"part": map[string]any{
						"sessionID": otherSession,
						"messageID": genMessageID().Draw(t, "msgID"),
						"id":        genPartID().Draw(t, "partID"),
					},
				},
			}
			expectErr = true
			errSubstring = "session mismatch"

		case 3:
			// Empty messageID
			event = map[string]any{
				"type": "message.part.updated",
				"properties": map[string]any{
					"part": map[string]any{
						"sessionID": targetSession,
						"messageID": "",
						"id":        genPartID().Draw(t, "partID"),
					},
				},
			}
			expectErr = true
			errSubstring = "invalid or missing messageID"

		case 4:
			// Empty partID
			event = map[string]any{
				"type": "message.part.updated",
				"properties": map[string]any{
					"part": map[string]any{
						"sessionID": targetSession,
						"messageID": genMessageID().Draw(t, "msgID"),
						"id":        "",
					},
				},
			}
			expectErr = true
			errSubstring = "invalid or missing partID"
		}

		msgID, partID, _, err := sse.ValidateAndExtractMessagePart(event, targetSession)

		if (err != nil) != expectErr {
			t.Fatalf("scenario=%d: expected error=%v, got error=%v", scenario, expectErr, err)
		}
		if err != nil && errSubstring != "" && !strings.Contains(err.Error(), errSubstring) {
			t.Fatalf("scenario=%d: error %q does not contain %q", scenario, err.Error(), errSubstring)
		}
		if !expectErr && (msgID == "" || partID == "") {
			t.Fatalf("scenario=%d: valid event should return non-empty IDs, got msgID=%q partID=%q",
				scenario, msgID, partID)
		}
	})
}

// TestPropMessageFormatting verifies that any markdown renders without error
// and produces non-empty HTML. Bold/italic/headers produce expected tags.
func TestPropMessageFormatting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		content := genMarkdownText().Draw(t, "content")
		result := views.RenderText(content)

		// Should never be empty for non-empty input
		if string(result) == "" {
			t.Fatalf("RenderText returned empty for input %q", content)
		}

		htmlStr := string(result)

		// Check structural properties based on markdown type.
		// Bold: **text** should produce <strong> when inner text is non-whitespace
		if strings.HasPrefix(content, "**") && strings.HasSuffix(content, "**") && len(content) > 4 {
			inner := content[2 : len(content)-2]
			if strings.TrimSpace(inner) != "" {
				if !strings.Contains(htmlStr, "<strong>") {
					t.Fatalf("bold markdown did not produce <strong> tag: input=%q html=%q", content, htmlStr)
				}
			}
		}
		// Headers: # text should produce <h1>
		if strings.HasPrefix(content, "# ") && len(content) > 2 {
			if !strings.Contains(htmlStr, "<h1") {
				t.Fatalf("h1 markdown did not produce <h1> tag: input=%q html=%q", content, htmlStr)
			}
		}
		// Code blocks: ``` should produce <pre> or <code>
		if strings.Contains(content, "```") {
			if !strings.Contains(htmlStr, "<pre") && !strings.Contains(htmlStr, "<code") {
				t.Fatalf("code block markdown did not produce <pre>/<code> tag: input=%q html=%q", content, htmlStr)
			}
		}
		// Links: [text](url) should produce <a href>
		if strings.Contains(content, "](https://example.com)") {
			if !strings.Contains(htmlStr, "<a href") {
				t.Fatalf("link markdown did not produce <a> tag: input=%q html=%q", content, htmlStr)
			}
		}
		// Inline code: `text` should produce <code>
		if strings.Contains(content, "Use `") && strings.Contains(content, "` here") {
			if !strings.Contains(htmlStr, "<code") {
				t.Fatalf("inline code markdown did not produce <code> tag: input=%q html=%q", content, htmlStr)
			}
		}
		// Lists: lines starting with "- " should produce <li>
		if strings.HasPrefix(content, "- ") {
			if !strings.Contains(htmlStr, "<li") {
				t.Fatalf("list markdown did not produce <li> tag: input=%q html=%q", content, htmlStr)
			}
		}
	})
}

// TestPropHTMLEscapingInTemplates verifies that no XSS payload survives
// Go template auto-escaping. The key property is that user-provided content
// never appears inside unescaped HTML tags in the output.
func TestPropHTMLEscapingInTemplates(t *testing.T) {
	tmpl := template.Must(template.New("test").Parse(`<div>{{.Content}}</div>`))

	rapid.Check(t, func(t *rapid.T) {
		payload := genXSSPayload().Draw(t, "xss")

		var buf strings.Builder
		data := struct{ Content string }{Content: payload}
		if err := tmpl.Execute(&buf, data); err != nil {
			t.Fatalf("Template execution failed: %v", err)
		}
		rendered := buf.String()

		// The key security property: after Go template escaping, the only
		// literal < and > in the output should be our wrapper <div> and </div>.
		// All user content angle brackets must be escaped to &lt; and &gt;.
		// Extract the inner content between our <div> and </div> wrapper.
		inner := rendered
		if idx := strings.Index(inner, "<div>"); idx >= 0 {
			inner = inner[idx+5:]
		}
		if idx := strings.LastIndex(inner, "</div>"); idx >= 0 {
			inner = inner[:idx]
		}

		// The inner content must not contain any unescaped HTML tags
		if strings.Contains(inner, "<") || strings.Contains(inner, ">") {
			t.Fatalf("Unescaped HTML found in template output for payload %q\nInner content: %s",
				payload, inner)
		}

		// Additionally verify the payload's dangerous characters were escaped
		if strings.Contains(payload, "<") {
			if !strings.Contains(rendered, "&lt;") {
				t.Fatalf("Expected &lt; escaping for payload %q\nRendered: %s", payload, rendered)
			}
		}
		if strings.Contains(payload, ">") {
			if !strings.Contains(rendered, "&gt;") {
				t.Fatalf("Expected &gt; escaping for payload %q\nRendered: %s", payload, rendered)
			}
		}
	})
}

// TestPropNewlinePreservation verifies that all lines survive rendering
// regardless of separator style or count.
func TestPropNewlinePreservation(t *testing.T) {
	tmpl := template.Must(template.New("test").Parse(`<div class="preserve-breaks">{{.}}</div>`))

	rapid.Check(t, func(t *rapid.T) {
		numLines := rapid.IntRange(1, 10).Draw(t, "numLines")
		lines := make([]string, numLines)
		for i := 0; i < numLines; i++ {
			lines[i] = genPlainText().Draw(t, fmt.Sprintf("line%d", i))
		}

		// Choose separator
		sep := rapid.SampledFrom([]string{"\n", "\r\n"}).Draw(t, "separator")
		input := strings.Join(lines, sep)

		var buf strings.Builder
		if err := tmpl.Execute(&buf, input); err != nil {
			t.Fatalf("Template execution failed: %v", err)
		}
		rendered := buf.String()

		// Every line's content should survive in the rendered output
		for i, line := range lines {
			// Template escaping may change < > & etc. but our genPlainText only has alphanumeric+space
			if !strings.Contains(rendered, line) {
				t.Fatalf("Line %d %q not found in rendered output\nInput: %q\nRendered: %q",
					i, line, input, rendered)
			}
		}
	})
}

// TestPropMessagePartsPrefixStability verifies that the prefix of parts never
// changes on update: only the content of an existing part or new parts at
// the end can change.
func TestPropMessagePartsPrefixStability(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		manager := sse.NewMessagePartsManager()
		msgID := "stability-test"
		numOps := rapid.IntRange(2, 30).Draw(t, "numOps")

		var knownPartIDs []string
		var snapshots [][]string // snapshots of partID order

		for i := 0; i < numOps; i++ {
			// Either update existing or add new
			var partID string
			if len(knownPartIDs) > 0 && rapid.IntRange(0, 2).Draw(t, fmt.Sprintf("choice%d", i)) < 2 {
				// Update existing
				idx := rapid.IntRange(0, len(knownPartIDs)-1).Draw(t, fmt.Sprintf("existIdx%d", i))
				partID = knownPartIDs[idx]
			} else {
				// New part
				partID = fmt.Sprintf("part_%d", i)
				knownPartIDs = append(knownPartIDs, partID)
			}

			content := fmt.Sprintf("content-%d", i)
			err := manager.UpdatePart(msgID, partID, views.MessagePartData{
				Type:    "text",
				Content: content,
			})
			if err != nil {
				t.Fatalf("update failed: %v", err)
			}

			// Record current order
			parts := manager.GetParts(msgID)
			var ids []string
			for _, p := range parts {
				ids = append(ids, p.PartID)
			}
			snapshots = append(snapshots, ids)
		}

		// Verify prefix stability: each snapshot's order must be a prefix of the next
		for i := 0; i < len(snapshots)-1; i++ {
			prev := snapshots[i]
			next := snapshots[i+1]
			if len(next) < len(prev) {
				t.Fatalf("snapshot %d has %d parts but snapshot %d has %d parts (parts disappeared)",
					i, len(prev), i+1, len(next))
			}
			// The first len(prev) elements must match
			for j := 0; j < len(prev); j++ {
				if prev[j] != next[j] {
					t.Fatalf("prefix violated at step %d->%d position %d: %q != %q\nprev=%v\nnext=%v",
						i, i+1, j, prev[j], next[j], prev, next)
				}
			}
		}
	})
}

// TestPropRenderTodoList verifies that valid JSON arrays always render successfully,
// and invalid JSON always falls back to <pre>.
func TestPropRenderTodoList(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		isValid := rapid.Bool().Draw(t, "isValidJSON")

		if isValid {
			// Generate a valid JSON array of todo items
			numItems := rapid.IntRange(0, 10).Draw(t, "numItems")
			items := make([]views.TodoItem, numItems)
			for i := 0; i < numItems; i++ {
				items[i] = views.TodoItem{
					Content: genPlainText().Draw(t, fmt.Sprintf("todoContent%d", i)),
					Status: rapid.SampledFrom([]string{
						"pending", "in_progress", "completed",
					}).Draw(t, fmt.Sprintf("todoStatus%d", i)),
					Priority: rapid.SampledFrom([]string{
						"high", "medium", "low",
					}).Draw(t, fmt.Sprintf("todoPriority%d", i)),
					ID: fmt.Sprintf("%d", i+1),
				}
			}
			jsonBytes, err := json.Marshal(items)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			result, err := views.RenderTodoList(tmpl, string(jsonBytes))
			if err != nil {
				t.Fatalf("RenderTodoList failed for valid JSON: %v\nInput: %s", err, string(jsonBytes))
			}
			resultStr := string(result)
			if resultStr == "" {
				t.Fatalf("RenderTodoList returned empty for valid JSON")
			}
			// Valid JSON should produce the todo-list container (or empty list container)
			if numItems > 0 {
				for _, item := range items {
					if !strings.Contains(resultStr, item.Content) {
						t.Fatalf("Todo content %q not found in rendered output", item.Content)
					}
				}
			}
		} else {
			// Generate invalid JSON
			invalidJSON := genPlainText().Draw(t, "invalidJSON")
			// Make sure it's actually invalid JSON (not accidentally a valid JSON string)
			invalidJSON = "{{" + invalidJSON + "}}"

			result, err := views.RenderTodoList(tmpl, invalidJSON)
			if err != nil {
				t.Fatalf("RenderTodoList should not error for invalid JSON, got: %v", err)
			}
			resultStr := string(result)
			if !strings.Contains(resultStr, "<pre") {
				t.Fatalf("Invalid JSON should fall back to <pre> rendering, got: %s", resultStr)
			}
		}
	})
}

// TestPropFileDropdownSelectionPreservation verifies that the selected file
// is always correctly reflected in the HTML output for any file list.
func TestPropFileDropdownSelectionPreservation(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		numFiles := rapid.IntRange(1, 20).Draw(t, "numFiles")
		files := make([]models.FileNode, numFiles)
		for i := 0; i < numFiles; i++ {
			name := fmt.Sprintf("file%d.go", i)
			files[i] = models.FileNode{
				Path: name,
				Name: name,
			}
		}

		// Pick which file to select (or none)
		selectFile := rapid.Bool().Draw(t, "selectFile")
		var currentPath string
		if selectFile {
			idx := rapid.IntRange(0, numFiles-1).Draw(t, "selectedIdx")
			currentPath = files[idx].Path
		}

		data := struct {
			Files       []models.FileNode
			CurrentPath string
		}{Files: files, CurrentPath: currentPath}

		var buf strings.Builder
		if err := tmpl.ExecuteTemplate(&buf, "file-dropdown", data); err != nil {
			t.Fatalf("Failed to render template: %v", err)
		}
		rendered := buf.String()

		// All file names should appear in the rendered output
		for _, f := range files {
			if !strings.Contains(rendered, f.Path) {
				t.Fatalf("File %q not found in rendered output", f.Path)
			}
		}

		// If a file is selected, the rendered output should contain "selected"
		if selectFile {
			if !strings.Contains(rendered, "selected") {
				t.Fatalf("Expected 'selected' attribute for file %q", currentPath)
			}
		}
	})
}

// TestPropAuthMiddleware verifies that invalid/missing tokens always redirect,
// while valid tokens always pass through.
func TestPropAuthMiddleware(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		server := &Server{
			authSessions: make(map[string]*auth.AuthSession),
		}

		// Generate some valid tokens
		numTokens := rapid.IntRange(1, 5).Draw(t, "numTokens")
		validTokens := make([]string, numTokens)
		for i := 0; i < numTokens; i++ {
			token := fmt.Sprintf("token_%d_%d", i, rapid.IntRange(1000, 9999).Draw(t, fmt.Sprintf("tokenVal%d", i)))
			validTokens[i] = token
			server.authSessions[token] = &auth.AuthSession{
				Email: fmt.Sprintf("user%d@example.com", i),
			}
		}

		scenario := rapid.IntRange(0, 2).Draw(t, "authScenario")

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		req := httptest.NewRequest("GET", "/protected", nil)
		recorder := httptest.NewRecorder()

		switch scenario {
		case 0:
			// No cookie at all - should redirect
			middleware.ChainMiddleware(next, server.withAuth, server.requireAuth).ServeHTTP(recorder, req)
			if nextCalled {
				t.Fatalf("handler should not be called without auth cookie")
			}
			if recorder.Code != http.StatusSeeOther {
				t.Fatalf("expected redirect %d, got %d", http.StatusSeeOther, recorder.Code)
			}

		case 1:
			// Invalid cookie - should redirect
			invalidToken := "invalid_token_" + fmt.Sprintf("%d", rapid.IntRange(10000, 99999).Draw(t, "invalidToken"))
			req.AddCookie(&http.Cookie{Name: "auth_session", Value: invalidToken})
			middleware.ChainMiddleware(next, server.withAuth, server.requireAuth).ServeHTTP(recorder, req)
			if nextCalled {
				t.Fatalf("handler should not be called with invalid auth cookie")
			}
			if recorder.Code != http.StatusSeeOther {
				t.Fatalf("expected redirect %d, got %d", http.StatusSeeOther, recorder.Code)
			}

		case 2:
			// Valid cookie - should pass through
			idx := rapid.IntRange(0, numTokens-1).Draw(t, "validTokenIdx")
			req.AddCookie(&http.Cookie{Name: "auth_session", Value: validTokens[idx]})
			middleware.ChainMiddleware(next, server.withAuth, server.requireAuth).ServeHTTP(recorder, req)
			if !nextCalled {
				t.Fatalf("handler should be called with valid auth cookie %q", validTokens[idx])
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
			}
		}
	})
}

// TestPropWaitForOpencodeReady verifies that the function always succeeds if
// the server becomes ready before timeout, for any number of initial failures.
func TestPropWaitForOpencodeReady(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		failCount := rapid.IntRange(0, 5).Draw(t, "failCount")
		var requestCount int32

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			if int(count) <= failCount {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("not ready"))
			} else {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"sessionId": "test"}`))
			}
		}))
		defer ts.Close()

		var port int
		fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)

		// Give enough timeout: failCount * 100ms polling + margin
		timeout := time.Duration(failCount+3) * 200 * time.Millisecond
		err := sandbox.WaitForOpencodeReady(port, timeout)
		if err != nil {
			t.Fatalf("expected success with %d failures and %v timeout, got error: %v",
				failCount, timeout, err)
		}

		finalCount := atomic.LoadInt32(&requestCount)
		if int(finalCount) < failCount+1 {
			t.Fatalf("expected at least %d requests, got %d", failCount+1, finalCount)
		}
	})
}
