package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"
)

type MessagePartData struct {
	Type         string
	Content      string
	RenderedHTML template.HTML // Used for text parts only
	PartID       string        // To identify updates to same part
}

// TodoItem represents a single todo item from todowrite tool
type TodoItem struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	ID       string `json:"id"`
}

// ToolData represents data passed to the tool template
type ToolData struct {
	Name        string
	Status      string
	Output      string
	Command     string        // For bash
	Filename    string        // For read/write/edit
	Pattern     string        // For grep/glob
	Description string        // For task
	Content     string        // For write
	InputJSON   string        // For generic tools
	ShowInput   bool          // Whether to show input section
	TodoHTML    template.HTML // For todowrite
}

type MessageData struct {
	ID          string
	Alignment   string
	Text        string
	Parts       []MessagePartData
	Provider    string
	Model       string
	IsStreaming bool
	HXSwapOOB   bool
}

// renderTodoList renders todowrite tool output using templates
func renderTodoList(templates *template.Template, output string) (template.HTML, error) {
	var todos []TodoItem
	if err := json.Unmarshal([]byte(output), &todos); err != nil {
		// If parsing fails, return plain text in pre tag
		escapedOutput := template.HTMLEscapeString(output)
		return template.HTML(`<pre class="overflow-x-auto">` + escapedOutput + `</pre>`), nil
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "todo", todos); err != nil {
		return "", fmt.Errorf("failed to execute todo template: %w", err)
	}

	return template.HTML(buf.String()), nil
}

// renderToolDetails generates HTML with collapsible details for tool output using a template
func renderToolDetails(templates *template.Template, toolName, status string, input map[string]interface{}, output string) template.HTML {
	toolData := ToolData{
		Name:   strings.ToLower(toolName),
		Status: status,
		Output: output,
	}

	// Extract specific info based on tool type
	switch strings.ToLower(toolName) {
	case "bash":
		if cmd, ok := input["command"].(string); ok {
			toolData.Command = cmd
		}

	case "write", "read", "edit", "multiedit":
		if path, ok := input["path"].(string); ok {
			toolData.Filename = path
		} else if file, ok := input["file_path"].(string); ok {
			toolData.Filename = file
		} else if file, ok := input["filePath"].(string); ok {
			// Handle camelCase variant from some tools
			toolData.Filename = file
		}
		// For write, also get content
		if strings.ToLower(toolName) == "write" {
			if content, ok := input["content"].(string); ok {
				toolData.Content = content
			}
		}

	case "grep":
		if pattern, ok := input["pattern"].(string); ok {
			toolData.Pattern = pattern
		}

	case "glob":
		if pattern, ok := input["pattern"].(string); ok {
			toolData.Pattern = pattern
		}

	case "task":
		if desc, ok := input["description"].(string); ok {
			toolData.Description = desc
		}

	case "todowrite":
		// Special handling for todowrite
		if todoHTML, err := renderTodoList(templates, output); err == nil {
			toolData.TodoHTML = todoHTML
		}

	default:
		// For generic tools, show input as JSON if available
		if len(input) > 0 {
			toolData.ShowInput = true
			if inputJSON, err := json.MarshalIndent(input, "", "  "); err == nil {
				toolData.InputJSON = string(inputJSON)
			}
		}
	}

	// Execute the template
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "tool", toolData); err != nil {
		log.Printf("Failed to execute tool template: %v", err)
		// Fallback to simple rendering
		return template.HTML(fmt.Sprintf("<div class='my-2 p-2 bg-gray-100 rounded'>Tool: %s (Status: %s)</div>",
			template.HTMLEscapeString(toolName),
			template.HTMLEscapeString(status)))
	}

	return template.HTML(buf.String())
}

// hasMarkdownElements checks if the rendered HTML contains actual markdown elements
// beyond just a simple paragraph wrapper
func hasMarkdownElements(html []byte) bool {
	htmlStr := string(html)

	// Check for markdown-specific elements
	// If it's just plain text, Blackfriday wraps it in a single <p> tag
	// If it has markdown, we'll see other elements
	markdownIndicators := []string{
		"<h1", "<h2", "<h3", "<h4", "<h5", "<h6", // Headers
		"<ul", "<ol", "<li", // Lists
		"<blockquote",   // Blockquotes
		"<pre", "<code", // Code blocks/inline
		"<table",         // Tables
		"<strong", "<em", // Bold/italic
		"<hr",      // Horizontal rules
		"<a href=", // Links (from autolink or markdown)
	}

	for _, indicator := range markdownIndicators {
		if strings.Contains(htmlStr, indicator) {
			return true
		}
	}

	// Check if there are multiple paragraphs (indicates markdown line breaks)
	pCount := strings.Count(htmlStr, "<p>")
	if pCount > 1 {
		return true
	}

	return false
}

