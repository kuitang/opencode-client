package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
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

// ===================== Question Tool Generators =====================

// genQuestionOption generates a random QuestionOption.
func genQuestionOption() *rapid.Generator[models.QuestionOption] {
	return rapid.Custom(func(t *rapid.T) models.QuestionOption {
		return models.QuestionOption{
			Label:       genAlphanumText().Draw(t, "optLabel"),
			Description: genPlainText().Draw(t, "optDesc"),
		}
	})
}

// genQuestionInfo generates a random QuestionInfo with 2-4 options.
func genQuestionInfo() *rapid.Generator[models.QuestionInfo] {
	return rapid.Custom(func(t *rapid.T) models.QuestionInfo {
		numOpts := rapid.IntRange(2, 4).Draw(t, "numOpts")
		opts := make([]models.QuestionOption, numOpts)
		for i := 0; i < numOpts; i++ {
			opts[i] = genQuestionOption().Draw(t, fmt.Sprintf("opt%d", i))
		}
		return models.QuestionInfo{
			Header:   genAlphanumText().Draw(t, "header"),
			Question: genPlainText().Draw(t, "question"),
			Options:  opts,
			Multiple: rapid.Bool().Draw(t, "multiple"),
		}
	})
}

// genQuestionRequest generates a random QuestionRequest with 1-4 questions.
func genQuestionRequest() *rapid.Generator[struct {
	RequestID string
	Questions []models.QuestionInfo
}] {
	return rapid.Custom(func(t *rapid.T) struct {
		RequestID string
		Questions []models.QuestionInfo
	} {
		numQ := rapid.IntRange(1, 4).Draw(t, "numQuestions")
		questions := make([]models.QuestionInfo, numQ)
		for i := 0; i < numQ; i++ {
			questions[i] = genQuestionInfo().Draw(t, fmt.Sprintf("q%d", i))
		}
		return struct {
			RequestID string
			Questions []models.QuestionInfo
		}{
			RequestID: fmt.Sprintf("question_%d", rapid.IntRange(1, 999999).Draw(t, "reqID")),
			Questions: questions,
		}
	})
}

// mockSandbox implements sandbox.Sandbox for unit tests.
type mockSandbox struct {
	baseURL string
}

func (m *mockSandbox) Start(apiKeys map[string]models.AuthConfig) error { return nil }
func (m *mockSandbox) OpencodeURL() string                              { return m.baseURL }
func (m *mockSandbox) GottyURL() string                                 { return "" }
func (m *mockSandbox) DownloadZip() (io.ReadCloser, error)              { return nil, nil }
func (m *mockSandbox) Stop() error                                      { return nil }
func (m *mockSandbox) IsRunning() bool                                  { return true }
func (m *mockSandbox) ContainerIP() string                              { return "" }

// ===================== Question Tool Property Tests =====================

// TestPropQuestionTemplateStructure verifies that for any valid question request,
// the rendered template always contains: data-question-id, all option labels,
// correct input types (radio vs checkbox), reply/reject URLs, and hidden count.
func TestPropQuestionTemplateStructure(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		formData := genQuestionRequest().Draw(t, "formData")

		var buf bytes.Buffer
		err := tmpl.ExecuteTemplate(&buf, "question", formData)
		if err != nil {
			t.Fatalf("ExecuteTemplate failed: %v", err)
		}
		html := buf.String()

		// Property 1: data-question-id matches the request ID
		if !strings.Contains(html, fmt.Sprintf(`data-question-id="%s"`, formData.RequestID)) {
			t.Fatalf("missing data-question-id for %q", formData.RequestID)
		}

		// Property 2: reply and reject URLs contain the request ID
		if !strings.Contains(html, "/question/"+formData.RequestID+"/reply") {
			t.Fatalf("missing reply URL for %q", formData.RequestID)
		}
		if !strings.Contains(html, "/question/"+formData.RequestID+"/reject") {
			t.Fatalf("missing reject URL for %q", formData.RequestID)
		}

		// Property 3: every option label appears as a value attribute
		for _, q := range formData.Questions {
			for _, opt := range q.Options {
				escaped := template.HTMLEscapeString(opt.Label)
				if !strings.Contains(html, escaped) {
					t.Fatalf("option label %q not found in rendered HTML", opt.Label)
				}
			}
		}

		// Property 4: multi-select questions use checkboxes, single-select use radio
		for _, q := range formData.Questions {
			if q.Multiple {
				if !strings.Contains(html, `type="checkbox"`) {
					t.Fatalf("multi-select question should have checkboxes")
				}
			} else {
				if !strings.Contains(html, `type="radio"`) {
					t.Fatalf("single-select question should have radio buttons")
				}
			}
		}

		// Property 5: hidden question_count field matches
		expectedCount := fmt.Sprintf(`value="%d"`, len(formData.Questions))
		if !strings.Contains(html, expectedCount) {
			t.Fatalf("question_count hidden field should have value %d", len(formData.Questions))
		}

		// Property 6: "Other" custom option always present
		if !strings.Contains(html, "Other") {
			t.Fatalf("missing 'Other' custom answer option")
		}
	})
}

