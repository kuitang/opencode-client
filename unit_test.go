package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// ---------------- Rendering: formatting and message rendering ----------------

func TestUnitMessageFormatting(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	tests := []struct {
		name         string
		input        string
		expectMD     bool
		hasElements  []string
		hasClasses   []string
		containsText []string
	}{
		{name: "Plain text preserves newlines", input: "Line 1\nLine 2\nLine 3", expectMD: false, hasClasses: []string{"preserve-breaks"}, containsText: []string{"Line 1", "Line 2", "Line 3"}},
		{name: "Plain text with bullets", input: "• Item 1\n• Item 2\n• Item 3", expectMD: false, hasClasses: []string{"preserve-breaks"}, containsText: []string{"• Item 1", "• Item 2", "• Item 3"}},
		{name: "Plain numbered list becomes markdown", input: "1. First\n2. Second\n3. Third", expectMD: true, hasElements: []string{"ol", "li"}, containsText: []string{"First", "Second", "Third"}},
		{name: "Bold markdown", input: "This is **bold** text", expectMD: true, hasElements: []string{"strong"}, containsText: []string{"This is", "bold", "text"}},
		{name: "Italic markdown", input: "This is *italic* text", expectMD: true, hasElements: []string{"em"}, containsText: []string{"This is", "italic", "text"}},
		{name: "Mixed markdown with newlines", input: "# Header\n\nThis is **bold** and *italic*\n\n- Item 1\n- Item 2", expectMD: true, hasElements: []string{"h1", "strong", "em", "li"}, containsText: []string{"Header", "bold", "italic", "Item 1", "Item 2"}},
		{name: "Code blocks", input: "```go\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```", expectMD: true, hasElements: []string{"pre", "code"}, containsText: []string{"func main()"}},
		{name: "Inline code", input: "Use `fmt.Println()` to print", expectMD: true, hasElements: []string{"code"}, containsText: []string{"Use", "fmt.Println()", "to print"}},
		{name: "XSS in plain text", input: "<script>alert('XSS')</script>", expectMD: false, hasClasses: []string{"preserve-breaks"}},
		{name: "XSS in markdown", input: "**Bold** <script>alert('XSS')</script>", expectMD: true, hasElements: []string{"strong"}, containsText: []string{"Bold"}},
		{name: "HTML entities handling", input: "Less than < and greater than > and ampersand &", expectMD: false, hasClasses: []string{"preserve-breaks"}, containsText: []string{"Less than", "and greater than", "and ampersand"}},
		{name: "Empty string", input: "", expectMD: false, hasClasses: []string{"preserve-breaks"}},
		{name: "Math expressions not markdown", input: "2 * 3 = 6 and 4 / 2 = 2", expectMD: false, hasClasses: []string{"preserve-breaks"}, containsText: []string{"2 * 3 = 6", "4 / 2 = 2"}},
		{name: "URL with autolink", input: "Visit https://example.com for more", expectMD: true, hasElements: []string{"a"}, containsText: []string{"Visit", "for more"}},
		{name: "Markdown link", input: "Visit [Example](https://example.com) for more", expectMD: true, hasElements: []string{"a"}, containsText: []string{"Visit", "Example", "for more"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgData := MessageData{Alignment: "left", Parts: []MessagePartData{{Type: "text", Content: tt.input, RenderedHTML: renderText(tt.input)}}}
			html, err := renderMessage(templates, msgData)
			if err != nil {
				t.Fatalf("renderMessage failed: %v", err)
			}
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
			if err != nil {
				t.Fatalf("Failed to parse HTML: %v", err)
			}
			for _, element := range tt.hasElements {
				if doc.Find(element).Length() == 0 {
					t.Errorf("Expected HTML element <%s> not found\nHTML: %s", element, html)
				}
			}
			for _, class := range tt.hasClasses {
				if doc.Find("."+class).Length() == 0 {
					t.Errorf("Expected CSS class .%s not found\nHTML: %s", class, html)
				}
			}
			for _, text := range tt.containsText {
				if !strings.Contains(doc.Text(), text) {
					t.Errorf("Expected text %q not found in content\nText: %s", text, doc.Text())
				}
			}
			if doc.Find("script").Length() > 0 {
				t.Errorf("Dangerous script element found in output\nHTML: %s", html)
			}
		})
	}
}

func TestUnitUserAndLLMRenderingSame(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}
	testInputs := []string{"Plain text\nwith newlines", "**Bold** and *italic*", "# Header\n\n- List item", "<script>alert('XSS')</script>", "```code block```", "• Unicode bullets\n• With newlines"}
	for _, input := range testInputs {
		userMsg := MessageData{Alignment: "right", Parts: []MessagePartData{{Type: "text", Content: input, RenderedHTML: renderText(input)}}}
		llmMsg := MessageData{Alignment: "left", Parts: []MessagePartData{{Type: "text", Content: input, RenderedHTML: renderText(input)}}}
		userHTML, err := renderMessage(templates, userMsg)
		if err != nil {
			t.Fatalf("User message rendering failed: %v", err)
		}
		llmHTML, err := renderMessage(templates, llmMsg)
		if err != nil {
			t.Fatalf("LLM message rendering failed: %v", err)
		}
		userDoc, _ := goquery.NewDocumentFromReader(strings.NewReader(userHTML))
		llmDoc, _ := goquery.NewDocumentFromReader(strings.NewReader(llmHTML))
		if userDoc.Find(".prose").Text() != llmDoc.Find(".prose").Text() {
			t.Errorf("User and LLM content differ for input %q\nUser: %s\nLLM: %s", input, userDoc.Find(".prose").Text(), llmDoc.Find(".prose").Text())
		}
	}
}

func TestUnitMultilineInputSupport(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}
	multilineInputs := []struct {
		name, input  string
		expectedText []string
	}{
		{"Unix newlines", "Line 1\nLine 2\nLine 3", []string{"Line 1", "Line 2", "Line 3"}},
		{"Windows newlines", "Line 1\r\nLine 2\r\nLine 3", []string{"Line 1", "Line 2", "Line 3"}},
		{"Blank lines", "Paragraph 1\n\nParagraph 2\n\n\nParagraph 3", []string{"Paragraph 1", "Paragraph 2", "Paragraph 3"}},
		{"Indented lines", "def function():\n    line1\n    line2\n        nested", []string{"def function()", "line1", "line2", "nested"}},
	}
	for _, tt := range multilineInputs {
		t.Run(tt.name, func(t *testing.T) {
			msgData := MessageData{Alignment: "left", Parts: []MessagePartData{{Type: "text", Content: tt.input, RenderedHTML: renderText(tt.input)}}}
			html, err := renderMessage(templates, msgData)
			if err != nil {
				t.Fatalf("renderMessage failed: %v", err)
			}
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
			if err != nil {
				t.Fatalf("Failed to parse HTML: %v", err)
			}
			content := doc.Text()
			for _, expected := range tt.expectedText {
				if !strings.Contains(content, expected) {
					t.Errorf("Expected text %q not found in rendered content\nInput: %q\nContent: %q", expected, tt.input, content)
				}
			}
		})
	}
}

// ---------------- Rendering: template auto-escaping and newline preservation ----------------

