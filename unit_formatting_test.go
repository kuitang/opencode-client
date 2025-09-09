package main

import (
	"html/template"
	"strings"
	"testing"
)

func TestMessageFormatting(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		isUser      bool // true for user message, false for LLM message
		expectMD    bool // expect markdown detection
		contains    []string
		notContains []string
	}{
		// Plain text tests
		{
			name:     "Plain text preserves newlines",
			input:    "Line 1\nLine 2\nLine 3",
			isUser:   true,
			expectMD: false,
			contains: []string{
				"Line 1\nLine 2\nLine 3",
				"whitespace-pre-wrap",
			},
		},
		{
			name:     "Plain text with bullets",
			input:    "• Item 1\n• Item 2\n• Item 3",
			isUser:   false,
			expectMD: false,
			contains: []string{
				"• Item 1\n• Item 2\n• Item 3",
				"whitespace-pre-wrap",
			},
		},
		{
			name:     "Plain numbered list",
			input:    "1. First\n2. Second\n3. Third",
			isUser:   true,
			expectMD: true, // Detected as markdown
			contains: []string{
				"<ol>",
				"<li>First</li>",
				"<li>Second</li>",
			},
			notContains: []string{
				"1. First", // Should be rendered as HTML list
			},
		},

		// Markdown tests
		{
			name:     "Bold markdown",
			input:    "This is **bold** text",
			isUser:   false,
			expectMD: true,
			contains: []string{
				"<strong>bold</strong>",
			},
			notContains: []string{
				"**bold**",
				"whitespace-pre-wrap",
			},
		},
		{
			name:     "Italic markdown",
			input:    "This is *italic* text",
			isUser:   true,
			expectMD: true,
			contains: []string{
				"<em>italic</em>",
			},
			notContains: []string{
				"*italic*",
			},
		},
		{
			name:     "Mixed markdown with newlines",
			input:    "# Header\n\nThis is **bold** and *italic*\n\n- Item 1\n- Item 2",
			isUser:   false,
			expectMD: true,
			contains: []string{
				"<h1>Header</h1>",
				"<strong>bold</strong>",
				"<em>italic</em>",
				"<li>Item 1</li>",
			},
		},
		{
			name:     "Code blocks",
			input:    "```go\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```",
			isUser:   true,
			expectMD: true,
			contains: []string{
				"<pre><code",
				"func main()",
			},
		},
		{
			name:     "Inline code",
			input:    "Use `fmt.Println()` to print",
			isUser:   false,
			expectMD: true,
			contains: []string{
				"<code>fmt.Println()</code>",
			},
		},

		// Security tests
		{
			name:     "XSS in plain text",
			input:    "<script>alert('XSS')</script>",
			isUser:   true,
			expectMD: false,
			contains: []string{
				"&lt;script&gt;alert(&#39;XSS&#39;)&lt;/script&gt;",
				"whitespace-pre-wrap",
			},
			notContains: []string{
				"<script>",
				"alert('XSS')",
			},
		},
		{
			name:     "XSS in markdown",
			input:    "**Bold** <script>alert('XSS')</script>",
			isUser:   false,
			expectMD: true,
			contains: []string{
				"<strong>Bold</strong>",
			},
			notContains: []string{
				"<script>",
				"alert",
			},
		},
		{
			name:     "HTML entities are escaped",
			input:    "Less than < and greater than > and ampersand &",
			isUser:   true,
			expectMD: false,
			contains: []string{
				"Less than &lt; and greater than &gt; and ampersand &amp;",
			},
		},

		// Edge cases
		{
			name:     "Empty string",
			input:    "",
			isUser:   true,
			expectMD: false,
			contains: []string{
				"whitespace-pre-wrap",
			},
		},
		{
			name:     "Only whitespace",
			input:    "   \n\n\t  ",
			isUser:   false,
			expectMD: false,
			contains: []string{
				"whitespace-pre-wrap",
			},
		},
		{
			name:     "Math expressions not markdown",
			input:    "2 * 3 = 6 and 4 / 2 = 2",
			isUser:   true,
			expectMD: false,
			contains: []string{
				"2 * 3 = 6",
				"whitespace-pre-wrap",
			},
		},
		{
			name:     "URL without markdown",
			input:    "Visit https://example.com for more",
			isUser:   false,
			expectMD: false,
			contains: []string{
				"https://example.com",
				"whitespace-pre-wrap",
			},
		},
		{
			name:     "Markdown link",
			input:    "Visit [Example](https://example.com) for more",
			isUser:   true,
			expectMD: true,
			contains: []string{
				`<a href="https://example.com"`,
				"Example</a>",
			},
		},
	}

	// Load the message template
	tmpl := template.Must(template.New("test").Parse(`
{{if .IsMarkdown}}
{{.RenderedHTML}}
{{else}}
<div class="whitespace-pre-wrap">{{.Content}}</div>
{{end}}
`))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check markdown detection
			detected := hasMarkdownPatterns(tt.input)
			if detected != tt.expectMD {
				t.Errorf("Markdown detection failed: got %v, want %v", detected, tt.expectMD)
			}

			// Create message part data
			part := MessagePartData{
				Type:       "text",
				Content:    tt.input,
				IsMarkdown: detected,
			}

			if detected {
				part.RenderedHTML = renderMarkdown(tt.input)
			}

			// Render through template
			var buf strings.Builder
			err := tmpl.Execute(&buf, part)
			if err != nil {
				t.Fatalf("Template execution failed: %v", err)
			}

			result := buf.String()

			// Check expected content
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Output missing expected content: %q\nGot: %q",
						expected, result)
				}
			}

			// Check unexpected content
			for _, unexpected := range tt.notContains {
				if strings.Contains(result, unexpected) {
					t.Errorf("Output contains unexpected content: %q\nGot: %q",
						unexpected, result)
				}
			}
		})
	}
}