// TestPropQuestionReplyRoundTrip verifies that for any set of selected options,
// the handler correctly proxies them to OpenCode in the right format and the
// response HTML contains all selected labels.
func TestPropQuestionReplyRoundTrip(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		formData := genQuestionRequest().Draw(t, "formData")

		// For each question, randomly select 1+ options
		var formValues url.Values = make(url.Values)
		formValues.Set("question_count", fmt.Sprintf("%d", len(formData.Questions)))

		var expectedAnswers [][]string
		for qi, q := range formData.Questions {
			key := fmt.Sprintf("q%d", qi)
			var selected []string

			if q.Multiple {
				// Select a random subset of options (at least 1)
				for oi, opt := range q.Options {
					include := rapid.Bool().Draw(t, fmt.Sprintf("incl%d_%d", qi, oi))
					if include {
						formValues.Add(key, opt.Label)
						selected = append(selected, opt.Label)
					}
				}
				// Ensure at least one is selected
				if len(selected) == 0 {
					formValues.Add(key, q.Options[0].Label)
					selected = append(selected, q.Options[0].Label)
				}
			} else {
				// Select exactly 1 option
				idx := rapid.IntRange(0, len(q.Options)-1).Draw(t, fmt.Sprintf("selIdx%d", qi))
				formValues.Set(key, q.Options[idx].Label)
				selected = []string{q.Options[idx].Label}
			}
			expectedAnswers = append(expectedAnswers, selected)
		}

		// Capture the JSON payload sent to OpenCode
		var receivedPayload map[string]any
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&receivedPayload)
			w.WriteHeader(http.StatusOK)
		}))
		defer mockServer.Close()

		s := &Server{
			Sandbox:   &mockSandbox{baseURL: mockServer.URL},
			templates: tmpl,
		}

		body := strings.NewReader(formValues.Encode())
		req := httptest.NewRequest("POST", "/question/"+formData.RequestID+"/reply", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("requestID", formData.RequestID)

		rr := httptest.NewRecorder()
		s.handleQuestionReply(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}

		// Property 1: response HTML contains all selected labels
		responseHTML := rr.Body.String()
		for _, answers := range expectedAnswers {
			for _, label := range answers {
				if !strings.Contains(responseHTML, template.HTMLEscapeString(label)) {
					t.Fatalf("response should contain selected label %q", label)
				}
			}
		}

		// Property 2: payload sent to OpenCode has correct structure
		rawAnswers, ok := receivedPayload["answers"].([]any)
		if !ok {
			t.Fatalf("payload missing 'answers' array")
		}
		if len(rawAnswers) != len(expectedAnswers) {
			t.Fatalf("expected %d question answers, got %d", len(expectedAnswers), len(rawAnswers))
		}
	})
}

