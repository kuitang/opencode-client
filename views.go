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
	Type        string
	Content     string
	RenderedHTML template.HTML  // Used for text parts only
	PartID      string // To identify updates to same part
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
		"<ul", "<ol", "<li",                        // Lists  
		"<blockquote",                              // Blockquotes
		"<pre", "<code",                            // Code blocks/inline
		"<table",                                   // Tables
		"<strong", "<em",                           // Bold/italic
		"<hr",                                      // Horizontal rules
		"<a href=",                                  // Links (from autolink or markdown)
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
			blackfriday.CommonExtensions | blackfriday.Autolink))
	
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
	return template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
}

// renderMessage renders a message using the provided templates
func renderMessage(templates *template.Template, msg MessageData) (string, error) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "message", msg); err != nil {
		return "", err
	}
	return buf.String(), nil
}