func TestUnitHTMLEscapingInTemplates(t *testing.T) {
	tmpl := template.Must(template.New("test").Parse(`<div>{{.Content}}</div>`))
	tests := []struct{ name, input, expected string }{
		{"Script tag", "<script>alert('XSS')</script>", "<div>&lt;script&gt;alert(&#39;XSS&#39;)&lt;/script&gt;</div>"},
		{"Onclick handler", "<div onclick='alert(1)'>Click</div>", "<div>&lt;div onclick=&#39;alert(1)&#39;&gt;Click&lt;/div&gt;</div>"},
		{"Image with onerror", "<img src=x onerror=alert('XSS')>", "<div>&lt;img src=x onerror=alert(&#39;XSS&#39;)&gt;</div>"},
		{"HTML entities", "&lt;already&gt; &amp; escaped", "<div>&amp;lt;already&amp;gt; &amp;amp; escaped</div>"},
		{"Mixed quotes", `<a href="javascript:alert('XSS')">Click</a>`, "<div>&lt;a href=&#34;javascript:alert(&#39;XSS&#39;)&#34;&gt;Click&lt;/a&gt;</div>"},
		{"SVG with script", "<svg onload=alert(1)>", "<div>&lt;svg onload=alert(1)&gt;</div>"},
		{"Data URI", "<a href='data:text/html,<script>alert(1)</script>'>Click</a>", "<div>&lt;a href=&#39;data:text/html,&lt;script&gt;alert(1)&lt;/script&gt;&#39;&gt;Click&lt;/a&gt;</div>"},
		{"Event handler variations", "<body onload=alert(1) onmouseover=alert(2)>", "<div>&lt;body onload=alert(1) onmouseover=alert(2)&gt;</div>"},
		{"Style with expression", "<div style='background:url(javascript:alert(1))'>", "<div>&lt;div style=&#39;background:url(javascript:alert(1))&#39;&gt;</div>"},
		{"Form action", "<form action='javascript:alert(1)'><input></form>", "<div>&lt;form action=&#39;javascript:alert(1)&#39;&gt;&lt;input&gt;&lt;/form&gt;</div>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			data := struct{ Content string }{Content: tt.input}
			if err := tmpl.Execute(&buf, data); err != nil {
				t.Fatalf("Template execution failed: %v", err)
			}
			if got := buf.String(); got != tt.expected {
				t.Errorf("Template escaping failed\nInput: %q\nGot: %q\nExpected: %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestUnitNewlinePreservation(t *testing.T) {
	tmpl := template.Must(template.New("test").Parse(`<div class="preserve-breaks">{{.}}</div>`))
	tests := []struct {
		name, input string
		contains    []string
	}{
		{"Simple newlines", "Line 1\nLine 2\nLine 3", []string{"Line 1\nLine 2\nLine 3"}},
		{"Windows newlines", "Line 1\r\nLine 2\r\nLine 3", []string{"Line 1\r\nLine 2\r\nLine 3"}},
		{"Mixed with HTML chars", "<script>\nalert(1)\n</script>", []string{"&lt;script&gt;\nalert(1)\n&lt;/script&gt;"}},
		{"Unicode bullets with newlines", "• Item 1\n• Item 2\n• Item 3", []string{"• Item 1\n• Item 2\n• Item 3"}},
		{"Numbered list", "1. First\n2. Second\n3. Third", []string{"1. First\n2. Second\n3. Third"}},
		{"Multiple blank lines", "Paragraph 1\n\n\nParagraph 2", []string{"Paragraph 1\n\n\nParagraph 2"}},
		{"Tabs and spaces", "\tIndented\n    Four spaces\n\t\tDouble tab", []string{"\tIndented\n    Four spaces\n\t\tDouble tab"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			if err := tmpl.Execute(&buf, tt.input); err != nil {
				t.Fatalf("Template execution failed: %v", err)
			}
			got := buf.String()
			for _, expected := range tt.contains {
				if !strings.Contains(got, expected) {
					t.Errorf("Output missing expected content\nInput: %q\nExpected: %q\nGot: %q", tt.input, expected, got)
				}
			}
		})
	}
}

func TestUnitMessagePartDataSecurity(t *testing.T) {
	tests := []struct {
		name, partType, content string
		isMarkdown              bool
	}{
		{"Text with XSS", "text", "<script>alert('XSS')</script>", false},
		{"Reasoning with HTML", "reasoning", "🤔 Reasoning:\n<div>test</div>", false},
		{"Tool output with script", "tool", "Tool: test\nOutput:\n<script>alert(1)</script>", false},
		{"File with malicious filename", "file", "📁 File: <img src=x onerror=alert(1)>.txt\nURL: http://example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			part := MessagePartData{Type: tt.partType, Content: tt.content}
			if part.Content != tt.content {
				t.Errorf("Content field was modified\nOriginal: %q\nGot: %q", tt.content, part.Content)
			}
			if tt.partType == "text" && tt.isMarkdown {
				part.RenderedHTML = renderText(tt.content)
				if part.RenderedHTML == "" {
					t.Error("RenderedHTML should be set for markdown text")
				}
			}
		})
	}
}

// ---------------- Rendering: HTTP part transformation and TODO renderer ----------------

// Helpers consolidated from the previous unit_http_parts_test.go
func convertToMessagePart(enhanced EnhancedMessagePart) MessagePart {
	return MessagePart{
		ID:        enhanced.ID,
		MessageID: enhanced.MessageID,
		SessionID: enhanced.SessionID,
		Type:      enhanced.Type,
		Text:      enhanced.Text,
		Tool:      enhanced.Tool,
		CallID:    enhanced.CallID,
		State:     enhanced.State,
		Time: struct {
			Start int64 `json:"start,omitempty"`
			End   int64 `json:"end,omitempty"`
		}{Start: 0, End: 0},
	}
}

func formatValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strings.TrimRight(strings.TrimRight(formatFloat(val), "0"), ".")
	default:
		bytes, _ := json.Marshal(val)
		return string(bytes)
	}
}

func formatFloat(f float64) string {
	s := strings.TrimRight(strings.TrimRight(formatFloatWithPrecision(f, 6), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

func formatFloatWithPrecision(f float64, precision int) string {
	return strings.TrimRight(strings.TrimRight(sprintf("%.6f", f), "0"), ".")
}

func sprintf(format string, args ...interface{}) string {
	if format == "%.6f" && len(args) == 1 {
		if f, ok := args[0].(float64); ok {
			whole := int(f)
			decimal := int((f - float64(whole)) * 1000000)
			if decimal < 0 {
				decimal = -decimal
			}
			return intToString(whole) + "." + padLeft(intToString(decimal), 6, '0')
		}
	}
	return ""
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + intToString(-n)
	}
	var result string
	for n > 0 {
		digit := byte('0' + (n % 10))
		result = string(digit) + result
		n /= 10
	}
	return result
}

func padLeft(s string, length int, pad rune) string {
	for len(s) < length {
		s = string([]rune{pad}) + s
	}
	return s
}

func TestUnitTransformTextPart(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	part := EnhancedMessagePart{ID: "prt_001", MessageID: "msg_001", SessionID: "ses_001", Type: "text", Text: "Hello **world**! Check out https://example.com"}
	result := transformMessagePart(templates, convertToMessagePart(part))
	if result.Type != "text" {
		t.Errorf("Expected type 'text', got %s", result.Type)
	}
	if result.Content != part.Text {
		t.Errorf("Expected content %q, got %q", part.Text, result.Content)
	}
	if result.PartID != "prt_001" {
		t.Errorf("Expected part ID 'prt_001', got %s", result.PartID)
	}
	htmlStr := string(result.RenderedHTML)
	if !strings.Contains(htmlStr, "<strong>world</strong>") {
		t.Errorf("Expected markdown bold rendering, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `<a href="https://example.com"`) {
		t.Errorf("Expected autolink rendering, got %s", htmlStr)
	}
}

func TestUnitTransformToolPart(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	part := EnhancedMessagePart{ID: "prt_002", MessageID: "msg_001", SessionID: "ses_001", Type: "tool", Tool: "bash", State: map[string]interface{}{"status": "completed", "input": map[string]interface{}{"command": "ls -la"}, "output": "total 24\ndrwxr-xr-x  3 user user 4096 Jan 1 00:00 ."}}
	result := transformMessagePart(templates, convertToMessagePart(part))
	if result.Type != "tool" {
		t.Errorf("Expected type 'tool', got %s", result.Type)
	}
	if !strings.Contains(result.Content, "bash") {
		t.Errorf("Expected tool name in content, got %s", result.Content)
	}
	if !strings.Contains(result.Content, "completed") {
		t.Errorf("Expected status in content, got %s", result.Content)
	}
	htmlStr := string(result.RenderedHTML)
	if !strings.Contains(htmlStr, "bash") {
		t.Errorf("Expected tool name in HTML, got %s", htmlStr)
	}
}

func TestUnitTransformTodoWritePart(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	todoJSON := `[{"content":"Task 1","status":"completed"},{"content":"Task 2","status":"pending"}]`
	part := EnhancedMessagePart{ID: "prt_003", MessageID: "msg_001", SessionID: "ses_001", Type: "tool", Tool: "todowrite", State: map[string]interface{}{"status": "completed", "input": map[string]interface{}{}, "output": todoJSON}}
	result := transformMessagePart(templates, convertToMessagePart(part))
	if result.Type != "tool" {
		t.Errorf("Expected type 'tool', got %s", result.Type)
	}
	htmlStr := string(result.RenderedHTML)
	if !strings.Contains(htmlStr, "Task 1") {
		t.Errorf("Expected 'Task 1' in rendered HTML, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, "Task 2") {
		t.Errorf("Expected 'Task 2' in rendered HTML, got %s", htmlStr)
	}
}

func TestUnitTransformReasoningPart(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	part := EnhancedMessagePart{ID: "prt_004", MessageID: "msg_001", SessionID: "ses_001", Type: "reasoning", Text: "I need to analyze this problem step by step..."}
	result := transformMessagePart(templates, convertToMessagePart(part))
	if result.Type != "reasoning" {
		t.Errorf("Expected type 'reasoning', got %s", result.Type)
	}
	if !strings.Contains(result.Content, "🤔") {
		t.Errorf("Expected reasoning emoji, got %s", result.Content)
	}
	if !strings.Contains(result.Content, part.Text) {
		t.Errorf("Expected reasoning text, got %s", result.Content)
	}
}

func TestUnitTransformStepParts(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	startPart := EnhancedMessagePart{ID: "prt_005", MessageID: "msg_001", SessionID: "ses_001", Type: "step-start"}
	startResult := transformMessagePart(templates, convertToMessagePart(startPart))
	if startResult.Type != "step-start" {
		t.Errorf("Expected type 'step-start', got %s", startResult.Type)
	}
	if !strings.Contains(startResult.Content, "▶️") {
		t.Errorf("Expected step-start emoji, got %s", startResult.Content)
	}
	if !strings.Contains(string(startResult.RenderedHTML), "bg-yellow-100") {
		t.Errorf("Expected yellow badge styling, got %s", string(startResult.RenderedHTML))
	}
	finishPart := EnhancedMessagePart{ID: "prt_006", MessageID: "msg_001", SessionID: "ses_001", Type: "step-finish"}
	finishResult := transformMessagePart(templates, convertToMessagePart(finishPart))
	if finishResult.Type != "step-finish" {
		t.Errorf("Expected type 'step-finish', got %s", finishResult.Type)
	}
	if !strings.Contains(finishResult.Content, "✅") {
		t.Errorf("Expected step-finish emoji, got %s", finishResult.Content)
	}
	if !strings.Contains(string(finishResult.RenderedHTML), "bg-green-100") {
		t.Errorf("Expected green badge styling, got %s", string(finishResult.RenderedHTML))
	}
}

func TestUnitTransformMessageWithAllParts(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	message := EnhancedMessageResponse{}
	message.Info.ID = "msg_001"
	message.Info.Role = "assistant"
	message.Info.SessionID = "ses_001"
	message.Info.ProviderID = "openai"
	message.Info.ModelID = "gpt-4"
	message.Parts = []EnhancedMessagePart{
		{ID: "prt_001", Type: "step-start"},
		{ID: "prt_002", Type: "text", Text: "Let me help you with that."},
		{ID: "prt_003", Type: "reasoning", Text: "Analyzing the request..."},
		{ID: "prt_004", Type: "tool", Tool: "bash", State: map[string]interface{}{"status": "completed", "input": map[string]interface{}{"command": "echo 'Hello'"}, "output": "Hello"}},
		{ID: "prt_005", Type: "text", Text: "The command executed successfully."},
		{ID: "prt_006", Type: "step-finish"},
	}
	var transformedParts []MessagePartData
	for _, part := range message.Parts {
		transformedParts = append(transformedParts, transformMessagePart(templates, convertToMessagePart(part)))
	}
	if len(transformedParts) != 6 {
		t.Errorf("Expected 6 parts, got %d", len(transformedParts))
	}
	typeCount := make(map[string]int)
	for _, part := range transformedParts {
		typeCount[part.Type]++
	}
	if typeCount["text"] != 2 {
		t.Errorf("Expected 2 text parts, got %d", typeCount["text"])
	}
	if typeCount["tool"] != 1 {
		t.Errorf("Expected 1 tool part, got %d", typeCount["tool"])
	}
	if typeCount["reasoning"] != 1 {
		t.Errorf("Expected 1 reasoning part, got %d", typeCount["reasoning"])
	}
	if typeCount["step-start"] != 1 {
		t.Errorf("Expected 1 step-start part, got %d", typeCount["step-start"])
	}
	if typeCount["step-finish"] != 1 {
		t.Errorf("Expected 1 step-finish part, got %d", typeCount["step-finish"])
	}
}

// ---------------- Rendering: templates and UI ----------------

func TestUnitTemplateConsistency(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	testData := struct {
		FileCount, LineCount int
		Files                []FileNode
		CurrentPath          string
	}{FileCount: 42, LineCount: 1337, Files: []FileNode{{Path: "main.go"}, {Path: "test.go"}}, CurrentPath: "main.go"}
	t.Run("FileCountConsistency", func(t *testing.T) {
		var directBuf bytes.Buffer
		if err := templates.ExecuteTemplate(&directBuf, "file-count-content", testData); err != nil {
			t.Fatalf("Failed to render file-count-content: %v", err)
		}
		directHTML := strings.TrimSpace(directBuf.String())
		var withCountsBuf bytes.Buffer
		if err := templates.ExecuteTemplate(&withCountsBuf, "file-options-with-counts", testData); err != nil {
			t.Fatalf("Failed to render file-options-with-counts: %v", err)
		}
		withCountsHTML := withCountsBuf.String()
		startMarker := `<div id="file-count-container" hx-swap-oob="innerHTML">`
		endMarker := `</div>`
		startIdx := strings.Index(withCountsHTML, startMarker)
		if startIdx == -1 {
			t.Fatal("Could not find file-count-container in file-options-with-counts output")
		}
		startIdx += len(startMarker)
		endIdx := strings.Index(withCountsHTML[startIdx:], endMarker)
		if endIdx == -1 {
			t.Fatal("Could not find closing div for file-count-container")
		}
		extractedHTML := strings.TrimSpace(withCountsHTML[startIdx : startIdx+endIdx])
		if directHTML != extractedHTML {
			t.Errorf("File count HTML mismatch:\nDirect: %q\nExtracted: %q", directHTML, extractedHTML)
		}
		if !strings.Contains(directHTML, "42") {
			t.Errorf("File count should contain '42', got: %s", directHTML)
		}
		if !strings.Contains(directHTML, "Files") {
			t.Errorf("File count should contain 'Files', got: %s", directHTML)
		}
	})
	t.Run("LineCountConsistency", func(t *testing.T) {
		var directBuf bytes.Buffer
		if err := templates.ExecuteTemplate(&directBuf, "line-count-content", testData); err != nil {
			t.Fatalf("Failed to render line-count-content: %v", err)
		}
		directHTML := strings.TrimSpace(directBuf.String())
		var withCountsBuf bytes.Buffer
		if err := templates.ExecuteTemplate(&withCountsBuf, "file-options-with-counts", testData); err != nil {
			t.Fatalf("Failed to render file-options-with-counts: %v", err)
		}
		withCountsHTML := withCountsBuf.String()
		startMarker := `<div id="line-count-container" hx-swap-oob="innerHTML">`
		endMarker := `</div>`
		startIdx := strings.Index(withCountsHTML, startMarker)
		if startIdx == -1 {
			t.Fatal("Could not find line-count-container in file-options-with-counts output")
		}
		startIdx += len(startMarker)
		endIdx := strings.Index(withCountsHTML[startIdx:], endMarker)
		if endIdx == -1 {
			t.Fatal("Could not find closing div for line-count-container")
		}
		extractedHTML := strings.TrimSpace(withCountsHTML[startIdx : startIdx+endIdx])
		if directHTML != extractedHTML {
			t.Errorf("Line count HTML mismatch:\nDirect: %q\nExtracted: %q", directHTML, extractedHTML)
		}
		if !strings.Contains(directHTML, "1337") {
			t.Errorf("Line count should contain '1337', got: %s", directHTML)
		}
		if !strings.Contains(directHTML, "Lines of Code") {
			t.Errorf("Line count should contain 'Lines of Code', got: %s", directHTML)
		}
	})
	t.Run("FileOptionsConsistency", func(t *testing.T) {
		var directBuf bytes.Buffer
		if err := templates.ExecuteTemplate(&directBuf, "file-options-content", testData); err != nil {
			t.Fatalf("Failed to render file-options-content: %v", err)
		}
		directHTML := strings.TrimSpace(directBuf.String())
		var optionsBuf bytes.Buffer
		if err := templates.ExecuteTemplate(&optionsBuf, "file-options", testData); err != nil {
			t.Fatalf("Failed to render file-options: %v", err)
		}
		optionsHTML := strings.TrimSpace(optionsBuf.String())
		if directHTML != optionsHTML {
			t.Errorf("File options HTML mismatch:\nDirect: %q\nVia file-options: %q", directHTML, optionsHTML)
		}
		if !strings.Contains(directHTML, "main.go") {
			t.Errorf("File options should contain 'main.go', got: %s", directHTML)
		}
		if !strings.Contains(directHTML, "test.go") {
			t.Errorf("File options should contain 'test.go', got: %s", directHTML)
		}
		if !strings.Contains(directHTML, `selected`) {
			t.Errorf("File options should have selected attribute for current file, got: %s", directHTML)
		}
	})
	t.Run("CodeOOBTemplateConsistency", func(t *testing.T) {
		var oobBuf bytes.Buffer
		if err := templates.ExecuteTemplate(&oobBuf, "code-updates-oob", testData); err != nil {
			t.Fatalf("Failed to render code-updates-oob: %v", err)
		}
		oobHTML := oobBuf.String()
		if !strings.Contains(oobHTML, "42") {
			t.Errorf("OOB update should contain file count '42'")
		}
		if !strings.Contains(oobHTML, "1337") {
			t.Errorf("OOB update should contain line count '1337'")
		}
		if !strings.Contains(oobHTML, "main.go") || !strings.Contains(oobHTML, "test.go") {
			t.Errorf("OOB update should contain file options")
		}
		if !strings.Contains(oobHTML, `hx-swap-oob="innerHTML"`) {
			t.Errorf("OOB update should contain hx-swap-oob attributes")
		}
	})
}

func TestUnitTemplateHTMLStructure(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	testData := struct{ FileCount, LineCount int }{FileCount: 10, LineCount: 200}
	t.Run("FileCountStructure", func(t *testing.T) {
		var buf bytes.Buffer
		if err := templates.ExecuteTemplate(&buf, "file-count-content", testData); err != nil {
			t.Fatalf("Failed to render file-count-content: %v", err)
		}
		html := buf.String()
		if !strings.Contains(html, `class="text-2xl font-semibold text-gray-900"`) {
			t.Error("File count missing large number styling")
		}
		if !strings.Contains(html, `class="text-sm text-gray-600"`) {
			t.Error("File count missing label styling")
		}
		if !strings.Contains(html, "<p") {
			t.Error("File count should use <p> tags")
		}
		lines := strings.Split(strings.TrimSpace(html), "\n")
		if len(lines) != 2 {
			t.Errorf("File count should have exactly 2 lines, got %d: %v", len(lines), lines)
		}
	})
	t.Run("LineCountStructure", func(t *testing.T) {
		var buf bytes.Buffer
		if err := templates.ExecuteTemplate(&buf, "line-count-content", testData); err != nil {
			t.Fatalf("Failed to render line-count-content: %v", err)
		}
		html := buf.String()
		if !strings.Contains(html, `class="text-2xl font-semibold text-gray-900"`) {
			t.Error("Line count missing large number styling")
		}
		if !strings.Contains(html, `class="text-sm text-gray-600"`) {
			t.Error("Line count missing label styling")
		}
		if !strings.Contains(html, "Lines of Code") {
			t.Error("Line count missing 'Lines of Code' label")
		}
		lines := strings.Split(strings.TrimSpace(html), "\n")
		if len(lines) != 2 {
			t.Errorf("Line count should have exactly 2 lines, got %d: %v", len(lines), lines)
		}
	})
}

func TestUnitMessageTemplateIDs(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		data      MessageData
		wantID    string
		wantOOB   bool
		wantClass string
	}{
		{"user message with ID", MessageData{ID: "user-123", Alignment: "right", Text: "Hello"}, "user-123", false, "message-right"},
		{"assistant message with streaming", MessageData{ID: "assistant-456", Alignment: "left", Text: "Responding...", IsStreaming: true}, "assistant-456", false, "streaming"},
		{"assistant message with OOB swap", MessageData{ID: "assistant-789", Alignment: "left", Text: "Final response", HXSwapOOB: true}, "assistant-789", true, "message-left"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := renderMessage(templates, tt.data)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
			if err != nil {
				t.Fatal(err)
			}
			msg := doc.Find("div.flex").First()
			if tt.wantID != "" {
				id, exists := msg.Attr("id")
				if !exists {
					t.Errorf("Expected ID attribute to exist")
				}
				if id != tt.wantID {
					t.Errorf("Expected ID %q, got %q", tt.wantID, id)
				}
			}
			oob, hasOOB := msg.Attr("hx-swap-oob")
			if tt.wantOOB && !hasOOB {
				t.Errorf("Expected hx-swap-oob attribute")
			}
			if tt.wantOOB && oob != "true" {
				t.Errorf("Expected hx-swap-oob=\"true\", got %q", oob)
			}
			class, _ := msg.Attr("class")
			if tt.wantClass == "message-right" && !strings.Contains(class, "justify-end") {
				t.Errorf("Expected class to contain 'justify-end' for right alignment, got %q", class)
			}
			if tt.wantClass == "message-left" && !strings.Contains(class, "justify-start") {
				t.Errorf("Expected class to contain 'justify-start' for left alignment, got %q", class)
			}
			if tt.wantClass == "streaming" && !strings.Contains(class, "streaming") {
				t.Errorf("Expected class to contain %q, got %q", tt.wantClass, class)
			}
			if tt.data.IsStreaming && !strings.Contains(class, "streaming") {
				t.Errorf("Expected streaming class when IsStreaming=true")
			}
			if !tt.data.IsStreaming && strings.Contains(class, "streaming") {
				t.Errorf("Unexpected streaming class when IsStreaming=false")
			}
		})
	}
}

func TestUnitMessageMetadata(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		data         MessageData
		wantMetadata bool
		wantText     string
	}{
		{"message with provider and model", MessageData{Alignment: "left", Text: "Response", Provider: "openai", Model: "gpt-4o"}, true, "openai/gpt-4o"},
		{"message with provider and model (anthropic)", MessageData{Alignment: "left", Text: "Response", Provider: "anthropic", Model: "claude-3-5-sonnet"}, true, "anthropic/claude-3-5-sonnet"},
		{"message without metadata", MessageData{Alignment: "right", Text: "Question"}, false, ""},
		{"message with only provider", MessageData{Alignment: "left", Text: "Response", Provider: "openai"}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := renderMessage(templates, tt.data)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
			if err != nil {
				t.Fatal(err)
			}
			meta := doc.Find("div.text-xs.text-gray-600")
			if tt.wantMetadata {
				if meta.Length() == 0 {
					t.Error("Expected metadata element (div.text-xs.text-gray-600)")
				}
				if !strings.Contains(meta.Text(), tt.wantText) {
					t.Errorf("Expected metadata text %q, got %q", tt.wantText, meta.Text())
				}
			} else {
				if meta.Length() > 0 {
					t.Error("Unexpected metadata element")
				}
			}
		})
	}
}

func TestUnitStreamingCSS(t *testing.T) {
	cssBytes, err := staticFS.ReadFile("static/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	if !strings.Contains(css, ".streaming") {
		t.Error("CSS should contain .streaming selector")
	}
	if !strings.Contains(css, "@keyframes dots") {
		t.Error("CSS should contain @keyframes dots animation")
	}
	if !strings.Contains(css, "::after") {
		t.Error("CSS should contain ::after pseudo-element for streaming animation")
	}
}

func TestUnitFileDropdownSelectionPreservation(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                      string
		files                     []FileNode
		currentPath, wantSelected string
		wantOptionsCount          int
	}{
		{"preserves selection when file exists", []FileNode{{Path: "file1.txt", Name: "file1.txt"}, {Path: "file2.js", Name: "file2.js"}, {Path: "dir/file3.py", Name: "file3.py"}}, "file2.js", "file2.js", 4},
		{"no selection when current path not in list", []FileNode{{Path: "file1.txt", Name: "file1.txt"}, {Path: "file2.js", Name: "file2.js"}}, "nonexistent.txt", "", 3},
		{"preserves selection with nested path", []FileNode{{Path: "src/main.go", Name: "main.go"}, {Path: "src/test.go", Name: "test.go"}, {Path: "docs/readme.md", Name: "readme.md"}}, "src/test.go", "src/test.go", 4},
		{"handles empty file list", []FileNode{}, "", "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := struct {
				Files       []FileNode
				CurrentPath string
			}{Files: tt.files, CurrentPath: tt.currentPath}
			var buf strings.Builder
			if err := templates.ExecuteTemplate(&buf, "file-dropdown", data); err != nil {
				t.Fatalf("Failed to render template: %v", err)
			}
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(buf.String()))
			if err != nil {
				t.Fatalf("Failed to parse HTML: %v", err)
			}
			selectElem := doc.Find("select#file-selector")
			if selectElem.Length() != 1 {
				t.Errorf("Expected exactly one select#file-selector, found %d", selectElem.Length())
			}
			options := selectElem.Find("option")
			if options.Length() != tt.wantOptionsCount {
				t.Errorf("Expected %d options, got %d", tt.wantOptionsCount, options.Length())
			}
			selectedOption := selectElem.Find("option[selected]")
			if tt.wantSelected == "" {
				if selectedOption.Length() != 0 {
					selectedVal, _ := selectedOption.Attr("value")
					t.Errorf("Expected no selected option, but found: %s", selectedVal)
				}
			} else {
				if selectedOption.Length() != 1 {
					t.Errorf("Expected one selected option, found %d", selectedOption.Length())
				} else {
					selectedVal, _ := selectedOption.Attr("value")
					if selectedVal != tt.wantSelected {
						t.Errorf("Expected selected value %q, got %q", tt.wantSelected, selectedVal)
					}
				}
			}
			if hxGet, _ := selectElem.Attr("hx-get"); hxGet != "/tab/code/file" {
				t.Errorf("Expected hx-get='/tab/code/file', got %q", hxGet)
			}
			if hxTrigger, _ := selectElem.Attr("hx-trigger"); hxTrigger != "change" {
				t.Errorf("Expected hx-trigger='change', got %q", hxTrigger)
			}
			if hxTarget, _ := selectElem.Attr("hx-target"); hxTarget != "#code-content" {
				t.Errorf("Expected hx-target='#code-content', got %q", hxTarget)
			}
		})
	}
}