// TestPropQuestionReplyCustomText verifies that __custom__ sentinel is always
// replaced with custom text and never appears in the proxied payload.
func TestPropQuestionReplyCustomText(t *testing.T) {
	tmpl, _ := views.LoadTemplates()
	rapid.Check(t, func(t *rapid.T) {
		customText := genPlainText().Draw(t, "customText")

		var receivedPayload map[string]any
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&receivedPayload)
			w.WriteHeader(http.StatusOK)
		}))
		defer mockServer.Close()

		s := &Server{
			Sandbox:   &mockSandbox{baseURL: mockServer.URL},
			templates: tmpl,
		}

		formValues := url.Values{
			"question_count": {"1"},
			"q0":             {"__custom__"},
			"q0_custom":      {customText},
		}

		body := strings.NewReader(formValues.Encode())
		req := httptest.NewRequest("POST", "/question/q_test/reply", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("requestID", "q_test")

		rr := httptest.NewRecorder()
		s.handleQuestionReply(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}

		// Property: __custom__ sentinel never appears in proxied payload
		payloadJSON, _ := json.Marshal(receivedPayload)
		if strings.Contains(string(payloadJSON), "__custom__") {
			t.Fatalf("__custom__ sentinel leaked into payload: %s", payloadJSON)
		}

		// Property: custom text appears in payload (trimmed)
		trimmed := strings.TrimSpace(customText)
		if trimmed != "" && !strings.Contains(string(payloadJSON), trimmed) {
			t.Fatalf("custom text %q not found in payload: %s", trimmed, payloadJSON)
		}
	})
}

// TestPropQuestionRejectAlwaysProxies verifies that reject always calls OpenCode
// and returns the dismissed template for any request ID.
func TestPropQuestionRejectAlwaysProxies(t *testing.T) {
	tmpl, _ := views.LoadTemplates()
	rapid.Check(t, func(t *rapid.T) {
		requestID := fmt.Sprintf("question_%d", rapid.IntRange(1, 999999).Draw(t, "reqID"))
		var calledPath string

		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calledPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		defer mockServer.Close()

		s := &Server{
			Sandbox:   &mockSandbox{baseURL: mockServer.URL},
			templates: tmpl,
		}

		req := httptest.NewRequest("POST", "/question/"+requestID+"/reject", nil)
		req.SetPathValue("requestID", requestID)

		rr := httptest.NewRecorder()
		s.handleQuestionReject(rr, req)

		// Property 1: always returns 200
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}

		// Property 2: proxied path includes the request ID
		if !strings.Contains(calledPath, requestID) {
			t.Fatalf("proxy path %q should contain request ID %q", calledPath, requestID)
		}

		// Property 3: response contains "dismissed"
		if !strings.Contains(rr.Body.String(), "dismissed") {
			t.Fatalf("response should contain 'dismissed'")
		}
	})
}

// ===================== Transform & Rendering Property Tests =====================

