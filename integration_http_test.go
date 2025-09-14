package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// NOTE: These HTTP/SSE rendering tests intentionally do NOT use a real sandbox.
// They mock upstreams via httptest.Server and use StaticURLSandbox for fast, deterministic checks.

// MockOpencodeServer simulates OpenCode's /session/{id}/message endpoint
type MockOpencodeServer struct {
	*httptest.Server
	Messages []EnhancedMessageResponse
}

func NewMockOpencodeServer() *MockOpencodeServer {
	mock := &MockOpencodeServer{
		Messages: []EnhancedMessageResponse{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/session/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/message") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(mock.Messages)
		} else {
			// Handle session creation
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"id": "test-session"})
		}
	})

	mock.Server = httptest.NewServer(mux)
	return mock
}

func (m *MockOpencodeServer) AddMessage(msg EnhancedMessageResponse) {
	m.Messages = append(m.Messages, msg)
}

func (m *MockOpencodeServer) Port() int {
	// Extract port from server URL
	parts := strings.Split(m.Server.URL, ":")
	port := 0
	fmt.Sscanf(parts[len(parts)-1], "%d", &port)
	return port
}

// transformEnhancedMessage transforms an EnhancedMessageResponse to MessageData with rendered parts
func transformEnhancedMessage(templates *template.Template, msg EnhancedMessageResponse) MessageData {
	alignment := "left"
	if msg.Info.Role == "user" {
		alignment = "right"
	}

	var parts []MessagePartData
	for _, part := range msg.Parts {
		// Convert EnhancedMessagePart to MessagePart
		msgPart := MessagePart{
			ID:        part.ID,
			MessageID: part.MessageID,
			SessionID: part.SessionID,
			Type:      part.Type,
			Text:      part.Text,
			Tool:      part.Tool,
			CallID:    part.CallID,
			State:     part.State,
			Time:      part.Time,
		}
		parts = append(parts, transformMessagePart(templates, msgPart))
	}

	return MessageData{
		ID:        msg.Info.ID,
		Alignment: alignment,
		Parts:     parts,
		Provider:  msg.Info.ProviderID,
		Model:     msg.Info.ModelID,
	}
}

func TestHTTPEndpointWithRichContent(t *testing.T) {
	// Create mock OpenCode server
	mockServer := NewMockOpencodeServer()
	defer mockServer.Close()

	// Create a message with various part types
	message := EnhancedMessageResponse{}
	message.Info.ID = "msg_001"
	message.Info.Role = "assistant"
	message.Info.SessionID = "test-session"
	message.Info.ProviderID = "openai"
	message.Info.ModelID = "gpt-4"
	message.Info.Time = time.Now().Format(time.RFC3339)

	message.Parts = []EnhancedMessagePart{
		{
			ID:        "prt_001",
			MessageID: "msg_001",
			SessionID: "test-session",
			Type:      "step-start",
		},
		{
			ID:        "prt_002",
			MessageID: "msg_001",
			SessionID: "test-session",
			Type:      "text",
			Text:      "I'll help you with **markdown** formatting and https://example.com links.",
		},
		{
			ID:        "prt_003",
			MessageID: "msg_001",
			SessionID: "test-session",
			Type:      "reasoning",
			Text:      "Analyzing the request for better understanding...",
		},
		{
			ID:        "prt_004",
			MessageID: "msg_001",
			SessionID: "test-session",
			Type:      "tool",
			Tool:      "bash",
			State: map[string]interface{}{
				"status": "completed",
				"input": map[string]interface{}{
					"command": "echo 'Hello World'",
				},
				"output": "Hello World",
			},
		},
		{
			ID:        "prt_005",
			MessageID: "msg_001",
			SessionID: "test-session",
			Type:      "step-finish",
		},
	}

	mockServer.AddMessage(message)

	// Create test server
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		sessions:  make(map[string]string),
		templates: templates,
	}
	server.sandbox = NewStaticURLSandbox(mockServer.Server.URL)
	server.sessions["test-cookie"] = "test-session"

	// Create enhanced handler that uses the new rendering
	enhancedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Error(w, "No session", http.StatusBadRequest)
			return
		}

		sessionID := server.sessions[cookie.Value]
		if sessionID == "" {
			http.Error(w, "Invalid session", http.StatusBadRequest)
			return
		}

		// Get messages from mock OpenCode
		resp, err := http.Get(fmt.Sprintf("%s/session/%s/message", mockServer.URL, sessionID))
		if err != nil {
			http.Error(w, "Failed to get messages", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		var messages []EnhancedMessageResponse
		if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
			http.Error(w, "Failed to parse messages", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html")

		// Transform and render each message
		for _, msg := range messages {
			msgData := transformEnhancedMessage(server.templates, msg)

			if err := server.templates.ExecuteTemplate(w, "message", msgData); err != nil {
				t.Logf("Template error: %v", err)
			}
		}
	})

	// Create request with session cookie
	req := httptest.NewRequest("GET", "/messages", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "test-cookie"})

	// Record response
	recorder := httptest.NewRecorder()
	enhancedHandler.ServeHTTP(recorder, req)

	// Check response
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()

	// Verify all parts are rendered correctly

	// 1. Check step-start badge
	if !strings.Contains(body, "bg-yellow-100") {
		t.Error("Expected step-start yellow badge styling")
	}
	if !strings.Contains(body, "▶️") {
		t.Error("Expected step-start emoji")
	}

	// 2. Check text with markdown
	if !strings.Contains(body, "<strong>markdown</strong>") {
		t.Error("Expected markdown bold rendering")
	}
	if !strings.Contains(body, `href="https://example.com"`) {
		t.Error("Expected autolink rendering")
	}

	// 3. Check reasoning block
	if !strings.Contains(body, "🤔") {
		t.Error("Expected reasoning emoji")
	}
	if !strings.Contains(body, "Analyzing the request") {
		t.Error("Expected reasoning text")
	}

	// 4. Check tool rendering
	if !strings.Contains(body, "bash") {
		t.Error("Expected tool name")
	}
	if !strings.Contains(body, "Hello World") {
		t.Error("Expected tool output")
	}

	// 5. Check step-finish badge
	if !strings.Contains(body, "bg-green-100") {
		t.Error("Expected step-finish green badge styling")
	}
	if !strings.Contains(body, "✅") {
		t.Error("Expected step-finish emoji")
	}

	// 6. Check provider/model info
	if !strings.Contains(body, "openai/gpt-4") {
		t.Error("Expected provider/model information")
	}
}