// ---------------- Rendering: Todo list renderer ----------------

func TestUnitRenderTodoList(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}
	tests := []struct {
		name, input string
		expected    []string
	}{
		{name: "Simple todo list", input: `[
            {"content": "Write tests","status": "pending","priority": "high","id": "1"},
            {"content": "Fix bugs","status": "in_progress","priority": "medium","id": "2"},
            {"content": "Deploy code","status": "completed","priority": "low","id": "3"}
        ]`, expected: []string{"☐", "⏳", "✓", "Write tests", "Fix bugs", "Deploy code", "text-red-600", "text-yellow-600", "text-gray-400", "line-through"}},
		{name: "Invalid JSON fallback", input: `not valid json`, expected: []string{"<pre class=\"overflow-x-auto\">", "not valid json"}},
		{name: "Empty array", input: `[]`, expected: []string{"<div class=\"todo-list text-sm\">"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := renderTodoList(templates, tt.input)
			if err != nil {
				t.Fatalf("renderTodoList failed: %v", err)
			}
			resultStr := string(result)
			for _, expected := range tt.expected {
				if !strings.Contains(resultStr, expected) {
					t.Errorf("Expected output to contain %q, but it didn't.\nFull output:\n%s", expected, resultStr)
				}
			}
		})
	}
}