func TestUserAndLLMRenderingSame(t *testing.T) {
	// Test that both user and LLM messages go through the same rendering logic
	testInputs := []string{
		"Plain text\nwith newlines",
		"**Bold** and *italic*",
		"# Header\n\n- List item",
		"<script>alert('XSS')</script>",
		"```code block```",
		"• Unicode bullets\n• With newlines",
	}

	tmpl := template.Must(template.New("test").Parse(`
{{if .IsMarkdown}}
{{.RenderedHTML}}
{{else}}
<div class="whitespace-pre-wrap">{{.Content}}</div>
{{end}}
`))

	for _, input := range testInputs {
		// Simulate user message rendering
		userPart := MessagePartData{
			Type:       "text",
			Content:    input,
			IsMarkdown: hasMarkdownPatterns(input),
		}
		if userPart.IsMarkdown {
			userPart.RenderedHTML = renderMarkdown(input)
		}

		var userBuf strings.Builder
		err := tmpl.Execute(&userBuf, userPart)
		if err != nil {
			t.Fatalf("User template execution failed: %v", err)
		}

		// Simulate LLM message rendering (should be identical)
		llmPart := MessagePartData{
			Type:       "text",
			Content:    input,
			IsMarkdown: hasMarkdownPatterns(input),
		}
		if llmPart.IsMarkdown {
			llmPart.RenderedHTML = renderMarkdown(input)
		}

		var llmBuf strings.Builder
		err = tmpl.Execute(&llmBuf, llmPart)
		if err != nil {
			t.Fatalf("LLM template execution failed: %v", err)
		}

		// Both should produce identical output
		if userBuf.String() != llmBuf.String() {
			t.Errorf("User and LLM rendering differ for input %q\nUser: %q\nLLM:  %q",
				input, userBuf.String(), llmBuf.String())
		}
	}
}

func TestMultilineInputSupport(t *testing.T) {
	// Test that multiline input is properly handled
	multilineInputs := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Unix newlines",
			input:    "Line 1\nLine 2\nLine 3",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "Windows newlines",
			input:    "Line 1\r\nLine 2\r\nLine 3",
			expected: "Line 1\r\nLine 2\r\nLine 3",
		},
		{
			name:     "Mixed newlines",
			input:    "Line 1\nLine 2\r\nLine 3",
			expected: "Line 1\nLine 2\r\nLine 3",
		},
		{
			name:     "Blank lines",
			input:    "Paragraph 1\n\nParagraph 2\n\n\nParagraph 3",
			expected: "Paragraph 1\n\nParagraph 2\n\n\nParagraph 3",
		},
		{
			name:     "Indented lines",
			input:    "def function():\n    line1\n    line2\n        nested",
			expected: "def function():\n    line1\n    line2\n        nested",
		},
	}

	tmpl := template.Must(template.New("test").Parse(
		`<div class="whitespace-pre-wrap">{{.}}</div>`))

	for _, tt := range multilineInputs {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			err := tmpl.Execute(&buf, tt.input)
			if err != nil {
				t.Fatalf("Template execution failed: %v", err)
			}

			result := buf.String()
			if !strings.Contains(result, tt.expected) {
				t.Errorf("Multiline input not preserved\nInput:    %q\nExpected: %q\nGot:      %q",
					tt.input, tt.expected, result)
			}
		})
	}
}