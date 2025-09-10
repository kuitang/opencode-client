package main

import (
	"html/template"
	"strings"
	"testing"
)

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
	// Test that preserve-breaks CSS preserves newlines
	tmpl := template.Must(template.New("test").Parse(
		`<div class="preserve-breaks">{{.}}</div>`))

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
				Type:    tt.partType,
				Content: tt.content,
				// IsMarkdown field removed in unified renderer
			}

			// Verify Content field is unchanged (not pre-escaped)
			if part.Content != tt.content {
				t.Errorf("Content field was modified\nOriginal: %q\nGot:      %q",
					tt.content, part.Content)
			}

			// For text type with markdown, verify RenderedHTML is set
			if tt.partType == "text" && tt.isMarkdown {
				part.RenderedHTML = renderText(tt.content)
				if part.RenderedHTML == "" {
					t.Error("RenderedHTML should be set for markdown text")
				}
			}
		})
	}
}
