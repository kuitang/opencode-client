package views

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"

	"opencode-chat/internal/models"
	"opencode-chat/internal/templates"
)

// MessagePartData holds transformed message part data for rendering.
type MessagePartData struct {
	Type         string
	Content      string
	RenderedHTML template.HTML // Used for text parts only
	PartID       string       // To identify updates to same part
}

// TodoItem represents a single todo item from todowrite tool.
type TodoItem struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	ID       string `json:"id"`
}

// ToolData represents data passed to the tool template.
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

// MessageData holds data for rendering a complete message.
type MessageData struct {
	ID          string
	Alignment   string
	Text        string
	Parts       []MessagePartData
	Provider    string
	Model       string
	IsStreaming bool
	HXSwapOOB  bool
}

// LoadTemplates loads and parses all templates from the embedded filesystem.
func LoadTemplates() (*template.Template, error) {
	// Pre-declare renderComponent variable for closure
	var renderComponent func(string, any) template.HTML

	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"dict": func(values ...any) map[string]any {
			if len(values)%2 != 0 {
				panic("dict must have even number of arguments")
			}
			dict := make(map[string]any)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					panic("dict keys must be strings")
				}
				dict[key] = values[i+1]
			}
			return dict
		},
		"renderComponent": func(templateName string, data any) template.HTML {
			return renderComponent(templateName, data)
		},
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templates.TemplateFS, "templates/*.html", "templates/tabs/*.html")
	if err != nil {
		return nil, err
	}

	// Now set the actual renderComponent function that can access tmpl
	renderComponent = func(templateName string, data any) template.HTML {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, templateName, data); err != nil {
			return template.HTML(fmt.Sprintf("<!-- Error rendering %s: %v -->", templateName, err))
		}
		return template.HTML(buf.String())
	}

	return tmpl, nil
}

// RenderText converts text to HTML with markdown support and autolink, then sanitizes it.
func RenderText(text string) template.HTML {
	html := blackfriday.Run([]byte(text),
		blackfriday.WithExtensions(
			blackfriday.CommonExtensions|blackfriday.Autolink))

	policy := bluemonday.UGCPolicy()
	safeHTML := policy.SanitizeBytes(html)

	if HasMarkdownElements(html) {
		return template.HTML(safeHTML)
	}
	return template.HTML(`<div class="preserve-breaks">` + string(safeHTML) + `</div>`)
}

// HasMarkdownElements checks if the rendered HTML contains actual markdown elements
// beyond just a simple paragraph wrapper.
func HasMarkdownElements(html []byte) bool {
	htmlStr := string(html)

	markdownIndicators := []string{
		"<h1", "<h2", "<h3", "<h4", "<h5", "<h6",
		"<ul", "<ol", "<li",
		"<blockquote",
		"<pre", "<code",
		"<table",
		"<strong", "<em",
		"<hr",
		"<a href=",
	}

	for _, indicator := range markdownIndicators {
		if strings.Contains(htmlStr, indicator) {
			return true
		}
	}

	pCount := strings.Count(htmlStr, "<p>")
	if pCount > 1 {
		return true
	}

	return false
}

// RenderMessage renders a message using the provided templates.
func RenderMessage(tmpl *template.Template, msg MessageData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "message", msg); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderTodoList renders todowrite tool output using templates.
func RenderTodoList(tmpl *template.Template, output string) (template.HTML, error) {
	var todos []TodoItem
	if err := json.Unmarshal([]byte(output), &todos); err != nil {
		escapedOutput := template.HTMLEscapeString(output)
		return template.HTML(`<pre class="overflow-x-auto">` + escapedOutput + `</pre>`), nil
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "todo", todos); err != nil {
		return "", fmt.Errorf("failed to execute todo template: %w", err)
	}

	return template.HTML(buf.String()), nil
}

// RenderToolDetails generates HTML with collapsible details for tool output using a template.
func RenderToolDetails(tmpl *template.Template, toolName, status string, input map[string]any, output string) template.HTML {
	toolData := ToolData{
		Name:   strings.ToLower(toolName),
		Status: status,
		Output: output,
	}

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
			toolData.Filename = file
		}
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
		if todoHTML, err := RenderTodoList(tmpl, output); err == nil {
			toolData.TodoHTML = todoHTML
		}

	default:
		if len(input) > 0 {
			toolData.ShowInput = true
			if inputJSON, err := json.MarshalIndent(input, "", "  "); err == nil {
				toolData.InputJSON = string(inputJSON)
			}
		}
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "tool", toolData); err != nil {
		log.Printf("Failed to execute tool template: %v", err)
		return template.HTML(fmt.Sprintf("<div class='my-2 p-2 bg-gray-100 rounded'>Tool: %s (Status: %s)</div>",
			template.HTMLEscapeString(toolName),
			template.HTMLEscapeString(status)))
	}

	return template.HTML(buf.String())
}

// ParseRawMessagePart converts raw map data from SSE events into a MessagePart struct.
func ParseRawMessagePart(partID string, partData map[string]any) models.MessagePart {
	msgPart := models.MessagePart{
		ID:   partID,
		Type: partData["type"].(string),
	}

	switch msgPart.Type {
	case "text", "reasoning":
		if text, ok := partData["text"].(string); ok {
			msgPart.Text = text
		}
	case "tool":
		if toolName, ok := partData["tool"].(string); ok {
			msgPart.Tool = toolName
		}
		if state, ok := partData["state"].(map[string]any); ok {
			msgPart.State = state
		}
	case "file", "snapshot", "patch", "agent":
		if state, ok := partData["state"].(map[string]any); ok {
			msgPart.State = state
		}
	}

	return msgPart
}

// TransformMessagePart transforms a MessagePart from OpenCode API to MessagePartData with proper rendering.
func TransformMessagePart(tmpl *template.Template, part models.MessagePart) MessagePartData {
	switch part.Type {
	case "text":
		renderedHTML := RenderText(part.Text)
		return MessagePartData{
			Type:         "text",
			Content:      part.Text,
			RenderedHTML: renderedHTML,
			PartID:       part.ID,
		}

	case "tool":
		status, _ := part.State["status"].(string)
		input, _ := part.State["input"].(map[string]any)
		output, _ := part.State["output"].(string)

		renderedHTML := RenderToolDetails(tmpl, part.Tool, status, input, output)

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
		renderedHTML := RenderText(reasoningText)
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
		return MessagePartData{
			Type:    part.Type,
			Content: part.Text,
			PartID:  part.ID,
		}
	}
}
