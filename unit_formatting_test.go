package main

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestMessageFormatting(t *testing.T) {
	// Test both function-level and component-level rendering
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	tests := []struct {
		name         string
		input        string
		expectMD     bool     // expect markdown detection
		hasElements  []string // HTML elements that should exist
		hasClasses   []string // CSS classes that should exist
		containsText []string // Text content that should be preserved
	}{
		// Plain text tests
		{
			name:         "Plain text preserves newlines",
			input:        "Line 1\nLine 2\nLine 3",
			expectMD:     false,
			hasClasses:   []string{"preserve-breaks"},
			containsText: []string{"Line 1", "Line 2", "Line 3"},
		},
		{
			name:         "Plain text with bullets",
			input:        "• Item 1\n• Item 2\n• Item 3",
			expectMD:     false,
			hasClasses:   []string{"preserve-breaks"},
			containsText: []string{"• Item 1", "• Item 2", "• Item 3"},
		},
		{
			name:         "Plain numbered list becomes markdown",
			input:        "1. First\n2. Second\n3. Third",
			expectMD:     true,
			hasElements:  []string{"ol", "li"},
			containsText: []string{"First", "Second", "Third"},
		},

		// Markdown tests
		{
			name:         "Bold markdown",
			input:        "This is **bold** text",
			expectMD:     true,
			hasElements:  []string{"strong"},
			containsText: []string{"This is", "bold", "text"},
		},
		{
			name:         "Italic markdown",
			input:        "This is *italic* text",
			expectMD:     true,
			hasElements:  []string{"em"},
			containsText: []string{"This is", "italic", "text"},
		},
		{
			name:         "Mixed markdown with newlines",
			input:        "# Header\n\nThis is **bold** and *italic*\n\n- Item 1\n- Item 2",
			expectMD:     true,
			hasElements:  []string{"h1", "strong", "em", "li"},
			containsText: []string{"Header", "bold", "italic", "Item 1", "Item 2"},
		},
		{
			name:         "Code blocks",
			input:        "```go\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```",
			expectMD:     true,
			hasElements:  []string{"pre", "code"},
			containsText: []string{"func main()"},
		},
		{
			name:         "Inline code",
			input:        "Use `fmt.Println()` to print",
			expectMD:     true,
			hasElements:  []string{"code"},
			containsText: []string{"Use", "fmt.Println()", "to print"},
		},

		// Security tests - XSS should be sanitized
		{
			name:         "XSS in plain text",
			input:        "<script>alert('XSS')</script>",
			expectMD:     false,
			hasClasses:   []string{"preserve-breaks"},
			containsText: []string{}, // Script tags should be removed entirely by Bluemonday
		},
		{
			name:         "XSS in markdown",
			input:        "**Bold** <script>alert('XSS')</script>",
			expectMD:     true,
			hasElements:  []string{"strong"},
			containsText: []string{"Bold"}, // Script should be removed, bold should remain
		},
		{
			name:         "HTML entities handling",
			input:        "Less than < and greater than > and ampersand &",
			expectMD:     false,
			hasClasses:   []string{"preserve-breaks"},
			containsText: []string{"Less than", "and greater than", "and ampersand"},
		},

		// Edge cases
		{
			name:         "Empty string",
			input:        "",
			expectMD:     false,
			hasClasses:   []string{"preserve-breaks"},
			containsText: []string{},
		},
		{
			name:         "Math expressions not markdown",
			input:        "2 * 3 = 6 and 4 / 2 = 2",
			expectMD:     false,
			hasClasses:   []string{"preserve-breaks"},
			containsText: []string{"2 * 3 = 6", "4 / 2 = 2"},
		},
		{
			name:         "URL with autolink",
			input:        "Visit https://example.com for more",
			expectMD:     true, // URLs get autolinked
			hasElements:  []string{"a"},
			containsText: []string{"Visit", "for more"},
		},
		{
			name:         "Markdown link",
			input:        "Visit [Example](https://example.com) for more",
			expectMD:     true,
			hasElements:  []string{"a"},
			containsText: []string{"Visit", "Example", "for more"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the rendering pipeline: renderText -> renderMessage -> HTML parsing
			msgData := MessageData{
				Alignment: "left",
				Parts: []MessagePartData{{
					Type:         "text",
					Content:      tt.input,
					RenderedHTML: renderText(tt.input),
				}},
			}

			// Render the full message using templates
			html, err := renderMessage(templates, msgData)
			if err != nil {
				t.Fatalf("renderMessage failed: %v", err)
			}

			// Parse HTML with goquery for semantic testing
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
			if err != nil {
				t.Fatalf("Failed to parse HTML: %v", err)
			}

			// Check for expected HTML elements
			for _, element := range tt.hasElements {
				if doc.Find(element).Length() == 0 {
					t.Errorf("Expected HTML element <%s> not found\nHTML: %s", element, html)
				}
			}

			// Check for expected CSS classes
			for _, class := range tt.hasClasses {
				if doc.Find("."+class).Length() == 0 {
					t.Errorf("Expected CSS class .%s not found\nHTML: %s", class, html)
				}
			}

			// Check for expected text content
			for _, text := range tt.containsText {
				if !strings.Contains(doc.Text(), text) {
					t.Errorf("Expected text %q not found in content\nText: %s", text, doc.Text())
				}
			}

			// Security check: ensure no dangerous elements exist
			if doc.Find("script").Length() > 0 {
				t.Errorf("Dangerous script element found in output\nHTML: %s", html)
			}
		})
	}
}

