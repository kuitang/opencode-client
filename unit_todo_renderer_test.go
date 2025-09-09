package main

import (
	"strings"
	"testing"
)

func TestRenderTodoList(t *testing.T) {
	// Load templates directly
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected []string // strings that should be present in output
	}{
		{
			name: "Simple todo list",
			input: `[
				{
					"content": "Write tests",
					"status": "pending",
					"priority": "high",
					"id": "1"
				},
				{
					"content": "Fix bugs",
					"status": "in_progress",
					"priority": "medium",
					"id": "2"
				},
				{
					"content": "Deploy code",
					"status": "completed",
					"priority": "low",
					"id": "3"
				}
			]`,
			expected: []string{
				"☐", // pending checkbox
				"⏳", // in progress checkbox  
				"✓", // completed checkbox
				"Write tests",
				"Fix bugs", 
				"Deploy code",
				"text-red-600", // high priority
				"text-yellow-600", // medium priority
				"text-gray-400", // low priority
				"line-through", // completed item styling
			},
		},
		{
			name: "Invalid JSON fallback",
			input: `not valid json`,
			expected: []string{
				"<pre class=\"overflow-x-auto\">",
				"not valid json",
			},
		},
		{
			name: "Empty array",
			input: `[]`,
			expected: []string{
				"<div class=\"todo-list text-sm\">",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := renderTodoList(templates, tt.input)
			if err != nil {
				t.Fatalf("renderTodoList failed: %v", err)
			}
			
			resultStr := string(result)
			for _, expected := range tt.expected {
				if !strings.Contains(resultStr, expected) {
					t.Errorf("Expected output to contain %q, but it didn't.\nFull output:\n%s", expected, resultStr)
				}
			}
		})
	}
}

func TestTodoItemStatuses(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	input := `[
		{"content": "Pending task", "status": "pending", "priority": "high", "id": "1"},
		{"content": "In progress task", "status": "in_progress", "priority": "medium", "id": "2"}, 
		{"content": "Completed task", "status": "completed", "priority": "low", "id": "3"}
	]`
	
	result, err := renderTodoList(templates, input)
	if err != nil {
		t.Fatalf("renderTodoList failed: %v", err)
	}
	
	resultStr := string(result)
	
	// Check that pending items have empty checkbox
	if !strings.Contains(resultStr, "☐") {
		t.Error("Should contain pending checkbox ☐")
	}
	
	// Check that in_progress items have hourglass
	if !strings.Contains(resultStr, "⏳") {
		t.Error("Should contain in_progress checkbox ⏳")
	}
	
	// Check that completed items have checkmark
	if !strings.Contains(resultStr, "✓") {
		t.Error("Should contain completed checkbox ✓")
	}
	
	// Check that completed items have strikethrough
	if !strings.Contains(resultStr, "line-through") {
		t.Error("Should contain line-through styling for completed items")
	}
}