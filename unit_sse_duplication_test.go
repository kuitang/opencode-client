package main

import (
	"strings"
	"testing"
	"time"
)

func TestSSEMessagePartNoDuplication(t *testing.T) {
	// Test that message parts are not duplicated during SSE streaming
	manager := NewMessagePartsManager()

	// Simulate receiving the same part ID multiple times with updates
	messageID := "msg_test123"
	partID := "prt_test456"

	// First update - initial text
	part1 := MessagePartData{
		Type:    "text",
		Content: "I'll analyze",
	}
	err := manager.UpdatePart(messageID, partID, part1)
	if err != nil {
		t.Fatalf("Failed to update part: %v", err)
	}

	// Second update - expanded text (same partID)
	part2 := MessagePartData{
		Type:    "text",
		Content: "I'll analyze OSCR's stock",
	}
	err = manager.UpdatePart(messageID, partID, part2)
	if err != nil {
		t.Fatalf("Failed to update part: %v", err)
	}

	// Third update - complete text (same partID)
	part3 := MessagePartData{
		Type:    "text",
		Content: "I'll analyze OSCR's stock price over the last 6 months",
	}
	err = manager.UpdatePart(messageID, partID, part3)
	if err != nil {
		t.Fatalf("Failed to update part: %v", err)
	}

	// Verify only ONE part exists for this message
	parts := manager.GetParts(messageID)
	if len(parts) != 1 {
		t.Errorf("Expected exactly 1 part, got %d parts", len(parts))
		for i, p := range parts {
			t.Logf("Part %d: Type=%s, Content=%s", i, p.Type, p.Content)
		}
	}

	// Verify the content is the latest update
	if parts[0].Content != "I'll analyze OSCR's stock price over the last 6 months" {
		t.Errorf("Expected final content, got: %s", parts[0].Content)
	}

	// Verify the partID is preserved
	if parts[0].PartID != partID {
		t.Errorf("Expected partID %s, got %s", partID, parts[0].PartID)
	}
}

func TestSSEMultiplePartTypes(t *testing.T) {
	// Test handling of multiple part types in sequence
	manager := NewMessagePartsManager()
	messageID := "msg_test789"

	// Add text part
	textPart := MessagePartData{
		Type:    "text",
		Content: "Analyzing data",
	}
	manager.UpdatePart(messageID, "prt_text1", textPart)

	// Add tool part
	toolPart := MessagePartData{
		Type:    "tool",
		Content: "Tool: webfetch\nStatus: running",
	}
	manager.UpdatePart(messageID, "prt_tool1", toolPart)

	// Update tool part (same ID)
	toolPartUpdated := MessagePartData{
		Type:    "tool",
		Content: "Tool: webfetch\nStatus: completed\nOutput: ...",
	}
	manager.UpdatePart(messageID, "prt_tool1", toolPartUpdated)

	// Add another text part
	textPart2 := MessagePartData{
		Type:    "text",
		Content: "The analysis shows...",
	}
	manager.UpdatePart(messageID, "prt_text2", textPart2)

	// Verify correct number of parts (3 total, not 4)
	parts := manager.GetParts(messageID)
	if len(parts) != 3 {
		t.Errorf("Expected 3 parts, got %d", len(parts))
	}

	// Verify order is maintained
	expectedOrder := []string{"prt_text1", "prt_tool1", "prt_text2"}
	for i, part := range parts {
		if part.PartID != expectedOrder[i] {
			t.Errorf("Part %d: expected ID %s, got %s", i, expectedOrder[i], part.PartID)
		}
	}

	// Verify tool part was updated, not duplicated
	if parts[1].Content != "Tool: webfetch\nStatus: completed\nOutput: ..." {
		t.Errorf("Tool part not updated correctly: %s", parts[1].Content)
	}
}

func TestSSEHTMLGenerationNoDuplication(t *testing.T) {
	// Test that HTML generation doesn't create duplicate elements
	manager := NewMessagePartsManager()
	messageID := "msg_html_test"

	// Simulate incremental text updates
	updates := []string{
		"The",
		"The file",
		"The file does",
		"The file does not",
		"The file does not exist",
	}

	partID := "prt_incremental"
	for _, content := range updates {
		part := MessagePartData{
			Type:    "text",
			Content: content,
		}
		manager.UpdatePart(messageID, partID, part)
	}

	// Get final parts
	parts := manager.GetParts(messageID)

	// Should have exactly 1 part
	if len(parts) != 1 {
		t.Errorf("Expected 1 part after incremental updates, got %d", len(parts))
	}

	// Render the part
	part := parts[0]
	part.RenderedHTML = renderText(part.Content)

	// Check that rendered HTML doesn't contain duplicates
	htmlStr := string(part.RenderedHTML)

	// Count occurrences of the final text - should appear exactly once
	count := strings.Count(htmlStr, "The file does not exist")
	if count != 1 {
		t.Errorf("Text appears %d times in rendered HTML, expected 1", count)
		t.Logf("Rendered HTML: %s", htmlStr)
	}
}

func TestSSERapidUpdates(t *testing.T) {
	// Test rapid updates don't cause race conditions or duplicates
	manager := NewMessagePartsManager()
	messageID := "msg_rapid"
	partID := "prt_rapid"

	// Simulate rapid updates
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			part := MessagePartData{
				Type:    "text",
				Content: strings.Repeat("a", i+1),
			}
			manager.UpdatePart(messageID, partID, part)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for updates to complete
	<-done

	// Verify only one part exists
	parts := manager.GetParts(messageID)
	if len(parts) != 1 {
		t.Errorf("Expected 1 part after rapid updates, got %d", len(parts))
	}

	// Verify final content is correct
	expectedLen := 100
	if len(parts[0].Content) != expectedLen {
		t.Errorf("Expected content length %d, got %d", expectedLen, len(parts[0].Content))
	}
}