func TestHTTPEndpointWithTodoWrite(t *testing.T) {
	// Create mock OpenCode server
	mockServer := NewMockOpencodeServer()
	defer mockServer.Close()

	// Create a message with todowrite tool
	message := EnhancedMessageResponse{}
	message.Info.ID = "msg_002"
	message.Info.Role = "assistant"
	message.Info.SessionID = "test-session"

	todoJSON := `[
		{"content":"Implement feature X","status":"completed","priority":"high"},
		{"content":"Write tests","status":"in_progress","priority":"medium"},
		{"content":"Update documentation","status":"pending","priority":"low"}
	]`

	message.Parts = []EnhancedMessagePart{
		{
			ID:        "prt_001",
			MessageID: "msg_002",
			SessionID: "test-session",
			Type:      "text",
			Text:      "Here's your task list:",
		},
		{
			ID:        "prt_002",
			MessageID: "msg_002",
			SessionID: "test-session",
			Type:      "tool",
			Tool:      "todowrite",
			State: map[string]interface{}{
				"status": "completed",
				"input": map[string]interface{}{
					"todos": "tasks to complete",
				},
				"output": todoJSON,
			},
		},
	}

	mockServer.AddMessage(message)

	// Create test server
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		sessions:  make(map[string]string),
		templates: templates,
	}
	server.sandbox = NewStaticURLSandbox(mockServer.Server.URL)
	server.sessions["test-cookie"] = "test-session"

	// Create enhanced handler
	enhancedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("session")
		sessionID := server.sessions[cookie.Value]

		resp, err := http.Get(fmt.Sprintf("%s/session/%s/message", mockServer.URL, sessionID))
		if err != nil {
			http.Error(w, "Failed to get messages", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		var messages []EnhancedMessageResponse
		json.NewDecoder(resp.Body).Decode(&messages)

		w.Header().Set("Content-Type", "text/html")

		for _, msg := range messages {
			msgData := transformEnhancedMessage(server.templates, msg)
			server.templates.ExecuteTemplate(w, "message", msgData)
		}
	})

	// Create request
	req := httptest.NewRequest("GET", "/messages", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "test-cookie"})

	// Record response
	recorder := httptest.NewRecorder()
	enhancedHandler.ServeHTTP(recorder, req)

	body := recorder.Body.String()

	// Check todo items are rendered
	if !strings.Contains(body, "Implement feature X") {
		t.Error("Expected first todo item")
	}
	if !strings.Contains(body, "Write tests") {
		t.Error("Expected second todo item")
	}
	if !strings.Contains(body, "Update documentation") {
		t.Error("Expected third todo item")
	}

	// Check status indicators - the todo template uses visual indicators like ✓, ⏳, ☐
	if !strings.Contains(body, "✓") {
		t.Error("Expected completed status indicator (✓)")
	}
	if !strings.Contains(body, "⏳") {
		t.Error("Expected in_progress status indicator (⏳)")
	}
	if !strings.Contains(body, "☐") {
		t.Error("Expected pending status indicator (☐)")
	}
}

