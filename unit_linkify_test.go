package main

import (
	"html/template"
	"strings"
	"testing"
)

func TestRenderMarkdownWithAutolink(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name:  "Plain URL becomes link",
			input: "Check out https://example.com for more info",
			contains: []string{
				`<a href="https://example.com"`,
				`>https://example.com</a>`,
			},
		},
		{
			name:  "Multiple URLs",
			input: "Visit https://google.com and http://github.com",
			contains: []string{
				`<a href="https://google.com"`,
				`<a href="http://github.com"`,
			},
		},
		{
			name:  "URL in markdown context",
			input: "**Bold** text with https://example.com link",
			contains: []string{
				`<strong>Bold</strong>`,
				`<a href="https://example.com"`,
			},
		},
		{
			name:  "Markdown link syntax still works",
			input: "[Google](https://google.com) is a search engine",
			contains: []string{
				`<a href="https://google.com"`,
				`>Google</a>`,
			},
			excludes: []string{
				"[Google]",
			},
		},
		{
			name:  "XSS attempt in URL",
			input: "Click here: javascript:alert('XSS')",
			excludes: []string{
				"<a href=\"javascript:", // Should not be linkified
			},
		},
		{
			name:  "Malicious HTML with URL",
			input: "<script>alert('XSS')</script> https://safe.com",
			contains: []string{
				`<a href="https://safe.com"`,
			},
			excludes: []string{
				"<script>",
				"alert",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(renderMarkdown(tt.input))

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected to contain %q\nGot: %q", expected, result)
				}
			}

			for _, unexpected := range tt.excludes {
				if strings.Contains(result, unexpected) {
					t.Errorf("Should not contain %q\nGot: %q", unexpected, result)
				}
			}
		})
	}
}

func TestRenderPlainTextWithAutolink(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name:  "URL with preserved newlines",
			input: "Line 1\nhttps://example.com\nLine 3",
			contains: []string{
				`<a href="https://example.com"`,
				"<br", // HardLineBreak adds <br> tags
			},
		},
		{
			name:  "Unicode bullets with URLs",
			input: "• Visit https://example.com\n• Check http://github.com\n• Done",
			contains: []string{
				"• Visit",
				`<a href="https://example.com"`,
				"• Check",
				`<a href="http://github.com"`,
				"• Done",
				"<br",
			},
		},
		{
			name:  "Multiple newlines preserved",
			input: "Paragraph 1\n\nParagraph 2 with https://link.com",
			contains: []string{
				"Paragraph 1",
				"Paragraph 2",
				`<a href="https://link.com"`,
				// Note: double newlines create <p> tags, not <br>
			},
		},
		{
			name:  "Plain text with minimal markdown",
			input: "Text with https://example.com link",
			contains: []string{
				"Text with",
				`<a href="https://example.com"`,
			},
		},
		{
			name:  "XSS prevention in plain text",
			input: "<script>alert('XSS')</script>\nhttps://safe.com",
			contains: []string{
				`<a href="https://safe.com"`,
			},
			excludes: []string{
				"<script>",
				"alert",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(renderPlainText(tt.input))

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected to contain %q\nGot: %q", expected, result)
				}
			}

			for _, unexpected := range tt.excludes {
				if strings.Contains(result, unexpected) {
					t.Errorf("Should not contain %q\nGot: %q", unexpected, result)
				}
			}
		})
	}
}

func TestMarkdownVsPlainTextLineBreaks(t *testing.T) {
	input := "Line 1\nLine 2\nLine 3"

	// Markdown should NOT preserve single line breaks
	markdownResult := string(renderMarkdown(input))
	if strings.Contains(markdownResult, "<br") {
		t.Errorf("Markdown should not add <br> for single newlines\nGot: %q", markdownResult)
	}

	// Plain text SHOULD preserve line breaks
	plainResult := string(renderPlainText(input))
	if !strings.Contains(plainResult, "<br") {
		t.Errorf("Plain text should add <br> for newlines\nGot: %q", plainResult)
	}
}

func TestURLLinkingComposition(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		isMarkdown bool
		checkFunc  func(string) bool
	}{
		{
			name:       "Markdown with URLs",
			input:      "# Header\n\nVisit https://example.com for **bold** text",
			isMarkdown: true,
			checkFunc: func(result string) bool {
				return strings.Contains(result, "<h1>") &&
					strings.Contains(result, "<strong>") &&
					strings.Contains(result, `<a href="https://example.com"`)
			},
		},
		{
			name:       "Plain text with URLs",
			input:      "• Item 1\n• Visit https://example.com\n• Item 3",
			isMarkdown: false,
			checkFunc: func(result string) bool {
				return strings.Contains(result, "• Item 1") &&
					strings.Contains(result, `<a href="https://example.com"`) &&
					strings.Contains(result, "<br")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result template.HTML
			if tt.isMarkdown {
				result = renderMarkdown(tt.input)
			} else {
				result = renderPlainText(tt.input)
			}

			if !tt.checkFunc(string(result)) {
				t.Errorf("Composition test failed for %s\nGot: %q", tt.name, result)
			}
		})
	}
}