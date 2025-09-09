package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// Test that message templates render with correct IDs
func TestMessageTemplateIDs(t *testing.T) {
	server, err := NewServer(GetTestPort())
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
		{
			name: "user message with ID",
			data: MessageData{
				ID:        "user-123",
				Alignment: "right",
				Text:      "Hello",
			},
			wantID:    "user-123",
			wantOOB:   false,
			wantClass: "message-right",
		},
		{
			name: "assistant message with streaming",
			data: MessageData{
				ID:          "assistant-456",
				Alignment:   "left",
				Text:        "Responding...",
				IsStreaming: true,
			},
			wantID:    "assistant-456",
			wantOOB:   false,
			wantClass: "streaming",
		},
		{
			name: "assistant message with OOB swap",
			data: MessageData{
				ID:        "assistant-789",
				Alignment: "left",
				Text:      "Final response",
				HXSwapOOB: true,
			},
			wantID:    "assistant-789",
			wantOOB:   true,
			wantClass: "message-left",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := server.templates.ExecuteTemplate(&buf, "message", tt.data)
			if err != nil {
				t.Fatal(err)
			}

			doc, err := goquery.NewDocumentFromReader(&buf)
			if err != nil {
				t.Fatal(err)
			}

			msg := doc.Find(".message").First()

			// Check ID
			if tt.wantID != "" {
				id, exists := msg.Attr("id")
				if !exists {
					t.Errorf("Expected ID attribute to exist")
				}
				if id != tt.wantID {
					t.Errorf("Expected ID %q, got %q", tt.wantID, id)
				}
			}

			// Check OOB swap
			oob, hasOOB := msg.Attr("hx-swap-oob")
			if tt.wantOOB && !hasOOB {
				t.Errorf("Expected hx-swap-oob attribute")
			}
			if tt.wantOOB && !strings.Contains(oob, tt.wantID) {
				t.Errorf("Expected hx-swap-oob to reference ID %q", tt.wantID)
			}

			// Check class
			class, _ := msg.Attr("class")
			if !strings.Contains(class, tt.wantClass) {
				t.Errorf("Expected class to contain %q, got %q", tt.wantClass, class)
			}

			// Check streaming class specifically
			if tt.data.IsStreaming && !strings.Contains(class, "streaming") {
				t.Errorf("Expected streaming class when IsStreaming=true")
			}
			if !tt.data.IsStreaming && strings.Contains(class, "streaming") {
				t.Errorf("Unexpected streaming class when IsStreaming=false")
			}
		})
	}
}

// Test that message metadata renders correctly
func TestMessageMetadata(t *testing.T) {
	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		data         MessageData
		wantMetadata bool
		wantText     string
	}{
		{
			name: "message with provider and model",
			data: MessageData{
				Alignment: "left",
				Text:      "Response",
				Provider:  "anthropic",
				Model:     "claude-3-5-sonnet",
			},
			wantMetadata: true,
			wantText:     "anthropic/claude-3-5-sonnet",
		},
		{
			name: "message without metadata",
			data: MessageData{
				Alignment: "right",
				Text:      "Question",
			},
			wantMetadata: false,
		},
		{
			name: "message with only provider",
			data: MessageData{
				Alignment: "left",
				Text:      "Response",
				Provider:  "openai",
			},
			wantMetadata: false, // Both provider AND model required
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := server.templates.ExecuteTemplate(&buf, "message", tt.data)
			if err != nil {
				t.Fatal(err)
			}

			doc, err := goquery.NewDocumentFromReader(&buf)
			if err != nil {
				t.Fatal(err)
			}

			meta := doc.Find(".message-meta")
			if tt.wantMetadata {
				if meta.Length() == 0 {
					t.Error("Expected message-meta element")
				}
				if !strings.Contains(meta.Text(), tt.wantText) {
					t.Errorf("Expected metadata text %q, got %q", tt.wantText, meta.Text())
				}
			} else {
				if meta.Length() > 0 {
					t.Error("Unexpected message-meta element")
				}
			}
		})
	}
}

// Test CSS streaming animation
func TestStreamingCSS(t *testing.T) {
	// Read CSS file and verify streaming animation exists
	cssBytes, err := staticFS.ReadFile("static/styles.css")
	if err != nil {
		t.Fatal(err)
	}

	css := string(cssBytes)

	// Check for streaming class
	if !strings.Contains(css, ".message.streaming") {
		t.Error("CSS should contain .message.streaming selector")
	}

	// Check for animation keyframes
	if !strings.Contains(css, "@keyframes dots") {
		t.Error("CSS should contain @keyframes dots animation")
	}

	// Check for ::after pseudo-element
	if !strings.Contains(css, ".message.streaming .message-bubble::after") {
		t.Error("CSS should style streaming message bubble ::after")
	}
}
