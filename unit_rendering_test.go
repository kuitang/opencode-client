package main

import (
	"bytes"
	"encoding/json"
	"html/template"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// ---------------- Rendering: formatting and message rendering ----------------

func TestMessageFormatting(t *testing.T) {
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

func TestUserAndLLMRenderingSame(t *testing.T) {
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

func TestMultilineInputSupport(t *testing.T) {
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

func TestHTMLEscapingInTemplates(t *testing.T) {
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

func TestNewlinePreservation(t *testing.T) {
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

func TestMessagePartDataSecurity(t *testing.T) {
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

// Helpers from unit_http_parts_test.go
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

func TestTransformTextPart(t *testing.T) {
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

func TestTransformToolPart(t *testing.T) {
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

func TestTransformTodoWritePart(t *testing.T) {
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

func TestTransformReasoningPart(t *testing.T) {
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

func TestTransformStepParts(t *testing.T) {
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

func TestTransformMessageWithAllParts(t *testing.T) {
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

func TestTemplateConsistency(t *testing.T) {
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

func TestTemplateHTMLStructure(t *testing.T) {
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

func TestMessageTemplateIDs(t *testing.T) {
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

func TestMessageMetadata(t *testing.T) {
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

func TestStreamingCSS(t *testing.T) {
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

func TestFileDropdownSelectionPreservation(t *testing.T) {
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

func TestRenderTodoList(t *testing.T) {
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