// TestPropTransformMessagePartTypes verifies that TransformMessagePart preserves
// type and partID, and produces type-specific content for any valid part.
func TestPropTransformMessagePartTypes(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		partType := rapid.SampledFrom([]string{"text", "tool", "reasoning", "step-start", "step-finish"}).Draw(t, "partType")
		partID := genPartID().Draw(t, "partID")
		content := genPlainText().Draw(t, "content")

		part := models.MessagePart{
			ID:   partID,
			Type: partType,
		}

		switch partType {
		case "text":
			part.Text = content
		case "reasoning":
			part.Text = content
		case "tool":
			toolName := rapid.SampledFrom([]string{"bash", "read", "edit", "write", "glob", "grep", "webfetch", "todowrite"}).Draw(t, "toolName")
			status := rapid.SampledFrom([]string{"running", "completed", "error"}).Draw(t, "toolStatus")
			part.Tool = toolName
			part.State = map[string]any{
				"status": status,
				"input":  map[string]any{"command": content},
				"output": content,
			}
		}

		result := views.TransformMessagePart(tmpl, part)

		// Property 1: type is preserved
		if result.Type != partType {
			t.Fatalf("expected type %q, got %q", partType, result.Type)
		}

		// Property 2: partID is preserved
		if result.PartID != partID {
			t.Fatalf("expected partID %q, got %q", partID, result.PartID)
		}

		// Property 3: type-specific content properties
		switch partType {
		case "text":
			if result.Content != part.Text {
				t.Fatalf("text content not preserved: expected %q, got %q", part.Text, result.Content)
			}
			if strings.TrimSpace(part.Text) != "" && string(result.RenderedHTML) == "" {
				t.Fatalf("non-empty text should produce non-empty RenderedHTML")
			}
		case "reasoning":
			if !strings.Contains(result.Content, "🤔") {
				t.Fatalf("reasoning should contain thinking emoji")
			}
			if !strings.Contains(result.Content, part.Text) {
				t.Fatalf("reasoning should contain original text")
			}
		case "tool":
			if !strings.Contains(result.Content, part.Tool) {
				t.Fatalf("tool content should contain tool name %q, got %q", part.Tool, result.Content)
			}
		case "step-start":
			if !strings.Contains(result.Content, "▶️") {
				t.Fatalf("step-start should contain play emoji")
			}
			if !strings.Contains(string(result.RenderedHTML), "bg-yellow-100") {
				t.Fatalf("step-start should have yellow badge styling")
			}
		case "step-finish":
			if !strings.Contains(result.Content, "✅") {
				t.Fatalf("step-finish should contain check emoji")
			}
			if !strings.Contains(string(result.RenderedHTML), "bg-green-100") {
				t.Fatalf("step-finish should have green badge styling")
			}
		}
	})
}

// TestPropMultilineRendering verifies that for any set of lines joined by
// any separator (\n or \r\n), all lines survive in the rendered message HTML.
func TestPropMultilineRendering(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		numLines := rapid.IntRange(2, 10).Draw(t, "numLines")
		lines := make([]string, numLines)
		for i := 0; i < numLines; i++ {
			lines[i] = genAlphanumText().Draw(t, fmt.Sprintf("line%d", i))
		}
		sep := rapid.SampledFrom([]string{"\n", "\r\n"}).Draw(t, "separator")
		input := strings.Join(lines, sep)

		msgData := views.MessageData{
			Alignment: "left",
			Parts: []views.MessagePartData{{
				Type:         "text",
				Content:      input,
				RenderedHTML: views.RenderText(input),
			}},
		}
		html, err := views.RenderMessage(tmpl, msgData)
		if err != nil {
			t.Fatalf("RenderMessage failed: %v", err)
		}

		for i, line := range lines {
			if !strings.Contains(html, line) {
				t.Fatalf("line %d %q not found in rendered HTML", i, line)
			}
		}
	})
}

// ===================== Template & UI Property Tests =====================

// TestPropTemplateCountConsistency verifies that sub-template rendering
// matches composite template rendering for any file/line count values.
func TestPropTemplateCountConsistency(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		fileCount := rapid.IntRange(0, 10000).Draw(t, "fileCount")
		lineCount := rapid.IntRange(0, 100000).Draw(t, "lineCount")
		numFiles := rapid.IntRange(0, 10).Draw(t, "numFiles")

		files := make([]models.FileNode, numFiles)
		for i := 0; i < numFiles; i++ {
			name := fmt.Sprintf("file%d.go", i)
			files[i] = models.FileNode{Path: name, Name: name}
		}

		var currentPath string
		if numFiles > 0 {
			currentPath = files[0].Path
		}

		data := struct {
			FileCount, LineCount int
			Files                []models.FileNode
			CurrentPath          string
		}{FileCount: fileCount, LineCount: lineCount, Files: files, CurrentPath: currentPath}

		// Render file-count-content directly
		var directBuf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&directBuf, "file-count-content", data); err != nil {
			t.Fatalf("Failed to render file-count-content: %v", err)
		}
		directHTML := strings.TrimSpace(directBuf.String())

		// Render via composite template and extract
		var compositeBuf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&compositeBuf, "file-options-with-counts", data); err != nil {
			t.Fatalf("Failed to render file-options-with-counts: %v", err)
		}
		compositeHTML := compositeBuf.String()

		// Property: sub-template output must appear in composite output
		startMarker := `<div id="file-count-container" hx-swap-oob="innerHTML">`
		startIdx := strings.Index(compositeHTML, startMarker)
		if startIdx == -1 {
			t.Fatalf("file-count-container not found in composite output")
		}
		startIdx += len(startMarker)
		endIdx := strings.Index(compositeHTML[startIdx:], `</div>`)
		if endIdx == -1 {
			t.Fatalf("closing div not found")
		}
		extracted := strings.TrimSpace(compositeHTML[startIdx : startIdx+endIdx])

		if directHTML != extracted {
			t.Fatalf("file count mismatch:\ndirect: %q\nextracted: %q", directHTML, extracted)
		}

		// Property: file count value appears in output
		expectedCount := fmt.Sprintf("%d", fileCount)
		if !strings.Contains(directHTML, expectedCount) {
			t.Fatalf("file count %d not found in output", fileCount)
		}
	})
}