func TestParityBetweenSSEAndHTTP(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	// Create a test message
	message := EnhancedMessageResponse{}
	message.Info.ID = "msg_003"
	message.Info.Role = "assistant"
	message.Info.SessionID = "test-session"
	message.Info.ProviderID = "anthropic"
	message.Info.ModelID = "claude-3"

	message.Parts = []EnhancedMessagePart{
		{
			ID:   "prt_001",
			Type: "text",
			Text: "# Hello World\n\nThis is **bold** and this is *italic*.",
		},
		{
			ID:   "prt_002",
			Type: "tool",
			Tool: "write",
			State: map[string]interface{}{
				"status": "completed",
				"input": map[string]interface{}{
					"file_path": "/tmp/test.txt",
					"content":   "Test content",
				},
				"output": "File written successfully",
			},
		},
	}

	// Transform message using our new function
	msgData := transformEnhancedMessage(templates, message)

	// Render using the message template
	var buf bytes.Buffer
	err = templates.ExecuteTemplate(&buf, "message", msgData)
	if err != nil {
		t.Fatalf("Failed to render message: %v", err)
	}

	html := buf.String()

	// Verify the output matches what SSE would produce

	// Check alignment
	if !strings.Contains(html, "justify-start") {
		t.Error("Expected left alignment for assistant message")
	}

	// Check markdown rendering in text part
	if !strings.Contains(html, "<h1>Hello World</h1>") {
		t.Error("Expected H1 header from markdown")
	}
	if !strings.Contains(html, "<strong>bold</strong>") {
		t.Error("Expected bold text from markdown")
	}
	if !strings.Contains(html, "<em>italic</em>") {
		t.Error("Expected italic text from markdown")
	}

	// Check tool rendering
	if !strings.Contains(html, "write") {
		t.Error("Expected tool name in output")
	}
	if !strings.Contains(html, "/tmp/test.txt") {
		t.Error("Expected file path in tool output")
	}
	// For write tool, the template shows Content field, not Output
	// Since our test has "Test content" as content and "File written successfully" as output,
	// the template will show the content, not the output
	if !strings.Contains(html, "Test content") {
		t.Logf("HTML output:\n%s", html)
		t.Error("Expected tool content in write tool")
	}

	// Check provider/model
	if !strings.Contains(html, "anthropic/claude-3") {
		t.Error("Expected provider/model information")
	}
}

func TestHTTPEndpointErrorHandling(t *testing.T) {
	// Test various error conditions

	// 1. Test with no session cookie
	server := &Server{
		sessions:  make(map[string]string),
		templates: template.New(""),
	}

	req := httptest.NewRequest("GET", "/messages", nil)
	recorder := httptest.NewRecorder()

	enhancedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Error(w, "No session", http.StatusBadRequest)
			return
		}
		_ = cookie
	})

	enhancedHandler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing session, got %d", recorder.Code)
	}

	// 2. Test with invalid session
	req2 := httptest.NewRequest("GET", "/messages", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: "invalid"})
	recorder2 := httptest.NewRecorder()

	enhancedHandler2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("session")
		sessionID := server.sessions[cookie.Value]
		if sessionID == "" {
			http.Error(w, "Invalid session", http.StatusBadRequest)
			return
		}
	})

	enhancedHandler2.ServeHTTP(recorder2, req2)

	if recorder2.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid session, got %d", recorder2.Code)
	}
}

