package main

import (
	"strings"
	"testing"
)

func TestRenderTextWithAutolink(t *testing.T) {
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
			result := string(renderText(tt.input))

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

func TestRenderTextWithLineBreaksAndURLs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name:  "URL with preserved newlines (via CSS)",
			input: "Line 1\nhttps://example.com\nLine 3",
			contains: []string{
				`<a href="https://example.com"`,
				"Line 1",
				"Line 3",
				// Note: Line breaks preserved via CSS white-space: pre-line, not <br> tags
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
			},
		},
		{
			name:  "Multiple newlines",
			input: "Paragraph 1\n\nParagraph 2 with https://link.com",
			contains: []string{
				"Paragraph 1",
				"Paragraph 2",
				`<a href="https://link.com"`,
				// Double newlines create <p> tags in markdown
			},
		},
		{
			name:  "Plain text with URL",
			input: "Text with https://example.com link",
			contains: []string{
				"Text with",
				`<a href="https://example.com"`,
			},
		},
		{
			name:  "XSS prevention",
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
			result := string(renderText(tt.input))

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

func TestUnifiedTextRendering(t *testing.T) {
	input := "Line 1\nLine 2\nLine 3"

	// Unified renderer doesn't add <br> tags
	// Line breaks are preserved via CSS white-space: pre-line
	result := string(renderText(input))
	if strings.Contains(result, "<br") {
		t.Errorf("Unified renderer should not add <br> tags\nGot: %q", result)
	}
	
	// Content should still be present
	if !strings.Contains(result, "Line 1") || !strings.Contains(result, "Line 2") {
		t.Errorf("Content should be preserved\nGot: %q", result)
	}
}

func TestURLLinkingComposition(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		checkFunc func(string) bool
	}{
		{
			name:  "Markdown with URLs",
			input: "# Header\n\nVisit https://example.com for **bold** text",
			checkFunc: func(result string) bool {
				return strings.Contains(result, "<h1>") &&
					strings.Contains(result, "<strong>") &&
					strings.Contains(result, `<a href="https://example.com"`)
			},
		},
		{
			name:  "Plain text with URLs and line breaks (CSS preserved)",
			input: "• Item 1\n• Visit https://example.com\n• Item 3",
			checkFunc: func(result string) bool {
				return strings.Contains(result, "• Item 1") &&
					strings.Contains(result, `<a href="https://example.com"`) &&
					strings.Contains(result, "• Item 3")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderText(tt.input)

			if !tt.checkFunc(string(result)) {
				t.Errorf("Composition test failed for %s\nGot: %q", tt.name, result)
			}
		})
	}
}