// TestPropTemplateHTMLStructure verifies structural invariants of count templates
// hold for any file/line count values.
func TestPropTemplateHTMLStructure(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		fileCount := rapid.IntRange(0, 99999).Draw(t, "fileCount")
		lineCount := rapid.IntRange(0, 999999).Draw(t, "lineCount")

		data := struct{ FileCount, LineCount int }{fileCount, lineCount}

		// Property: file-count-content has correct styling and value
		var fcBuf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&fcBuf, "file-count-content", data); err != nil {
			t.Fatalf("file-count-content: %v", err)
		}
		fcHTML := fcBuf.String()
		fcDoc, err := goquery.NewDocumentFromReader(strings.NewReader(fcHTML))
		if err != nil {
			t.Fatalf("parse file-count HTML: %v", err)
		}
		if fcDoc.Find(`.text-2xl`).Length() == 0 {
			t.Fatalf("file count missing text-2xl styling")
		}
		if fcDoc.Find(`.text-sm`).Length() == 0 {
			t.Fatalf("file count missing text-sm styling")
		}
		if !strings.Contains(fcHTML, fmt.Sprintf("%d", fileCount)) {
			t.Fatalf("file count %d not in output", fileCount)
		}

		// Property: line-count-content has same structure and correct value
		var lcBuf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&lcBuf, "line-count-content", data); err != nil {
			t.Fatalf("line-count-content: %v", err)
		}
		lcHTML := lcBuf.String()
		if !strings.Contains(lcHTML, fmt.Sprintf("%d", lineCount)) {
			t.Fatalf("line count %d not in output", lineCount)
		}
		if !strings.Contains(lcHTML, "Lines of Code") {
			t.Fatalf("missing 'Lines of Code' label")
		}
	})
}

// TestPropMessageTemplateAttributes verifies that message IDs, alignment classes,
// streaming flags, and OOB swap attributes are correctly rendered for any combination.
func TestPropMessageTemplateAttributes(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		id := genMessageID().Draw(t, "msgID")
		alignment := rapid.SampledFrom([]string{"left", "right"}).Draw(t, "alignment")
		isStreaming := rapid.Bool().Draw(t, "streaming")
		hxSwapOOB := rapid.Bool().Draw(t, "oob")
		text := genPlainText().Draw(t, "text")

		data := views.MessageData{
			ID:         id,
			Alignment:  alignment,
			Text:       text,
			IsStreaming: isStreaming,
			HXSwapOOB:  hxSwapOOB,
		}

		html, err := views.RenderMessage(tmpl, data)
		if err != nil {
			t.Fatalf("RenderMessage: %v", err)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		msg := doc.Find("div.flex").First()

		// Property: ID is always present
		gotID, exists := msg.Attr("id")
		if !exists {
			t.Fatalf("expected id attribute")
		}
		if gotID != id {
			t.Fatalf("expected id %q, got %q", id, gotID)
		}

		// Property: alignment maps to justify class
		class, _ := msg.Attr("class")
		if alignment == "right" && !strings.Contains(class, "justify-end") {
			t.Fatalf("right alignment should have justify-end")
		}
		if alignment == "left" && !strings.Contains(class, "justify-start") {
			t.Fatalf("left alignment should have justify-start")
		}

		// Property: streaming flag maps to streaming class
		if isStreaming && !strings.Contains(class, "streaming") {
			t.Fatalf("streaming=true should add streaming class")
		}
		if !isStreaming && strings.Contains(class, "streaming") {
			t.Fatalf("streaming=false should not have streaming class")
		}

		// Property: OOB flag maps to hx-swap-oob attribute
		if hxSwapOOB {
			oob, hasOOB := msg.Attr("hx-swap-oob")
			if !hasOOB || oob != "true" {
				t.Fatalf("expected hx-swap-oob=true when HXSwapOOB=true")
			}
		}
	})
}