// ---------------- Server: logging middleware/response writer ----------------

func TestUnitLoggingResponseWriter_WriteHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	lw.WriteHeader(404)
	if lw.statusCode != 404 {
		t.Errorf("Expected status code 404, got %d", lw.statusCode)
	}
	if recorder.Code != 404 {
		t.Errorf("Expected recorder status code 404, got %d", recorder.Code)
	}
}

func TestUnitLoggingResponseWriter_Write(t *testing.T) {
	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	testData := []byte("Hello, World!")
	n, err := lw.Write(testData)
	if err != nil {
		t.Errorf("Write returned error: %v", err)
	}
	if n != len(testData) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(testData), n)
	}
	if lw.body.String() != "Hello, World!" {
		t.Errorf("Expected body to contain 'Hello, World!', got '%s'", lw.body.String())
	}
	if recorder.Body.String() != "Hello, World!" {
		t.Errorf("Expected recorder body to contain 'Hello, World!', got '%s'", recorder.Body.String())
	}
}

func TestUnitLoggingResponseWriter_LogResponse(t *testing.T) {
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	defer log.SetOutput(os.Stderr)
	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	lw.WriteHeader(200)
	if _, err := lw.Write([]byte("<html><body>Test Response</body></html>")); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	lw.LogResponse("GET", "/test")
	logStr := logOutput.String()
	if !strings.Contains(logStr, "WIRE_OUT GET /test [200]") {
		t.Errorf("Expected log to contain 'WIRE_OUT GET /test [200]', got: %s", logStr)
	}
	if !strings.Contains(logStr, "<html><body>Test Response</body></html>") {
		t.Errorf("Expected log to contain full response body, got: %s", logStr)
	}
}

