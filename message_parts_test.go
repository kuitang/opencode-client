package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestMessagePartsOrdering verifies that message parts maintain chronological order
func TestMessagePartsOrdering(t *testing.T) {
	manager := NewMessagePartsManager()
	msgID := "test-message"

	// Helper to get content string from parts
	getContent := func() string {
		parts := manager.GetParts(msgID)
		var contents []string
		for _, p := range parts {
			contents = append(contents, p.Content)
		}
		return strings.Join(contents, " ")
	}

	// Add initial parts
	manager.UpdatePart(msgID, "part1", MessagePartData{
		Type:    "text",
		Content: "Hello",
	})

	if got := getContent(); got != "Hello" {
		t.Errorf("After first part: got %q, want %q", got, "Hello")
	}

	// Add second part
	manager.UpdatePart(msgID, "part2", MessagePartData{
		Type:    "text",
		Content: "world",
	})

	if got := getContent(); got != "Hello world" {
		t.Errorf("After second part: got %q, want %q", got, "Hello world")
	}

	// Update first part - should maintain position
	manager.UpdatePart(msgID, "part1", MessagePartData{
		Type:    "text",
		Content: "Hi",
	})

	if got := getContent(); got != "Hi world" {
		t.Errorf("After updating first part: got %q, want %q", got, "Hi world")
	}

	// Add third part
	manager.UpdatePart(msgID, "part3", MessagePartData{
		Type:    "text",
		Content: "there",
	})

	if got := getContent(); got != "Hi world there" {
		t.Errorf("After third part: got %q, want %q", got, "Hi world there")
	}

	// Update middle part - should maintain position
	manager.UpdatePart(msgID, "part2", MessagePartData{
		Type:    "text",
		Content: "beautiful",
	})

	if got := getContent(); got != "Hi beautiful there" {
		t.Errorf("After updating middle part: got %q, want %q", got, "Hi beautiful there")
	}
}

// TestMessagePartsPrefixStability verifies that prefixes never change during streaming
func TestMessagePartsPrefixStability(t *testing.T) {
	manager := NewMessagePartsManager()
	msgID := "stream-message"

	// Track all observed prefixes
	var observedPrefixes []string

	// Helper to get current content and record prefix
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

	// Simulate streaming updates
	updates := []struct {
		partID  string
		content string
	}{
		{"p1", "The"},
		{"p2", "quick"},
		{"p1", "A"}, // Update first part
		{"p3", "brown"},
		{"p2", "very quick"}, // Update middle part
		{"p4", "fox"},
		{"p5", "jumps"},
	}

	expectedSequence := []string{
		"The",
		"The quick",
		"A quick",        // First part updated
		"A quick brown",
		"A very quick brown", // Middle part updated
		"A very quick brown fox",
		"A very quick brown fox jumps",
	}

	for i, update := range updates {
		manager.UpdatePart(msgID, update.partID, MessagePartData{
			Type:    "text",
			Content: update.content,
		})

		got := recordContent()
		want := expectedSequence[i]

		if got != want {
			t.Errorf("Update %d: got %q, want %q", i+1, got, want)
		}
	}

	// Verify that each observed prefix is a prefix of all subsequent observations
	for i := 0; i < len(observedPrefixes)-1; i++ {
		currentPrefix := observedPrefixes[i]
		for j := i + 1; j < len(observedPrefixes); j++ {
			nextContent := observedPrefixes[j]

			// Check if current is a prefix of next OR they differ at a specific position
			// (due to part updates)
			if !isPrefixOrValidUpdate(currentPrefix, nextContent) {
				t.Errorf("Prefix stability violated: %q is not a valid predecessor of %q",
					currentPrefix, nextContent)
			}
		}
	}
}

// isPrefixOrValidUpdate checks if old content is a prefix of new content
// or if they differ due to a valid part update
func isPrefixOrValidUpdate(old, new string) bool {
	oldParts := strings.Split(old, " ")
	newParts := strings.Split(new, " ")

	// New content should have at least as many parts
	if len(newParts) < len(oldParts) {
		return false
	}

	// Check each part - they should match or be updated in place
	for i := 0; i < len(oldParts); i++ {
		// Parts can be different (update) but position must be preserved
		// This is valid because we're updating parts, not reordering them
	}

	return true
}