// TestPropMessageMetadata verifies that provider/model metadata appears
// if and only if both fields are set.
func TestPropMessageMetadata(t *testing.T) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	rapid.Check(t, func(t *rapid.T) {
		provider := rapid.SampledFrom([]string{"", "openai", "opencode", "anthropic"}).Draw(t, "provider")
		model := rapid.SampledFrom([]string{"", "gpt-4o", "minimax-m2.5-free", "claude-3"}).Draw(t, "model")
		alignment := rapid.SampledFrom([]string{"left", "right"}).Draw(t, "alignment")

		data := views.MessageData{
			Alignment: alignment,
			Text:      "test",
			Provider:  provider,
			Model:     model,
		}

		html, err := views.RenderMessage(tmpl, data)
		if err != nil {
			t.Fatalf("RenderMessage: %v", err)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		meta := doc.Find("div.text-xs.text-gray-600")
		shouldHaveMeta := provider != "" && model != ""

		if shouldHaveMeta {
			if meta.Length() == 0 {
				t.Fatalf("expected metadata for provider=%q model=%q", provider, model)
			}
			expectedText := provider + "/" + model
			if !strings.Contains(meta.Text(), expectedText) {
				t.Fatalf("expected metadata %q, got %q", expectedText, meta.Text())
			}
		} else {
			if meta.Length() > 0 {
				t.Fatalf("unexpected metadata for provider=%q model=%q", provider, model)
			}
		}
	})
}

// ===================== Rate Limiter & Logging Property Tests =====================

// TestPropRateLimiterCoalescence verifies that for any number of rapid updates,
// the first executes immediately and subsequent ones coalesce into a single execution.
func TestPropRateLimiterCoalescence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numUpdates := rapid.IntRange(2, 20).Draw(t, "numUpdates")

		var counter int32
		var lastValue int32
		limiter := NewUpdateRateLimiter(500 * time.Millisecond)

		// First update should be immediate
		limiter.TryUpdate(context.Background(), func() {
			atomic.AddInt32(&counter, 1)
			atomic.StoreInt32(&lastValue, 1)
		})
		time.Sleep(100 * time.Millisecond)

		if atomic.LoadInt32(&counter) != 1 {
			t.Fatalf("first update should be immediate, got %d executions", atomic.LoadInt32(&counter))
		}

		// Rapid subsequent updates should coalesce
		for i := 2; i <= numUpdates; i++ {
			val := int32(i)
			limiter.TryUpdate(context.Background(), func() {
				atomic.AddInt32(&counter, 1)
				atomic.StoreInt32(&lastValue, val)
			})
		}

		// Wait for coalesced update to fire
		time.Sleep(600 * time.Millisecond)

		finalCount := atomic.LoadInt32(&counter)
		if finalCount != 2 {
			t.Fatalf("expected 2 executions (1 immediate + 1 coalesced), got %d", finalCount)
		}

		finalValue := atomic.LoadInt32(&lastValue)
		if finalValue != int32(numUpdates) {
			t.Fatalf("coalesced update should use latest value %d, got %d", numUpdates, finalValue)
		}
	})
}

