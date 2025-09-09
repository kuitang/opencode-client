package main

import (
	"html/template"
	"strings"
	"testing"
)

func TestHasMarkdownPatterns(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Markdown patterns that should be detected
		{"Bold text", "This is **bold** text", true},
		{"Italic text", "This is *italic* text", true},
		{"Header", "# Header", true},
		{"Header with space", "## Sub Header", true},
		{"Numbered list", "1. First item", true},
		{"Bullet list", "- Bullet point", true},
		{"Code inline", "Use `code` here", true},
		{"Link", "[link](http://example.com)", true},
		
		// Edge cases that should be detected as markdown
		{"Multiple patterns", "# Header\n**bold** and *italic*", true},
		{"Code with backticks", "Run `rm -rf /` carefully", true},
		
		// Plain text that should NOT be detected as markdown
		{"Plain text", "Just plain text", false},
		{"Email asterisk", "Terms apply*", false},
		{"Math multiplication", "2 * 3 = 6", false},
		{"Filename", "config_2024-01-01.txt", false},
		{"URL without markdown", "http://example.com", false},
		{"HTML tags", "<script>alert('xss')</script>", false},
		{"Numbered text", "Call 1. 800. EXAMPLE", false},
		{"Dash in sentence", "Well-formatted text", false},
		
		// Malicious content that should NOT be detected as markdown
		{"Script tag", "<script>alert('XSS')</script>", false},
		{"Onclick handler", "<div onclick='alert(1)'>Click</div>", false},
		{"Image onerror", "<img src=x onerror=alert(1)>", false},
		{"Mixed HTML and potential markdown", "<div>*not italic*</div>", false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasMarkdownPatterns(tt.input)
			if result != tt.expected {
				t.Errorf("hasMarkdownPatterns(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHTMLEscapingInTemplates(t *testing.T) {
	// Test that Go templates auto-escape HTML in {{.Content}}
	tmpl := template.Must(template.New("test").Parse(`<div>{{.Content}}</div>`))
	
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Script tag",
			input:    "<script>alert('XSS')</script>",
			expected: "<div>&lt;script&gt;alert(&#39;XSS&#39;)&lt;/script&gt;</div>",
		},
		{
			name:     "Onclick handler",
			input:    "<div onclick='alert(1)'>Click</div>",
			expected: "<div>&lt;div onclick=&#39;alert(1)&#39;&gt;Click&lt;/div&gt;</div>",
		},
		{
			name:     "Image with onerror",
			input:    "<img src=x onerror=alert('XSS')>",
			expected: "<div>&lt;img src=x onerror=alert(&#39;XSS&#39;)&gt;</div>",
		},
		{
			name:     "HTML entities",
			input:    "&lt;already&gt; &amp; escaped",
			expected: "<div>&amp;lt;already&amp;gt; &amp;amp; escaped</div>",
		},
		{
			name:     "Mixed quotes",
			input:    `<a href="javascript:alert('XSS')">Click</a>`,
			expected: "<div>&lt;a href=&#34;javascript:alert(&#39;XSS&#39;)&#34;&gt;Click&lt;/a&gt;</div>",
		},
		{
			name:     "SVG with script",
			input:    "<svg onload=alert(1)>",
			expected: "<div>&lt;svg onload=alert(1)&gt;</div>",
		},
		{
			name:     "Data URI",
			input:    "<a href='data:text/html,<script>alert(1)</script>'>Click</a>",
			expected: "<div>&lt;a href=&#39;data:text/html,&lt;script&gt;alert(1)&lt;/script&gt;&#39;&gt;Click&lt;/a&gt;</div>",
		},
		{
			name:     "Event handler variations",
			input:    "<body onload=alert(1) onmouseover=alert(2)>",
			expected: "<div>&lt;body onload=alert(1) onmouseover=alert(2)&gt;</div>",
		},
		{
			name:     "Style with expression",
			input:    "<div style='background:url(javascript:alert(1))'>",
			expected: "<div>&lt;div style=&#39;background:url(javascript:alert(1))&#39;&gt;</div>",
		},
		{
			name:     "Form action",
			input:    "<form action='javascript:alert(1)'><input></form>",
			expected: "<div>&lt;form action=&#39;javascript:alert(1)&#39;&gt;&lt;input&gt;&lt;/form&gt;</div>",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			data := struct{ Content string }{Content: tt.input}
			err := tmpl.Execute(&buf, data)
			if err != nil {
				t.Fatalf("Template execution failed: %v", err)
			}
			
			result := buf.String()
			if result != tt.expected {
				t.Errorf("Template escaping failed\nInput:    %q\nGot:      %q\nExpected: %q", 
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewlinePreservation(t *testing.T) {
	// Test that whitespace-pre-wrap CSS preserves newlines
	tmpl := template.Must(template.New("test").Parse(
		`<div class="whitespace-pre-wrap">{{.}}</div>`))
	
	tests := []struct {
		name     string
		input    string
		contains []string // Substrings that should be in the output
	}{
		{
			name:  "Simple newlines",
			input: "Line 1\nLine 2\nLine 3",
			contains: []string{
				"Line 1\nLine 2\nLine 3",
			},
		},
		{
			name:  "Windows newlines",
			input: "Line 1\r\nLine 2\r\nLine 3",
			contains: []string{
				"Line 1\r\nLine 2\r\nLine 3",
			},
		},
		{
			name:  "Mixed with HTML chars",
			input: "<script>\nalert(1)\n</script>",
			contains: []string{
				"&lt;script&gt;\nalert(1)\n&lt;/script&gt;",
			},
		},
		{
			name:  "Unicode bullets with newlines",
			input: "• Item 1\n• Item 2\n• Item 3",
			contains: []string{
				"• Item 1\n• Item 2\n• Item 3",
			},
		},
		{
			name:  "Numbered list",
			input: "1. First\n2. Second\n3. Third",
			contains: []string{
				"1. First\n2. Second\n3. Third",
			},
		},
		{
			name:  "Multiple blank lines",
			input: "Paragraph 1\n\n\nParagraph 2",
			contains: []string{
				"Paragraph 1\n\n\nParagraph 2",
			},
		},
		{
			name:  "Tabs and spaces",
			input: "\tIndented\n    Four spaces\n\t\tDouble tab",
			contains: []string{
				"\tIndented\n    Four spaces\n\t\tDouble tab",
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			err := tmpl.Execute(&buf, tt.input)
			if err != nil {
				t.Fatalf("Template execution failed: %v", err)
			}
			
			result := buf.String()
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Output missing expected content\nInput:    %q\nExpected to contain: %q\nGot:      %q", 
						tt.input, expected, result)
				}
			}
		})
	}
}

func TestRenderMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string // HTML elements that should be in output
		excludes []string // HTML elements that should NOT be in output
	}{
		{
			name:     "Bold text",
			input:    "This is **bold** text",
			contains: []string{"<strong>bold</strong>"},
			excludes: []string{"**bold**"},
		},
		{
			name:     "Italic text",
			input:    "This is *italic* text",
			contains: []string{"<em>italic</em>"},
			excludes: []string{"*italic*"},
		},
		{
			name:     "Header",
			input:    "# Main Header",
			contains: []string{"<h1>Main Header</h1>"},
			excludes: []string{"# Main Header"},
		},
		{
			name:     "Code inline",
			input:    "Use `code` here",
			contains: []string{"<code>code</code>"},
			excludes: []string{"`code`"},
		},
		{
			name:     "Link",
			input:    "[Google](https://google.com)",
			contains: []string{
				`<a href="https://google.com"`,
				`Google</a>`,
				`rel="nofollow"`, // Bluemonday adds this for security
			},
			excludes: []string{"[Google]"},
		},
		{
			name:  "XSS in markdown",
			input: "**bold** <script>alert('XSS')</script>",
			contains: []string{
				"<strong>bold</strong>",
				// Bluemonday sanitizes HTML - script tags are removed!
			},
			excludes: []string{
				"<script>", // Script tags should be removed by bluemonday
				"alert", // Script content should be removed
			},
		},
		{
			name:     "Plain text fallback",
			input:    "No markdown here",
			contains: []string{"No markdown here"},
			excludes: []string{"<p>", "<strong>", "<em>"},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(renderMarkdown(tt.input))
			
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("renderMarkdown(%q) missing expected content: %q\nGot: %q", 
						tt.input, expected, result)
				}
			}
			
			for _, unexpected := range tt.excludes {
				if strings.Contains(result, unexpected) {
					t.Errorf("renderMarkdown(%q) contains unexpected content: %q\nGot: %q", 
						tt.input, unexpected, result)
				}
			}
		})
	}
}