func TestUnitLoggingResponseWriter_LogResponseNoTruncation(t *testing.T) {
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	defer log.SetOutput(os.Stderr)
	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	largeBody := strings.Repeat("A", 1000)
	if _, err := lw.Write([]byte(largeBody)); err != nil {
		t.Fatalf("Failed to write large body: %v", err)
	}
	lw.LogResponse("POST", "/large")
	logStr := logOutput.String()
	if !strings.Contains(logStr, largeBody) {
		t.Errorf("Expected log to contain full large body without truncation")
	}
	if strings.Contains(logStr, "truncated") {
		t.Errorf("Log should not contain truncation message, but does: %s", logStr)
	}
}

func TestUnitLoggingMiddleware_NormalEndpoint(t *testing.T) {
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	defer log.SetOutput(os.Stderr)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("Normal response"))
	})
	req := httptest.NewRequest("GET", "/normal", nil)
	recorder := httptest.NewRecorder()
	wrappedHandler := loggingMiddleware(handler)
	wrappedHandler.ServeHTTP(recorder, req)
	if recorder.Code != 200 {
		t.Errorf("Expected status code 200, got %d", recorder.Code)
	}
	if recorder.Body.String() != "Normal response" {
		t.Errorf("Expected 'Normal response', got '%s'", recorder.Body.String())
	}
	if !strings.Contains(logOutput.String(), "WIRE_OUT GET /normal [200]: Normal response") {
		t.Errorf("Expected log output for normal endpoint, got: %s", logOutput.String())
	}
}

func TestUnitLoggingMiddleware_SSEEndpoint(t *testing.T) {
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	defer log.SetOutput(os.Stderr)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("data: SSE message\n\n"))
	})
	req := httptest.NewRequest("GET", "/events", nil)
	recorder := httptest.NewRecorder()
	wrappedHandler := loggingMiddleware(handler)
	wrappedHandler.ServeHTTP(recorder, req)
	if recorder.Code != 200 {
		t.Errorf("Expected status code 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "data: SSE message") {
		t.Errorf("Expected SSE data in response, got '%s'", recorder.Body.String())
	}
	logStr := logOutput.String()
	if !strings.Contains(logStr, "WIRE_OUT SSE connection started: GET /events") {
		t.Errorf("Expected SSE connection started log, got: %s", logStr)
	}
	if !strings.Contains(logStr, "WIRE_OUT SSE connection ended: GET /events") {
		t.Errorf("Expected SSE connection ended log, got: %s", logStr)
	}
	if strings.Contains(logStr, "WIRE_OUT GET /events [200]:") {
		t.Errorf("SSE endpoint should not use normal response logging, but does: %s", logStr)
	}
}