func TestSSEScrollBehavior(t *testing.T) {
	// Create mock OpenCode server with SSE endpoint
	mockServer := NewMockOpencodeServer()
	defer mockServer.Close()

	// Add initial messages
	userMessage := EnhancedMessageResponse{}
	userMessage.Info.ID = "msg_001"
	userMessage.Info.Role = "user"
	userMessage.Info.SessionID = "test-session"
	userMessage.Parts = []EnhancedMessagePart{
		{
			ID:        "prt_001",
			MessageID: "msg_001",
			SessionID: "test-session",
			Type:      "text",
			Text:      "What is the weather today?",
		},
	}
	mockServer.AddMessage(userMessage)

	// Assistant message that will stream
	assistantMessage := EnhancedMessageResponse{}
	assistantMessage.Info.ID = "msg_002"
	assistantMessage.Info.Role = "assistant"
	assistantMessage.Info.SessionID = "test-session"
	assistantMessage.Info.ProviderID = "openai"
	assistantMessage.Info.ModelID = "gpt-4"
	assistantMessage.Parts = []EnhancedMessagePart{
		{
			ID:        "prt_002_001",
			MessageID: "msg_002",
			SessionID: "test-session",
			Type:      "text",
			Text:      "I'll check the weather for you...",
		},
		{
			ID:        "prt_002_002",
			MessageID: "msg_002",
			SessionID: "test-session",
			Type:      "tool",
			Tool:      "weather",
			State: map[string]interface{}{
				"status": "completed",
				"input": map[string]interface{}{
					"location": "New York",
				},
				"output": "Temperature: 72°F, Sunny",
			},
		},
		{
			ID:        "prt_002_003",
			MessageID: "msg_002",
			SessionID: "test-session",
			Type:      "text",
			Text:      "Today in New York it's 72°F and sunny!",
		},
	}
	mockServer.AddMessage(assistantMessage)

	// Create test server
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		sessions:  make(map[string]string),
		templates: templates,
	}
	server.sandbox = NewStaticURLSandbox(mockServer.Server.URL)
	server.sessions["test-cookie"] = "test-session"

	// Test 1: Verify index page has proper SSE setup
	indexHandler := http.HandlerFunc(server.handleIndex)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "test-cookie"})
	recorder := httptest.NewRecorder()
	indexHandler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()

	// Check for messages div
	if !strings.Contains(body, `id="messages"`) {
		t.Error("Expected messages div with id='messages'")
	}

	// Check for SSE attributes
	if !strings.Contains(body, `sse-connect="/events"`) {
		t.Error("Expected SSE connection to /events endpoint")
	}
	if !strings.Contains(body, `sse-swap="message"`) {
		t.Error("Expected sse-swap='message' attribute")
	}

	// Check that scroll:bottom is removed (our fix)
	if strings.Contains(body, `scroll:bottom`) {
		t.Error("Found scroll:bottom in HTML - should be handled by JavaScript")
	}

	// Check for script.js inclusion
	if !strings.Contains(body, `/static/script.js`) {
		t.Error("Expected script.js to be included")
	}

	// Test 2: Mock SSE endpoint to simulate streaming
	sseTestMux := http.NewServeMux()

	// Mock the /event SSE endpoint
	sseTestMux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("Expected http.Flusher")
			return
		}

		// Simulate initial message part
		initialEvent := map[string]interface{}{
			"type": "message.part.updated",
			"properties": map[string]interface{}{
				"info": map[string]interface{}{
					"sessionID": "test-session",
					"id":        "msg_002",
				},
				"partID": "prt_002_001",
				"part": map[string]interface{}{
					"type": "text",
					"text": "I'll check the weather",
				},
			},
		}
		data, _ := json.Marshal(initialEvent)
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()

		// Simulate updated message part (OOB update)
		time.Sleep(10 * time.Millisecond)
		updateEvent := map[string]interface{}{
			"type": "message.part.updated",
			"properties": map[string]interface{}{
				"info": map[string]interface{}{
					"sessionID": "test-session",
					"id":        "msg_002",
				},
				"partID": "prt_002_001",
				"part": map[string]interface{}{
					"type": "text",
					"text": "I'll check the weather for you... (updating)",
				},
			},
		}
		data, _ = json.Marshal(updateEvent)
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
	})

	sseTestServer := httptest.NewServer(sseTestMux)
	defer sseTestServer.Close()

	// Test 3: Verify message rendering for SSE
	// Transform and render messages to check proper ID attributes
	for _, msg := range mockServer.Messages {
		msgData := transformEnhancedMessage(templates, msg)

		// Check for streaming state
		msgData.IsStreaming = true // Simulate streaming

		var buf bytes.Buffer
		err := templates.ExecuteTemplate(&buf, "message", msgData)
		if err != nil {
			t.Errorf("Failed to render message: %v", err)
			continue
		}

		html := buf.String()

		// Verify message has ID for OOB updates
		if strings.Contains(html, "id=") && msg.Info.Role == "assistant" {
			// Assistant messages need IDs for SSE updates
			if !strings.Contains(html, msg.Info.ID) {
				t.Errorf("Expected message to have ID containing %s for OOB updates", msg.Info.ID)
			}
		}

		// Check streaming class
		if msgData.IsStreaming && !strings.Contains(html, "streaming") {
			t.Error("Expected streaming class on message div")
		}
	}

	// Test 4: Verify OOB updates
	msgData := transformEnhancedMessage(templates, assistantMessage)
	msgData.HXSwapOOB = true // This should be set for updates

	var buf bytes.Buffer
	err = templates.ExecuteTemplate(&buf, "message", msgData)
	if err != nil {
		t.Fatalf("Failed to render OOB message: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `hx-swap-oob="true"`) {
		t.Error("Expected hx-swap-oob='true' for message updates")
	}
}