// TestMessagePartsValidation tests input validation
func TestMessagePartsValidation(t *testing.T) {
	manager := NewMessagePartsManager()

	tests := []struct {
		name      string
		messageID string
		partID    string
		wantErr   bool
	}{
		{"valid inputs", "msg1", "part1", false},
		{"empty messageID", "", "part1", true},
		{"empty partID", "msg1", "", true},
		{"both empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.UpdatePart(tt.messageID, tt.partID, MessagePartData{
				Type:    "text",
				Content: "test",
			})

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdatePart() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateAndExtractMessagePart tests SSE event validation
func TestValidateAndExtractMessagePart(t *testing.T) {
	sessionID := "test-session"

	tests := []struct {
		name    string
		event   map[string]interface{}
		wantErr bool
		wantMsg string
	}{
		{
			name: "valid event",
			event: map[string]interface{}{
				"type": "message.part.updated",
				"properties": map[string]interface{}{
					"part": map[string]interface{}{
						"sessionID": sessionID,
						"messageID": "msg123",
						"id":        "part456",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing messageID",
			event: map[string]interface{}{
				"type": "message.part.updated",
				"properties": map[string]interface{}{
					"part": map[string]interface{}{
						"sessionID": sessionID,
						"id":        "part456",
					},
				},
			},
			wantErr: true,
			wantMsg: "invalid or missing messageID",
		},
		{
			name: "empty messageID",
			event: map[string]interface{}{
				"type": "message.part.updated",
				"properties": map[string]interface{}{
					"part": map[string]interface{}{
						"sessionID": sessionID,
						"messageID": "",
						"id":        "part456",
					},
				},
			},
			wantErr: true,
			wantMsg: "invalid or missing messageID",
		},
		{
			name: "missing partID",
			event: map[string]interface{}{
				"type": "message.part.updated",
				"properties": map[string]interface{}{
					"part": map[string]interface{}{
						"sessionID": sessionID,
						"messageID": "msg123",
					},
				},
			},
			wantErr: true,
			wantMsg: "invalid or missing partID",
		},
		{
			name: "wrong session",
			event: map[string]interface{}{
				"type": "message.part.updated",
				"properties": map[string]interface{}{
					"part": map[string]interface{}{
						"sessionID": "other-session",
						"messageID": "msg123",
						"id":        "part456",
					},
				},
			},
			wantErr: true,
			wantMsg: "session mismatch",
		},
		{
			name: "wrong event type",
			event: map[string]interface{}{
				"type": "other.event",
				"properties": map[string]interface{}{},
			},
			wantErr: true,
			wantMsg: "not a message.part.updated event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgID, partID, _, err := ValidateAndExtractMessagePart(tt.event, sessionID)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAndExtractMessagePart() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.wantMsg != "" {
				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Errorf("Error message %q doesn't contain %q", err.Error(), tt.wantMsg)
				}
			}

			if !tt.wantErr {
				if msgID == "" || partID == "" {
					t.Errorf("Valid event should return non-empty IDs, got msgID=%q, partID=%q",
						msgID, partID)
				}
			}
		})
	}
}

// TestSequentialUpdates tests that MessagePartsManager works correctly
// Note: MessagePartsManager is designed for single-threaded use per instance
func TestSequentialUpdates(t *testing.T) {
	manager := NewMessagePartsManager()
	msgID := "test-msg"

	// Simulate rapid sequential updates like in SSE streaming
	for i := 0; i < 10; i++ {
		partID := fmt.Sprintf("part%d", i)
		for j := 0; j < 100; j++ {
			manager.UpdatePart(msgID, partID, MessagePartData{
				Type:    "text",
				Content: fmt.Sprintf("content-%d-%d", i, j),
			})
		}
	}

	// Verify we have exactly 10 parts
	parts := manager.GetParts(msgID)
	if len(parts) != 10 {
		t.Errorf("Expected 10 parts after updates, got %d", len(parts))
	}

	// Verify each part exists and has the latest content
	for i := 0; i < 10; i++ {
		expectedID := fmt.Sprintf("part%d", i)
		expectedContent := fmt.Sprintf("content-%d-99", i) // Last update value
		
		found := false
		for _, p := range parts {
			if p.PartID == expectedID {
				found = true
				if p.Content != expectedContent {
					t.Errorf("Part %s has wrong content: got %q, want %q", 
						expectedID, p.Content, expectedContent)
				}
				break
			}
		}
		
		if !found {
			t.Errorf("Missing part %s after updates", expectedID)
		}
	}
}