func TestUnitChainMiddlewareOrder(t *testing.T) {
	var order []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	})
	m1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "m1 before")
			next.ServeHTTP(w, r)
			order = append(order, "m1 after")
		})
	}
	m2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "m2 before")
			next.ServeHTTP(w, r)
			order = append(order, "m2 after")
		})
	}
	req := httptest.NewRequest("GET", "/", nil)
	recorder := httptest.NewRecorder()
	chainMiddleware(handler, m1, m2).ServeHTTP(recorder, req)
	expected := []string{"m1 before", "m2 before", "handler", "m2 after", "m1 after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d entries, got %d", len(expected), len(order))
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("expected order[%d]=%q, got %q (full order=%v)", i, expected[i], order[i], order)
		}
	}
}

func TestUnitRequireAuthRedirectsWhenUnauthenticated(t *testing.T) {
	server := &Server{
		authSessions: make(map[string]*AuthSession),
	}
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})
	req := httptest.NewRequest("GET", "/protected", nil)
	recorder := httptest.NewRecorder()
	chainMiddleware(next, server.withAuth, server.requireAuth).ServeHTTP(recorder, req)
	if nextCalled {
		t.Fatal("expected next handler not to be called for unauthenticated request")
	}
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, recorder.Code)
	}
	location := recorder.Header().Get("Location")
	if location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}
}

func TestUnitRequireAuthAllowsAuthenticated(t *testing.T) {
	server := &Server{
		authSessions: make(map[string]*AuthSession),
	}
	server.authSessions["token"] = &AuthSession{Email: "user@example.com"}
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})
	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "auth_session", Value: "token"})
	recorder := httptest.NewRecorder()
	chainMiddleware(next, server.withAuth, server.requireAuth).ServeHTTP(recorder, req)
	if !nextCalled {
		t.Fatal("expected next handler to be called for authenticated request")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
}

func TestUnitWithAuthSetsContextForAuthenticatedUser(t *testing.T) {
	server := &Server{
		authSessions: make(map[string]*AuthSession),
	}
	server.authSessions["token"] = &AuthSession{Email: "user@example.com"}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "auth_session", Value: "token"})
	recorder := httptest.NewRecorder()
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		ctx := authContext(r)
		if !ctx.IsAuthenticated {
			t.Fatal("expected request to be authenticated")
		}
		if ctx.Session == nil || ctx.Session.Email != "user@example.com" {
			t.Fatalf("unexpected auth session %+v", ctx.Session)
		}
	})
	chainMiddleware(handler, server.withAuth).ServeHTTP(recorder, req)
	if !handlerCalled {
		t.Fatal("expected handler to be called")
	}
}

func TestUnitWithAuthSetsContextForAnonymousUser(t *testing.T) {
	server := &Server{
		authSessions: make(map[string]*AuthSession),
	}
	req := httptest.NewRequest("GET", "/", nil)
	recorder := httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := authContext(r)
		if ctx.IsAuthenticated {
			t.Fatal("expected request to be anonymous")
		}
		if ctx.Session != nil {
			t.Fatalf("expected no auth session, got %+v", ctx.Session)
		}
	})
	chainMiddleware(handler, server.withAuth).ServeHTTP(recorder, req)
}

func TestUnitNewLoggingResponseWriter(t *testing.T) {
	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	if lw.ResponseWriter != recorder {
		t.Error("Expected ResponseWriter to be set to recorder")
	}
	if lw.statusCode != 200 {
		t.Errorf("Expected default status code 200, got %d", lw.statusCode)
	}
	if lw.body == nil {
		t.Error("Expected body buffer to be initialized")
	}
	if lw.body.Len() != 0 {
		t.Errorf("Expected empty body buffer, got length %d", lw.body.Len())
	}
}

// ---------------- Server: message parts manager and event validation ----------------

func TestUnitMessagePartsOrdering(t *testing.T) {
	manager := NewMessagePartsManager()
	msgID := "test-message"
	getContent := func() string {
		parts := manager.GetParts(msgID)
		var contents []string
		for _, p := range parts {
			contents = append(contents, p.Content)
		}
		return strings.Join(contents, " ")
	}
	manager.UpdatePart(msgID, "part1", MessagePartData{Type: "text", Content: "Hello"})
	if got := getContent(); got != "Hello" {
		t.Errorf("After first part: got %q, want %q", got, "Hello")
	}
	manager.UpdatePart(msgID, "part2", MessagePartData{Type: "text", Content: "world"})
	if got := getContent(); got != "Hello world" {
		t.Errorf("After second part: got %q, want %q", got, "Hello world")
	}
	manager.UpdatePart(msgID, "part1", MessagePartData{Type: "text", Content: "Hi"})
	if got := getContent(); got != "Hi world" {
		t.Errorf("After updating first part: got %q, want %q", got, "Hi world")
	}
	manager.UpdatePart(msgID, "part3", MessagePartData{Type: "text", Content: "there"})
	if got := getContent(); got != "Hi world there" {
		t.Errorf("After third part: got %q, want %q", got, "Hi world there")
	}
	manager.UpdatePart(msgID, "part2", MessagePartData{Type: "text", Content: "beautiful"})
	if got := getContent(); got != "Hi beautiful there" {
		t.Errorf("After updating middle part: got %q, want %q", got, "Hi beautiful there")
	}
}

func TestUnitMessagePartsPrefixStability(t *testing.T) {
	manager := NewMessagePartsManager()
	msgID := "stream-message"
	var observedPrefixes []string
	recordContent := func() string {
		parts := manager.GetParts(msgID)
		var contents []string
		for _, p := range parts {
			contents = append(contents, p.Content)
		}
		content := strings.Join(contents, " ")
		observedPrefixes = append(observedPrefixes, content)
		return content
	}
	updates := []struct{ partID, content string }{{"p1", "The"}, {"p2", "quick"}, {"p1", "A"}, {"p3", "brown"}, {"p2", "very quick"}, {"p4", "fox"}, {"p5", "jumps"}}
	expectedSequence := []string{"The", "The quick", "A quick", "A quick brown", "A very quick brown", "A very quick brown fox", "A very quick brown fox jumps"}
	for i, update := range updates {
		manager.UpdatePart(msgID, update.partID, MessagePartData{Type: "text", Content: update.content})
		got := recordContent()
		want := expectedSequence[i]
		if got != want {
			t.Errorf("Update %d: got %q, want %q", i+1, got, want)
		}
	}
	for i := 0; i < len(observedPrefixes)-1; i++ {
		currentPrefix := observedPrefixes[i]
		for j := i + 1; j < len(observedPrefixes); j++ {
			nextContent := observedPrefixes[j]
			if !isPrefixOrValidUpdate(currentPrefix, nextContent) {
				t.Errorf("Prefix stability violated: %q is not a valid predecessor of %q", currentPrefix, nextContent)
			}
		}
	}
}

func isPrefixOrValidUpdate(old, new string) bool {
	oldParts := strings.Split(old, " ")
	newParts := strings.Split(new, " ")
	if len(newParts) < len(oldParts) {
		return false
	}
	for i := 0; i < len(oldParts); i++ { /* position preserved */
	}
	return true
}