// renderText converts text to HTML with markdown support and autolink, then sanitizes it
// Used for all text content (user input and LLM output) except tool/reasoning blocks
func renderText(text string) template.HTML {
	// Render with markdown and autolink extensions
	// This works for both markdown AND plain text
	html := blackfriday.Run([]byte(text),
		blackfriday.WithExtensions(
			blackfriday.CommonExtensions|blackfriday.Autolink))

	// Sanitize HTML to prevent XSS
	// UGCPolicy is designed for user-generated content
	// It allows formatting tags but removes dangerous elements like <script>
	policy := bluemonday.UGCPolicy()
	safeHTML := policy.SanitizeBytes(html)

	// Check if actual markdown was rendered
	if hasMarkdownElements(html) {
		// Markdown was rendered - use normal spacing
		return template.HTML(safeHTML)
	} else {
		// Plain text - add class to preserve line breaks
		// Wrap the content in a div with preserve-breaks class
		return template.HTML(`<div class="preserve-breaks">` + string(safeHTML) + `</div>`)
	}
}

// loadTemplates loads and parses all templates from the embedded filesystem
func loadTemplates() (*template.Template, error) {
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}
	return template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html", "templates/tabs/*.html")
}

// renderMessage renders a message using the provided templates
func renderMessage(templates *template.Template, msg MessageData) (string, error) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "message", msg); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// hasVisibleContent checks if a message part has visible content
func hasVisibleContent(part MessagePart) bool {
	if part.Type == "text" && part.Text != "" {
		return true
	}
	return part.Type != ""
}

// parseRawMessagePart converts raw map data from SSE events into a MessagePart struct
func parseRawMessagePart(partID string, partData map[string]interface{}) MessagePart {
	msgPart := MessagePart{
		ID:   partID,
		Type: partData["type"].(string),
	}

	// Extract fields based on part type
	switch msgPart.Type {
	case "text", "reasoning":
		if text, ok := partData["text"].(string); ok {
			msgPart.Text = text
		}
	case "tool":
		if toolName, ok := partData["tool"].(string); ok {
			msgPart.Tool = toolName
		}
		if state, ok := partData["state"].(map[string]interface{}); ok {
			msgPart.State = state
		}
	case "file", "snapshot", "patch", "agent":
		// These types may have state data
		if state, ok := partData["state"].(map[string]interface{}); ok {
			msgPart.State = state
		}
	}

	return msgPart
}

// transformMessagePart transforms a MessagePart from OpenCode API to MessagePartData with proper rendering
func transformMessagePart(templates *template.Template, part MessagePart) MessagePartData {
	switch part.Type {
	case "text":
		renderedHTML := renderText(part.Text)
		return MessagePartData{
			Type:         "text",
			Content:      part.Text,
			RenderedHTML: renderedHTML,
			PartID:       part.ID,
		}

	case "tool":
		status, _ := part.State["status"].(string)
		input, _ := part.State["input"].(map[string]interface{})
		output, _ := part.State["output"].(string)

		renderedHTML := renderToolDetails(templates, part.Tool, status, input, output)

		// Create text fallback
		var toolContent strings.Builder
		toolContent.WriteString(fmt.Sprintf("Tool: %s (Status: %s)", part.Tool, status))
		if len(input) > 0 {
			toolContent.WriteString("\nInput: ")
			for key, value := range input {
				toolContent.WriteString(fmt.Sprintf("%s=%v ", key, value))
			}
		}
		if output != "" {
			toolContent.WriteString("\nOutput:\n" + output)
		}

		return MessagePartData{
			Type:         "tool",
			Content:      toolContent.String(),
			RenderedHTML: renderedHTML,
			PartID:       part.ID,
		}

	case "reasoning":
		reasoningText := fmt.Sprintf("🤔 Reasoning:\n%s", part.Text)
		renderedHTML := renderText(reasoningText)
		return MessagePartData{
			Type:         "reasoning",
			Content:      reasoningText,
			RenderedHTML: renderedHTML,
			PartID:       part.ID,
		}

	case "step-start":
		badgeHTML := template.HTML(`<div class="flex items-center gap-2 px-3 py-1 bg-yellow-100 text-yellow-800 rounded-full text-sm my-2 w-fit">
			<span>▶️</span>
			<span>Step started</span>
		</div>`)
		return MessagePartData{
			Type:         "step-start",
			Content:      "▶️ Step started",
			RenderedHTML: badgeHTML,
			PartID:       part.ID,
		}

	case "step-finish":
		badgeHTML := template.HTML(`<div class="flex items-center gap-2 px-3 py-1 bg-green-100 text-green-800 rounded-full text-sm my-2 w-fit">
			<span>✅</span>
			<span>Step completed</span>
		</div>`)
		return MessagePartData{
			Type:         "step-finish",
			Content:      "✅ Step completed",
			RenderedHTML: badgeHTML,
			PartID:       part.ID,
		}

	case "file":
		filename, _ := part.State["filename"].(string)
		url, _ := part.State["url"].(string)
		return MessagePartData{
			Type:    "file",
			Content: fmt.Sprintf("📁 File: %s\nURL: %s", filename, url),
			PartID:  part.ID,
		}

	case "snapshot":
		return MessagePartData{
			Type:    "snapshot",
			Content: "📸 Snapshot taken",
			PartID:  part.ID,
		}

	case "patch":
		return MessagePartData{
			Type:    "patch",
			Content: "🔧 Code patch applied",
			PartID:  part.ID,
		}

	case "agent":
		return MessagePartData{
			Type:    "agent",
			Content: "🤖 Agent action",
			PartID:  part.ID,
		}

	default:
		// Handle unknown part types
		return MessagePartData{
			Type:    part.Type,
			Content: part.Text,
			PartID:  part.ID,
		}
	}
}