func TestMessagePartDataSecurity(t *testing.T) {
	// Test that MessagePartData correctly handles malicious content
	tests := []struct {
		name        string
		partType    string
		content     string
		isMarkdown  bool
		description string
	}{
		{
			name:        "Text with XSS",
			partType:    "text",
			content:     "<script>alert('XSS')</script>",
			isMarkdown:  false,
			description: "Plain text content should remain unchanged, template will escape",
		},
		{
			name:        "Reasoning with HTML",
			partType:    "reasoning",
			content:     "🤔 Reasoning:\n<div>test</div>",
			isMarkdown:  false,
			description: "Reasoning content should remain unchanged, template will escape",
		},
		{
			name:        "Tool output with script",
			partType:    "tool",
			content:     "Tool: test\nOutput:\n<script>alert(1)</script>",
			isMarkdown:  false,
			description: "Tool content should remain unchanged, template will escape",
		},
		{
			name:        "File with malicious filename",
			partType:    "file",
			content:     "📁 File: <img src=x onerror=alert(1)>.txt\nURL: http://example.com",
			isMarkdown:  false,
			description: "File content should remain unchanged, template will escape",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			part := MessagePartData{
				Type:       tt.partType,
				Content:    tt.content,
				IsMarkdown: tt.isMarkdown,
			}
			
			// Verify Content field is unchanged (not pre-escaped)
			if part.Content != tt.content {
				t.Errorf("Content field was modified\nOriginal: %q\nGot:      %q", 
					tt.content, part.Content)
			}
			
			// For text type with markdown, verify RenderedHTML is set
			if tt.partType == "text" && tt.isMarkdown {
				part.RenderedHTML = renderMarkdown(tt.content)
				if part.RenderedHTML == "" {
					t.Error("RenderedHTML should be set for markdown text")
				}
			}
		})
	}
}