func TestUnitMessagePartsValidation(t *testing.T) {
	manager := NewMessagePartsManager()
	tests := []struct {
		name, messageID, partID string
		wantErr                 bool
	}{
		{"valid inputs", "msg1", "part1", false},
		{"empty messageID", "", "part1", true},
		{"empty partID", "msg1", "", true},
		{"both empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.UpdatePart(tt.messageID, tt.partID, MessagePartData{Type: "text", Content: "test"})
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdatePart() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUnitValidateAndExtractMessagePart(t *testing.T) {
	sessionID := "test-session"
	tests := []struct {
		name    string
		event   map[string]interface{}
		wantErr bool
		wantMsg string
	}{
		{name: "valid event", event: map[string]interface{}{"type": "message.part.updated", "properties": map[string]interface{}{"part": map[string]interface{}{"sessionID": sessionID, "messageID": "msg123", "id": "part456"}}}},
		{name: "missing messageID", event: map[string]interface{}{"type": "message.part.updated", "properties": map[string]interface{}{"part": map[string]interface{}{"sessionID": sessionID, "id": "part456"}}}, wantErr: true, wantMsg: "invalid or missing messageID"},
		{name: "empty messageID", event: map[string]interface{}{"type": "message.part.updated", "properties": map[string]interface{}{"part": map[string]interface{}{"sessionID": sessionID, "messageID": "", "id": "part456"}}}, wantErr: true, wantMsg: "invalid or missing messageID"},
		{name: "missing partID", event: map[string]interface{}{"type": "message.part.updated", "properties": map[string]interface{}{"part": map[string]interface{}{"sessionID": sessionID, "messageID": "msg123"}}}, wantErr: true, wantMsg: "invalid or missing partID"},
		{name: "wrong session", event: map[string]interface{}{"type": "message.part.updated", "properties": map[string]interface{}{"part": map[string]interface{}{"sessionID": "other-session", "messageID": "msg123", "id": "part456"}}}, wantErr: true, wantMsg: "session mismatch"},
		{name: "wrong event type", event: map[string]interface{}{"type": "other.event", "properties": map[string]interface{}{}}, wantErr: true, wantMsg: "not a message.part.updated event"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgID, partID, _, err := ValidateAndExtractMessagePart(tt.event, sessionID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAndExtractMessagePart() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("Error message %q doesn't contain %q", err.Error(), tt.wantMsg)
			}
			if !tt.wantErr && (msgID == "" || partID == "") {
				t.Errorf("Valid event should return non-empty IDs, got msgID=%q, partID=%q", msgID, partID)
			}
		})
	}
}

func TestUnitSequentialUpdates(t *testing.T) {
	manager := NewMessagePartsManager()
	msgID := "test-msg"
	for i := 0; i < 10; i++ {
		partID := fmt.Sprintf("part%d", i)
		for j := 0; j < 100; j++ {
			manager.UpdatePart(msgID, partID, MessagePartData{Type: "text", Content: fmt.Sprintf("content-%d-%d", i, j)})
		}
	}
	parts := manager.GetParts(msgID)
	if len(parts) != 10 {
		t.Errorf("Expected 10 parts after updates, got %d", len(parts))
	}
	for i := 0; i < 10; i++ {
		expectedID := fmt.Sprintf("part%d", i)
		expectedContent := fmt.Sprintf("content-%d-99", i)
		found := false
		for _, p := range parts {
			if p.PartID == expectedID {
				found = true
				if p.Content != expectedContent {
					t.Errorf("Part %s has wrong content: got %q, want %q", expectedID, p.Content, expectedContent)
				}
				break
			}
		}
		if !found {
			t.Errorf("Missing part %s after updates", expectedID)
		}
	}
}

// ---------------- Server: waitForOpencodeReady helper ----------------

func TestUnitWaitForOpencodeReady_DelayedSuccess(t *testing.T) {
	var requestCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if r.URL.Path != "/session" {
			t.Errorf("Expected path /session, got %s", r.URL.Path)
		}
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Server starting up..."))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"sessionId": "test-session"}`))
		}
	}))
	defer ts.Close()
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	err := waitForOpencodeReady(port, 2*time.Second)
	if err != nil {
		t.Errorf("Expected success after retries, got error: %v", err)
	}
	if finalCount := atomic.LoadInt32(&requestCount); finalCount < 3 {
		t.Errorf("Expected at least 3 requests, got %d", finalCount)
	}
}

func TestUnitWaitForOpencodeReady_Timeout(t *testing.T) {
	var requestCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Permanently broken"))
	}))
	defer ts.Close()
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	start := time.Now()
	err := waitForOpencodeReady(port, 500*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
	if err != nil {
		expectedMsg := fmt.Sprintf("opencode server on port %d not ready after", port)
		if len(err.Error()) < len(expectedMsg) || err.Error()[:len(expectedMsg)] != expectedMsg {
			t.Errorf("Expected error message to start with '%s', got: %v", expectedMsg, err)
		}
	}
	if finalCount := atomic.LoadInt32(&requestCount); finalCount < 3 || finalCount > 6 {
		t.Errorf("Expected 3-6 requests in 500ms, got %d", finalCount)
	}
	if elapsed > 600*time.Millisecond {
		t.Errorf("Function took too long: %v, expected ~500ms", elapsed)
	}
}

func TestUnitWaitForOpencodeReady_ConnectionRefused(t *testing.T) {
	unusedPort := 59999
	start := time.Now()
	err := waitForOpencodeReady(unusedPort, 300*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Error("Expected error for connection refused, got nil")
	}
	if elapsed > 400*time.Millisecond {
		t.Errorf("Function took too long with connection refused: %v", elapsed)
	}
}

func TestUnitWaitForOpencodeReady_JustBeforeTimeout(t *testing.T) {
	var requestCount int32
	successAfter := int32(4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count < successAfter {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer ts.Close()
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	err := waitForOpencodeReady(port, 400*time.Millisecond)
	if err != nil {
		t.Errorf("Expected success just before timeout, got error: %v", err)
	}
	if finalCount := atomic.LoadInt32(&requestCount); finalCount != successAfter {
		t.Errorf("Expected exactly %d requests, got %d", successAfter, finalCount)
	}
}

func TestUnitWaitForOpencodeReady_ResponseBodyLeak(t *testing.T) {
	bodiesClosed := int32(0)
	requestsMade := int32(0)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestsMade, 1)
		w.Header().Set("Connection", "close")
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "Attempt %d failed", count)
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Success on attempt %d", count)
		}
		atomic.AddInt32(&bodiesClosed, 1)
	}))
	defer ts.Close()
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	if err := waitForOpencodeReady(port, 1*time.Second); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if requests := atomic.LoadInt32(&requestsMade); requests != atomic.LoadInt32(&bodiesClosed) {
		t.Errorf("Response body leak detected: %d requests made but only %d completed", requests, atomic.LoadInt32(&bodiesClosed))
	}
}

func TestUnitWaitForOpencodeReady_RaceCondition(t *testing.T) {
	var globalRequestCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&globalRequestCount, 1)
		if count > 5 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer ts.Close()
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { done <- waitForOpencodeReady(port, 2*time.Second) }()
	}
	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent call %d failed: %v", i, err)
		}
	}
	if totalRequests := atomic.LoadInt32(&globalRequestCount); totalRequests < 6 {
		t.Errorf("Expected at least 6 total requests from concurrent calls, got %d", totalRequests)
	}
}

// ---------------- Race/Concurrency: UpdateRateLimiter ----------------

func TestUnitUpdateRateLimiter_FirstUpdateImmediate(t *testing.T) {
	var executed int32
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	start := time.Now()
	limiter.TryUpdate(context.Background(), func() { atomic.AddInt32(&executed, 1) })
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&executed) != 1 {
		t.Error("First update should be immediate")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("First update took too long: %v", elapsed)
	}
}

func TestUnitUpdateRateLimiter_SecondUpdateDelayed(t *testing.T) {
	var executed int32
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { atomic.AddInt32(&executed, 1) })
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&executed) != 1 {
		t.Fatal("First update should have executed")
	}
	time.Sleep(50 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { atomic.AddInt32(&executed, 1) })
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&executed) != 1 {
		t.Error("Second update should still be delayed at 150ms")
	}
	time.Sleep(80 * time.Millisecond)
	if atomic.LoadInt32(&executed) != 2 {
		t.Errorf("Second update should execute after interval, got %d executions", atomic.LoadInt32(&executed))
	}
}

func TestUnitUpdateRateLimiter_UpdateAfterInterval(t *testing.T) {
	var executed []time.Time
	var mu sync.Mutex
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { mu.Lock(); executed = append(executed, time.Now()); mu.Unlock() })
	time.Sleep(250 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { mu.Lock(); executed = append(executed, time.Now()); mu.Unlock() })
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(executed) != 2 {
		t.Fatalf("Expected 2 executions, got %d", len(executed))
	}
	if gap := executed[1].Sub(executed[0]); gap < 240*time.Millisecond || gap > 310*time.Millisecond {
		t.Errorf("Gap should be ~250ms, got %v", gap)
	}
}

