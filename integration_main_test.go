package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestIndexPage(t *testing.T) {
	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	server.providers = []Provider{
		{
			ID:   "anthropic",
			Name: "Anthropic",
			Models: map[string]Model{
				"claude-3-5-haiku-20241022": {ID: "claude-3-5-haiku-20241022", Name: "Claude 3.5 Haiku"},
			},
		},
	}
	server.defaultModel = map[string]string{
		"anthropic": "claude-3-5-haiku-20241022",
	}

	// Start opencode for this test
	if err := server.startOpencodeServer(); err != nil {
		t.Fatalf("Failed to start opencode: %v", err)
	}
	defer server.stopOpencodeServer()
	if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		t.Fatalf("Opencode server not ready: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	server.handleIndex(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}

	// Check for essential elements
	if doc.Find("#messages").Length() == 0 {
		t.Error("Messages container not found")
	}

	if doc.Find("#message-input").Length() == 0 {
		t.Error("Message input not found")
	}

	if doc.Find("#provider").Length() == 0 {
		t.Error("Provider selector not found")
	}

	if doc.Find("#model").Length() == 0 {
		t.Error("Model selector not found")
	}

	// Check HTMX attributes
	form := doc.Find("form[hx-post='/send']")
	if form.Length() == 0 {
		t.Error("Send form with hx-post not found")
	}

	// Check SSE setup
	sseElement := doc.Find("[sse-connect='/events']")
	if sseElement.Length() == 0 {
		t.Error("SSE connection element not found")
	}

	// Check for clear button
	clearBtn := doc.Find("button[hx-post='/clear']")
	if clearBtn.Length() == 0 {
		t.Error("Clear session button not found")
	}
}

func TestSendMessage(t *testing.T) {
	server := StartTestServer(t, GetTestPort())
	defer server.stopOpencodeServer()

	// Create session
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	server.handleIndex(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("No session cookie created")
	}

	// Send message
	form := "message=Hello&provider=anthropic&model=claude-3-5-haiku-20241022"
	req = httptest.NewRequest("POST", "/send", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookies[0])

	w = httptest.NewRecorder()
	server.handleSend(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check response contains message bubble
	body := w.Body.String()
	if !strings.Contains(body, "justify-end") {
		t.Error("Response should contain right-aligned message bubble")
	}
	if !strings.Contains(body, "Hello") {
		t.Error("Response should contain the message text")
	}
	// Check that user message shows provider/model
	if !strings.Contains(body, "anthropic/claude-3-5-haiku-20241022") {
		t.Error("Response should contain provider/model info")
	}
}

func TestSSEStreaming(t *testing.T) {
	server := StartTestServer(t, GetTestPort())
	defer server.stopOpencodeServer()

	// Create a session first
	cookie := &http.Cookie{Name: "session", Value: "test-send"}
	sessionID, err := server.getOrCreateSession(cookie.Value)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Prepare form data
	form := url.Values{}
	form.Add("message", "Hello, OpenCode!")
	form.Add("provider", "anthropic")
	form.Add("model", "claude-3-5-haiku-20241022")

	req := httptest.NewRequest("POST", "/send", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	server.handleSend(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Parse response HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}

	// Check for right-aligned message (user message)
	userMsg := doc.Find(".message-right")
	if userMsg.Length() == 0 {
		t.Error("User message not found")
	}

	// Check message content
	msgText := userMsg.Find(".message-bubble").Text()
	if !strings.Contains(msgText, "Hello, OpenCode!") {
		t.Errorf("Message text not found, got: %s", msgText)
	}

	// Check model info
	if !strings.Contains(msgText, "anthropic/claude-3-5-haiku-20241022") {
		t.Error("Model info not found in message")
	}

	// Wait for message to be processed by opencode
	if err := WaitForMessageProcessed(server.opencodePort, sessionID, 5*time.Second); err != nil {
		t.Fatalf("Message not processed: %v", err)
	}

	// Verify message was sent to opencode
	// Get messages from opencode API
	apiResp, err := http.Get(fmt.Sprintf("http://localhost:%d/session/%s/message", server.opencodePort, sessionID))
	if err != nil {
		t.Fatalf("Failed to get messages from API: %v", err)
	}
	defer apiResp.Body.Close()

	var messages []MessageResponse
	json.NewDecoder(apiResp.Body).Decode(&messages)

	if len(messages) == 0 {
		t.Error("No messages found in opencode session")
	}
}

func TestClearSession(t *testing.T) {
	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start opencode
	err = server.startOpencodeServer()
	if err != nil {
		t.Fatalf("Failed to start opencode: %v", err)
	}
	defer server.stopOpencodeServer()
	if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		t.Fatalf("Opencode server not ready: %v", err)
	}

	// Create a session
	cookie := &http.Cookie{Name: "session", Value: "test-clear"}
	sessionID, err := server.getOrCreateSession(cookie.Value)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Send a message first
	form := url.Values{}
	form.Add("message", "Test message")
	form.Add("provider", "anthropic")
	form.Add("model", "claude-3-5-haiku-20241022")

	req := httptest.NewRequest("POST", "/send", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	server.handleSend(w, req)

	// Now clear the session
	req = httptest.NewRequest("POST", "/clear", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()

	server.handleClear(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check that old session is gone
	if _, exists := server.sessions[cookie.Value]; exists {
		// Session might be recreated, check if it's different
		newSessionID, _ := server.getOrCreateSession(cookie.Value)
		if newSessionID == sessionID {
			t.Error("Session was not cleared")
		}
	}
}

func TestGetMessages(t *testing.T) {
	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start opencode
	err = server.startOpencodeServer()
	if err != nil {
		t.Fatalf("Failed to start opencode: %v", err)
	}
	defer server.stopOpencodeServer()
	if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		t.Fatalf("Opencode server not ready: %v", err)
	}

	// Create session and send messages
	cookie := &http.Cookie{Name: "session", Value: "test-messages"}
	sessionID, err := server.getOrCreateSession(cookie.Value)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Send a message
	form := url.Values{}
	form.Add("message", "Test message for retrieval")
	form.Add("provider", "anthropic")
	form.Add("model", "claude-3-5-haiku-20241022")

	req := httptest.NewRequest("POST", "/send", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	server.handleSend(w, req)

	// Wait for message to be processed  
	if err := WaitForMessageProcessed(server.opencodePort, sessionID, 5*time.Second); err != nil {
		t.Logf("Warning: Message may not be processed yet: %v", err)
	}

	// Now get messages
	req = httptest.NewRequest("GET", "/messages", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()

	server.handleMessages(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Parse HTML response
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}

	// Check for message elements
	messages := doc.Find(".message")
	if messages.Length() == 0 {
		t.Error("No messages found in response")
	}

	// Check for user message (right-aligned)
	userMsg := doc.Find(".message-right")
	if userMsg.Length() == 0 {
		t.Error("User message not found")
	}

	msgText := userMsg.Find(".message-bubble").Text()
	if !strings.Contains(msgText, "Test message for retrieval") {
		t.Errorf("Expected message text not found, got: %s", msgText)
	}
}

func TestProviderModelSelection(t *testing.T) {
	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	// Start opencode for this test
	if err := server.startOpencodeServer(); err != nil {
		t.Fatalf("Failed to start opencode: %v", err)
	}
	defer server.stopOpencodeServer()
	if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		t.Fatalf("Opencode server not ready: %v", err)
	}
	
	server.providers = []Provider{
		{
			ID:   "anthropic",
			Name: "Anthropic",
			Models: map[string]Model{
				"claude-3-5-haiku-20241022": {ID: "claude-3-5-haiku-20241022", Name: "Claude 3.5 Haiku"},
				"claude-3-opus":     {ID: "claude-3-opus", Name: "Claude 3 Opus"},
			},
		},
		{
			ID:   "openai",
			Name: "OpenAI",
			Models: map[string]Model{
				"gpt-4": {ID: "gpt-4", Name: "GPT-4"},
			},
		},
	}
	server.defaultModel = map[string]string{
		"anthropic": "claude-3-5-haiku-20241022",
		"openai":    "gpt-4",
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	server.handleIndex(w, req)

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(w.Result().Body)
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}

	// Check provider dropdown
	providerOptions := doc.Find("#provider option")
	if providerOptions.Length() != 2 {
		t.Errorf("Expected 2 provider options, got %d", providerOptions.Length())
	}

	// Check if anthropic is selected by default
	selectedProvider := doc.Find("#provider option[selected]")
	if selectedProvider.AttrOr("value", "") != "anthropic" {
		t.Error("Anthropic not selected by default")
	}

	// Check that providers are rendered in HTML
	htmlContent := w.Body.String()
	if !strings.Contains(htmlContent, `value="anthropic"`) {
		t.Error("Anthropic provider not found in HTML")
	}
	if !strings.Contains(htmlContent, `>Anthropic</option>`) {
		t.Error("Anthropic provider name not found in HTML")
	}
}

func TestSSEEndpoint(t *testing.T) {
	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start opencode
	err = server.startOpencodeServer()
	if err != nil {
		t.Fatalf("Failed to start opencode: %v", err)
	}
	defer server.stopOpencodeServer()
	if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		t.Fatalf("Opencode server not ready: %v", err)
	}

	// Create session
	cookie := &http.Cookie{Name: "session", Value: "test-sse"}
	_, err = server.getOrCreateSession(cookie.Value)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest("GET", "/events", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()

	// Note: Full SSE testing would require a more complex setup
	// Here we just check that the endpoint responds correctly
	go server.handleSSE(w, req)

	// SSE headers are set when handler starts writing
	// For httptest.ResponseRecorder, we need to wait for the goroutine to start
	var resp *http.Response
	start := time.Now()
	for time.Since(start) < 2*time.Second {
		resp = w.Result()
		if resp.Header.Get("Content-Type") != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream, got %s", contentType)
	}

	cacheControl := resp.Header.Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Expected Cache-Control no-cache, got %s", cacheControl)
	}
}

func TestHTMXHeaders(t *testing.T) {
	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start opencode
	err = server.startOpencodeServer()
	if err != nil {
		t.Fatalf("Failed to start opencode: %v", err)
	}
	defer server.stopOpencodeServer()
	if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		t.Fatalf("Opencode server not ready: %v", err)
	}

	cookie := &http.Cookie{Name: "session", Value: "test-htmx"}
	server.getOrCreateSession(cookie.Value)

	// Test with HTMX request headers
	form := url.Values{}
	form.Add("message", "HTMX test")
	form.Add("provider", "anthropic")
	form.Add("model", "claude-3-5-haiku-20241022")

	req := httptest.NewRequest("POST", "/send", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	server.handleSend(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	// Should return HTML fragment, not full page
	htmlStr := string(body)
	if strings.Contains(htmlStr, "<!DOCTYPE html>") {
		t.Error("Should return HTML fragment, not full page")
	}

	// Should contain message bubble
	if !strings.Contains(htmlStr, "message-bubble") {
		t.Error("HTML fragment should contain message bubble")
	}
}
