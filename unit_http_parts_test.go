package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// convertToMessagePart converts test EnhancedMessagePart to MessagePart for testing
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
		// Convert string time fields to int64 (if needed, can be 0 for test purposes)
		Time: struct {
			Start int64 `json:"start,omitempty"`
			End   int64 `json:"end,omitempty"`
		}{
			Start: 0, // Test data doesn't need real timestamps
			End:   0,
		},
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
	// For simplicity in tests, just use a fixed format
	return strings.TrimRight(strings.TrimRight(sprintf("%.6f", f), "0"), ".")
}

func sprintf(format string, args ...interface{}) string {
	// Simple sprintf implementation for test
	if format == "%.6f" && len(args) == 1 {
		if f, ok := args[0].(float64); ok {
			// Convert to string with 6 decimal places
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

	part := EnhancedMessagePart{
		ID:        "prt_001",
		MessageID: "msg_001",
		SessionID: "ses_001",
		Type:      "text",
		Text:      "Hello **world**! Check out https://example.com",
	}

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

	// Check that markdown and autolink were applied
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

	part := EnhancedMessagePart{
		ID:        "prt_002",
		MessageID: "msg_001",
		SessionID: "ses_001",
		Type:      "tool",
		Tool:      "bash",
		State: map[string]interface{}{
			"status": "completed",
			"input": map[string]interface{}{
				"command": "ls -la",
			},
			"output": "total 24\ndrwxr-xr-x  3 user user 4096 Jan 1 00:00 .",
		},
	}

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

	// Check HTML rendering
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

	part := EnhancedMessagePart{
		ID:        "prt_003",
		MessageID: "msg_001",
		SessionID: "ses_001",
		Type:      "tool",
		Tool:      "todowrite",
		State: map[string]interface{}{
			"status": "completed",
			"input":  map[string]interface{}{},
			"output": todoJSON,
		},
	}

	result := transformMessagePart(templates, convertToMessagePart(part))

	if result.Type != "tool" {
		t.Errorf("Expected type 'tool', got %s", result.Type)
	}

	// Check that todo list was rendered
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

	part := EnhancedMessagePart{
		ID:        "prt_004",
		MessageID: "msg_001",
		SessionID: "ses_001",
		Type:      "reasoning",
		Text:      "I need to analyze this problem step by step...",
	}

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

	// Test step-start
	startPart := EnhancedMessagePart{
		ID:        "prt_005",
		MessageID: "msg_001",
		SessionID: "ses_001",
		Type:      "step-start",
	}

	startResult := transformMessagePart(templates, convertToMessagePart(startPart))

	if startResult.Type != "step-start" {
		t.Errorf("Expected type 'step-start', got %s", startResult.Type)
	}

	if !strings.Contains(startResult.Content, "▶️") {
		t.Errorf("Expected step-start emoji, got %s", startResult.Content)
	}

	// Check HTML badge
	htmlStr := string(startResult.RenderedHTML)
	if !strings.Contains(htmlStr, "bg-yellow-100") {
		t.Errorf("Expected yellow badge styling, got %s", htmlStr)
	}

	// Test step-finish
	finishPart := EnhancedMessagePart{
		ID:        "prt_006",
		MessageID: "msg_001",
		SessionID: "ses_001",
		Type:      "step-finish",
	}

	finishResult := transformMessagePart(templates, convertToMessagePart(finishPart))

	if finishResult.Type != "step-finish" {
		t.Errorf("Expected type 'step-finish', got %s", finishResult.Type)
	}

	if !strings.Contains(finishResult.Content, "✅") {
		t.Errorf("Expected step-finish emoji, got %s", finishResult.Content)
	}

	// Check HTML badge
	htmlStr = string(finishResult.RenderedHTML)
	if !strings.Contains(htmlStr, "bg-green-100") {
		t.Errorf("Expected green badge styling, got %s", htmlStr)
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
		{
			ID:   "prt_001",
			Type: "step-start",
		},
		{
			ID:   "prt_002",
			Type: "text",
			Text: "Let me help you with that.",
		},
		{
			ID:   "prt_003",
			Type: "reasoning",
			Text: "Analyzing the request...",
		},
		{
			ID:   "prt_004",
			Type: "tool",
			Tool: "bash",
			State: map[string]interface{}{
				"status": "completed",
				"input": map[string]interface{}{
					"command": "echo 'Hello'",
				},
				"output": "Hello",
			},
		},
		{
			ID:   "prt_005",
			Type: "text",
			Text: "The command executed successfully.",
		},
		{
			ID:   "prt_006",
			Type: "step-finish",
		},
	}

	// Transform all parts
	var transformedParts []MessagePartData
	for _, part := range message.Parts {
		transformedParts = append(transformedParts, transformMessagePart(templates, convertToMessagePart(part)))
	}

	// Verify we have all parts
	if len(transformedParts) != 6 {
		t.Errorf("Expected 6 parts, got %d", len(transformedParts))
	}

	// Check each part type is present
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