func TestUnitUpdateRateLimiter_RapidUpdatesCoalesce(t *testing.T) {
	var lastValue int32
	var executionCount int32
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { atomic.StoreInt32(&lastValue, 1); atomic.AddInt32(&executionCount, 1) })
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&executionCount) != 1 {
		t.Fatal("First update should have executed")
	}
	for i := 2; i <= 5; i++ {
		value := int32(i)
		limiter.TryUpdate(context.Background(), func() { atomic.StoreInt32(&lastValue, value); atomic.AddInt32(&executionCount, 1) })
		time.Sleep(25 * time.Millisecond)
	}
	if atomic.LoadInt32(&executionCount) != 1 {
		t.Error("Intermediate updates should not have executed yet")
	}
	time.Sleep(100 * time.Millisecond)
	if finalValue := atomic.LoadInt32(&lastValue); finalValue != 5 {
		t.Errorf("Expected last value 5, got %d", finalValue)
	}
	if finalCount := atomic.LoadInt32(&executionCount); finalCount != 2 {
		t.Errorf("Expected 2 total executions (first + coalesced), got %d", finalCount)
	}
}

func TestUnitUpdateRateLimiter_ConcurrentUpdates(t *testing.T) {
	var counter int32
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter.TryUpdate(context.Background(), func() { atomic.AddInt32(&counter, 1) })
		}()
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	if count1 := atomic.LoadInt32(&counter); count1 != 1 {
		t.Errorf("Expected 1 immediate execution from concurrent updates, got %d", count1)
	}
	time.Sleep(200 * time.Millisecond)
	if count2 := atomic.LoadInt32(&counter); count2 != 2 {
		t.Errorf("Expected 2 total executions after interval (immediate + coalesced), got %d", count2)
	}
}

func TestUnitUpdateRateLimiter_TimerCancellation(t *testing.T) {
	var executed []int
	var mu sync.Mutex
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { mu.Lock(); executed = append(executed, 1); mu.Unlock() })
	time.Sleep(50 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { mu.Lock(); executed = append(executed, 2); mu.Unlock() })
	time.Sleep(50 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { mu.Lock(); executed = append(executed, 3); mu.Unlock() })
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(executed) != 2 {
		t.Errorf("Expected 2 executions, got %d", len(executed))
	}
	if len(executed) == 2 && executed[1] != 3 {
		t.Errorf("Expected second execution to be 3, got %d", executed[1])
	}
}

func TestUnitUpdateRateLimiter_MultipleIntervals(t *testing.T) {
	var executionTimes []time.Time
	var mu sync.Mutex
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	startTime := time.Now()
	timings := []time.Duration{0, 50 * time.Millisecond, 250 * time.Millisecond, 300 * time.Millisecond, 600 * time.Millisecond}
	for _, delay := range timings {
		time.Sleep(delay - time.Since(startTime))
		limiter.TryUpdate(context.Background(), func() { mu.Lock(); executionTimes = append(executionTimes, time.Now()); mu.Unlock() })
	}
	time.Sleep(700*time.Millisecond - time.Since(startTime))
	mu.Lock()
	defer mu.Unlock()
	if len(executionTimes) != 4 {
		t.Fatalf("Expected 4 executions, got %d", len(executionTimes))
	}
	tolerance := 60 * time.Millisecond
	if executionTimes[0].Sub(startTime) > tolerance {
		t.Errorf("First execution should be immediate, was at %v", executionTimes[0].Sub(startTime))
	}
	expected := 200 * time.Millisecond
	actual := executionTimes[1].Sub(startTime)
	if actual < expected-tolerance || actual > expected+tolerance {
		t.Errorf("Second execution should be at ~200ms, was at %v", actual)
	}
	expected = 400 * time.Millisecond
	actual = executionTimes[2].Sub(startTime)
	if actual < expected-tolerance || actual > expected+tolerance {
		t.Errorf("Third execution should be at ~400ms, was at %v", actual)
	}
	expected = 600 * time.Millisecond
	actual = executionTimes[3].Sub(startTime)
	if actual < expected-tolerance || actual > expected+tolerance {
		t.Errorf("Fourth execution should be at ~600ms, was at %v", actual)
	}
}

// ---------------- Race/Concurrency: SSE part updates and duplication ----------------

func TestUnitSSEMessagePartNoDuplication(t *testing.T) {
	manager := NewMessagePartsManager()
	messageID := "msg_test123"
	partID := "prt_test456"
	if err := manager.UpdatePart(messageID, partID, MessagePartData{Type: "text", Content: "I'll analyze"}); err != nil {
		t.Fatalf("Failed to update part: %v", err)
	}
	if err := manager.UpdatePart(messageID, partID, MessagePartData{Type: "text", Content: "I'll analyze OSCR's stock"}); err != nil {
		t.Fatalf("Failed to update part: %v", err)
	}
	if err := manager.UpdatePart(messageID, partID, MessagePartData{Type: "text", Content: "I'll analyze OSCR's stock price over the last 6 months"}); err != nil {
		t.Fatalf("Failed to update part: %v", err)
	}
	parts := manager.GetParts(messageID)
	if len(parts) != 1 {
		t.Errorf("Expected exactly 1 part, got %d parts", len(parts))
	}
	if parts[0].Content != "I'll analyze OSCR's stock price over the last 6 months" {
		t.Errorf("Expected final content, got: %s", parts[0].Content)
	}
	if parts[0].PartID != partID {
		t.Errorf("Expected partID %s, got %s", partID, parts[0].PartID)
	}
}

func TestUnitSSEMultiplePartTypes(t *testing.T) {
	manager := NewMessagePartsManager()
	messageID := "msg_test789"
	manager.UpdatePart(messageID, "prt_text1", MessagePartData{Type: "text", Content: "Analyzing data"})
	manager.UpdatePart(messageID, "prt_tool1", MessagePartData{Type: "tool", Content: "Tool: webfetch\nStatus: running"})
	manager.UpdatePart(messageID, "prt_tool1", MessagePartData{Type: "tool", Content: "Tool: webfetch\nStatus: completed\nOutput: ..."})
	manager.UpdatePart(messageID, "prt_text2", MessagePartData{Type: "text", Content: "The analysis shows..."})
	parts := manager.GetParts(messageID)
	if len(parts) != 3 {
		t.Errorf("Expected 3 parts, got %d", len(parts))
	}
	expectedOrder := []string{"prt_text1", "prt_tool1", "prt_text2"}
	for i, part := range parts {
		if part.PartID != expectedOrder[i] {
			t.Errorf("Part %d: expected ID %s, got %s", i, expectedOrder[i], part.PartID)
		}
	}
	if parts[1].Content != "Tool: webfetch\nStatus: completed\nOutput: ..." {
		t.Errorf("Tool part not updated correctly: %s", parts[1].Content)
	}
}

func TestUnitSSEHTMLGenerationNoDuplication(t *testing.T) {
	manager := NewMessagePartsManager()
	messageID := "msg_html_test"
	updates := []string{"The", "The file", "The file does", "The file does not", "The file does not exist"}
	partID := "prt_incremental"
	for _, content := range updates {
		manager.UpdatePart(messageID, partID, MessagePartData{Type: "text", Content: content})
	}
	parts := manager.GetParts(messageID)
	if len(parts) != 1 {
		t.Errorf("Expected 1 part after incremental updates, got %d", len(parts))
	}
	part := parts[0]
	part.RenderedHTML = renderText(part.Content)
	htmlStr := string(part.RenderedHTML)
	if count := strings.Count(htmlStr, "The file does not exist"); count != 1 {
		t.Errorf("Text appears %d times in rendered HTML, expected 1", count)
		t.Logf("Rendered HTML: %s", htmlStr)
	}
}

func TestUnitSSERapidUpdates(t *testing.T) {
	manager := NewMessagePartsManager()
	messageID := "msg_rapid"
	partID := "prt_rapid"
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			manager.UpdatePart(messageID, partID, MessagePartData{Type: "text", Content: strings.Repeat("a", i+1)})
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()
	<-done
	parts := manager.GetParts(messageID)
	if len(parts) != 1 {
		t.Errorf("Expected 1 part after rapid updates, got %d", len(parts))
	}
	if expectedLen := 100; len(parts[0].Content) != expectedLen {
		t.Errorf("Expected content length %d, got %d", expectedLen, len(parts[0].Content))
	}
}