// TestPropLoggingResponseWriter verifies that for any status code and body,
// LoggingResponseWriter captures both correctly and logs them without truncation.
func TestPropLoggingResponseWriter(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		statusCode := rapid.IntRange(100, 599).Draw(t, "statusCode")
		body := genPlainText().Draw(t, "body")
		method := rapid.SampledFrom([]string{"GET", "POST", "PUT", "DELETE"}).Draw(t, "method")
		path := "/" + genAlphanumText().Draw(t, "path")

		var logOutput bytes.Buffer
		log.SetOutput(&logOutput)
		defer log.SetOutput(os.Stderr)

		recorder := httptest.NewRecorder()
		lw := middleware.NewLoggingResponseWriter(recorder)

		// Property: default status is 200 and body buffer is initialized
		if lw.StatusCode != 200 {
			t.Fatalf("default status should be 200, got %d", lw.StatusCode)
		}
		if lw.Body == nil || lw.Body.Len() != 0 {
			t.Fatalf("body buffer should be initialized and empty")
		}

		lw.WriteHeader(statusCode)
		n, err := lw.Write([]byte(body))

		// Property: write succeeds without error
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}
		// Property: all bytes are written
		if n != len(body) {
			t.Fatalf("wrote %d bytes, expected %d", n, len(body))
		}
		// Property: status code is captured
		if lw.StatusCode != statusCode {
			t.Fatalf("expected status %d, got %d", statusCode, lw.StatusCode)
		}
		// Property: body is captured in both places
		if lw.Body.String() != body {
			t.Fatalf("lw.Body mismatch")
		}
		if recorder.Body.String() != body {
			t.Fatalf("recorder.Body mismatch")
		}

		// Property: LogResponse includes status and body in log (no truncation)
		lw.LogResponse(method, path)
		logStr := logOutput.String()
		expectedPrefix := fmt.Sprintf("WIRE_OUT %s %s [%d]", method, path, statusCode)
		if !strings.Contains(logStr, expectedPrefix) {
			t.Fatalf("log should contain %q, got %q", expectedPrefix, logStr)
		}
		if !strings.Contains(logStr, body) {
			t.Fatalf("log should contain response body without truncation")
		}
	})
}

// TestPropLoggingMiddleware verifies that the /events path gets SSE-specific
// connection start/end logging, while all other paths get WIRE_OUT logging.
func TestPropLoggingMiddleware(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		method := rapid.SampledFrom([]string{"GET", "POST", "PUT", "DELETE", "PATCH"}).Draw(t, "method")
		body := genPlainText().Draw(t, "body")
		statusCode := rapid.IntRange(200, 599).Draw(t, "status")
		isSSE := rapid.Bool().Draw(t, "isSSE")

		// SSE detection is path-based: r.URL.Path == "/events"
		var path string
		if isSSE {
			path = "/events"
		} else {
			path = "/" + genAlphanumText().Draw(t, "path")
		}

		var logOutput bytes.Buffer
		log.SetOutput(&logOutput)
		defer log.SetOutput(os.Stderr)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(statusCode)
			w.Write([]byte(body))
		})

		req := httptest.NewRequest(method, path, nil)
		recorder := httptest.NewRecorder()
		middleware.LoggingMiddleware(handler).ServeHTTP(recorder, req)

		logStr := logOutput.String()

		if isSSE {
			// Property: /events path gets SSE connection start/end logging
			if !strings.Contains(logStr, "WIRE_OUT SSE connection started") {
				t.Fatalf("SSE should log connection started, got: %s", logStr)
			}
			if !strings.Contains(logStr, "WIRE_OUT SSE connection ended") {
				t.Fatalf("SSE should log connection ended, got: %s", logStr)
			}
			// Property: SSE endpoint should NOT use normal response logging
			if strings.Contains(logStr, fmt.Sprintf("WIRE_OUT %s /events [", method)) {
				t.Fatalf("SSE endpoint should not use normal response logging")
			}
		} else {
			// Property: non-events paths get WIRE_OUT with method, path, status
			expectedLog := fmt.Sprintf("WIRE_OUT %s %s [%d]", method, path, statusCode)
			if !strings.Contains(logStr, expectedLog) {
				t.Fatalf("expected log containing %q, got %q", expectedLog, logStr)
			}
		}
	})
}