func TestUserAndLLMRenderingSame(t *testing.T) {
	// Test that both user and LLM messages render identically
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	testInputs := []string{
		"Plain text\nwith newlines",
		"**Bold** and *italic*",
		"# Header\n\n- List item",
		"<script>alert('XSS')</script>",
		"```code block```",
		"• Unicode bullets\n• With newlines",
	}

	for _, input := range testInputs {
		// Create user message
		userMsg := MessageData{
			Alignment: "right",
			Parts: []MessagePartData{{
				Type:         "text",
				Content:      input,
				RenderedHTML: renderText(input),
			}},
		}

		// Create LLM message
		llmMsg := MessageData{
			Alignment: "left",
			Parts: []MessagePartData{{
				Type:         "text",
				Content:      input,
				RenderedHTML: renderText(input),
			}},
		}

		userHTML, err := renderMessage(templates, userMsg)
		if err != nil {
			t.Fatalf("User message rendering failed: %v", err)
		}

		llmHTML, err := renderMessage(templates, llmMsg)
		if err != nil {
			t.Fatalf("LLM message rendering failed: %v", err)
		}

		// Parse both with goquery and compare content (ignoring alignment differences)
		userDoc, _ := goquery.NewDocumentFromReader(strings.NewReader(userHTML))
		llmDoc, _ := goquery.NewDocumentFromReader(strings.NewReader(llmHTML))

		// The text content should be identical
		if userDoc.Find(".prose").Text() != llmDoc.Find(".prose").Text() {
			t.Errorf("User and LLM content differ for input %q\nUser: %s\nLLM: %s",
				input, userDoc.Find(".prose").Text(), llmDoc.Find(".prose").Text())
		}
	}
}

func TestMultilineInputSupport(t *testing.T) {
	// Test that multiline input is properly handled using the full rendering pipeline
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	multilineInputs := []struct {
		name         string
		input        string
		expectedText []string // Text fragments that should be preserved
	}{
		{
			name:         "Unix newlines",
			input:        "Line 1\nLine 2\nLine 3",
			expectedText: []string{"Line 1", "Line 2", "Line 3"},
		},
		{
			name:         "Windows newlines",
			input:        "Line 1\r\nLine 2\r\nLine 3",
			expectedText: []string{"Line 1", "Line 2", "Line 3"},
		},
		{
			name:         "Blank lines",
			input:        "Paragraph 1\n\nParagraph 2\n\n\nParagraph 3",
			expectedText: []string{"Paragraph 1", "Paragraph 2", "Paragraph 3"},
		},
		{
			name:         "Indented lines",
			input:        "def function():\n    line1\n    line2\n        nested",
			expectedText: []string{"def function()", "line1", "line2", "nested"},
		},
	}

	for _, tt := range multilineInputs {
		t.Run(tt.name, func(t *testing.T) {
			msgData := MessageData{
				Alignment: "left",
				Parts: []MessagePartData{{
					Type:         "text",
					Content:      tt.input,
					RenderedHTML: renderText(tt.input),
				}},
			}

			html, err := renderMessage(templates, msgData)
			if err != nil {
				t.Fatalf("renderMessage failed: %v", err)
			}

			doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
			if err != nil {
				t.Fatalf("Failed to parse HTML: %v", err)
			}

			// Check that all expected text fragments are preserved
			content := doc.Text()
			for _, expected := range tt.expectedText {
				if !strings.Contains(content, expected) {
					t.Errorf("Expected text %q not found in rendered content\nInput: %q\nContent: %q",
						expected, tt.input, content)
				}
			}
		})
	}
}
