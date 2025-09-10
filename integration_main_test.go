package main

import (
	"bufio"
	"context"
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

	// Check HTMX attributes - hx-post is on the textarea and button, not the form
	sendElements := doc.Find("[hx-post='/send']")
	if sendElements.Length() == 0 {
		t.Error("Send elements with hx-post not found")
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

	// Check for right-aligned message (user message) - new template uses flex justify-end
	userMsg := doc.Find("div.my-2.flex.justify-end")
	if userMsg.Length() == 0 {
		t.Error("User message not found")
	}

	// Check message content in the message bubble
	msgBubble := userMsg.Find("div.bg-gray-300")
	msgText := msgBubble.Text()
	if !strings.Contains(msgText, "Hello, OpenCode!") {
		t.Errorf("Message text not found, got: %s", msgText)
	}

	// Check model info
	modelInfo := msgBubble.Find("div.text-xs.text-gray-600").Text()
	if !strings.Contains(modelInfo, "anthropic/claude-3-5-haiku-20241022") {
		t.Errorf("Model info not found in message, got: %s", modelInfo)
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

	// Wait for message to actually be stored in opencode
	// Need to poll because handleSend sends message async
	var messagesHTML string
	success := false
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		messagesHTML = server.getMessagesHTML(sessionID)
		if messagesHTML != "" {
			success = true
			break
		}
	}

	if !success {
		t.Fatal("No messages found after waiting 2 seconds")
	}

	// Parse HTML response
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(messagesHTML))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}

	// Check for message elements - new template uses flex containers
	messages := doc.Find("div.my-2.flex")
	if messages.Length() == 0 {
		t.Error("No messages found in response")
	}

	// Check for user message (right-aligned with justify-end)
	userMsg := doc.Find("div.justify-end")
	if userMsg.Length() == 0 {
		t.Error("User message not found")
	}

	// Get the message text from the rounded container
	msgText := userMsg.Find("div.rounded-lg").Text()
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
				"claude-3-opus":             {ID: "claude-3-opus", Name: "Claude 3 Opus"},
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

// TestSSEEndpoint - TODO: Implement proper SSE testing
//
// What we SHOULD test:
// 1. SSE Connection Setup:
//   - Verify endpoint returns correct headers (Content-Type: text/event-stream, Cache-Control: no-cache)
//   - Verify connection stays open (doesn't close immediately)
//   - Verify SSE connects to the OpenCode /event endpoint correctly
//
// 2. Session Filtering:
//   - Verify SSE only forwards events for the client's session ID
//   - Send messages to multiple sessions and verify filtering works
//   - Verify message.part.updated events are filtered by sessionID
//
// 3. Event Streaming:
//   - Verify initial "server.connected" event is received
//   - Send a message via /send and verify corresponding SSE events arrive
//   - Verify events arrive in correct SSE format (data: {json}\n\n)
//   - Verify message parts are streamed incrementally (not all at once)
//
// 4. Message Transformation:
//   - Verify SSE handler correctly transforms OpenCode events to HTML
//   - Verify transformMessagePart is called for each message part
//   - Verify HTML includes proper HTMX attributes (hx-swap-oob)
//
// 5. Connection Management:
//   - Verify SSE reconnects if OpenCode connection drops
//   - Verify client disconnect is handled cleanly
//   - Verify no goroutine leaks after client disconnects
//
// Technical Challenges:
// - SSE streams are long-lived and never end normally
// - bufio.Scanner.Scan() blocks indefinitely on SSE streams
// - reader.ReadString() also blocks waiting for data
// - Need to handle partial reads and buffering correctly
// - httptest.ResponseRecorder doesn't work well with streaming responses
// - Context cancellation doesn't interrupt blocked Read operations
//
// Potential Solutions:
// 1. Use a custom Reader wrapper that supports timeouts
// 2. Use non-blocking I/O with SetReadDeadline on the underlying connection
// 3. Read raw bytes in chunks instead of line-by-line
// 4. Use a separate goroutine with channels but handle goroutine cleanup properly
// 5. Consider using a real HTTP client/server instead of httptest
func TestSSEEndpoint(t *testing.T) {
	// This test verifies that our /events SSE endpoint streams transformed
	// assistant message updates to the client with minimal stubbing, and that
	// subsequent frames use hx-swap-oob for updates.

	srv, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Seed a known session so getOrCreateSession returns without calling /session
	cookie := &http.Cookie{Name: "session", Value: "sess_test_sse"}
	sessionID := "ses_test_123"
	srv.sessions[cookie.Value] = sessionID

	// Upstream SSE stub emitting two incremental text parts for one message
	upstreamMux := http.NewServeMux()
	upstreamMux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		if f, ok := w.(http.Flusher); ok {
			// Two parts build up "Hello world"
			events := []string{
				fmt.Sprintf(`{"type":"message.part.updated","properties":{"part":{"id":"prt1","messageID":"msg1","sessionID":"%s","type":"text","text":"Hello"}}}`, sessionID),
				fmt.Sprintf(`{"type":"message.part.updated","properties":{"part":{"id":"prt2","messageID":"msg1","sessionID":"%s","type":"text","text":" world"}}}`, sessionID),
			}
			for _, e := range events {
				fmt.Fprintf(w, "data: %s\n\n", e)
				f.Flush()
				time.Sleep(50 * time.Millisecond)
			}
		}
	})
	upstream := httptest.NewServer(upstreamMux)
	defer upstream.Close()

	// Point server to upstream stub
	hostport := strings.TrimPrefix(upstream.URL, "http://")
	parts := strings.Split(hostport, ":")
	if len(parts) < 2 {
		t.Fatalf("failed to parse upstream URL: %s", upstream.URL)
	}
	var p int
	if _, err := fmt.Sscanf(parts[1], "%d", &p); err != nil {
		t.Fatalf("failed to parse upstream port: %v", err)
	}
	srv.opencodePort = p

	// Expose /events
	mux := http.NewServeMux()
	mux.HandleFunc("/events", srv.handleSSE)
	app := httptest.NewServer(mux)
	defer app.Close()

	// Connect with cookie
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", app.URL+"/events", nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect to SSE endpoint: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("unexpected content type: %s", ct)
	}

	// Read SSE events using a scanner; collect first two message frames
	scanner := bufio.NewScanner(resp.Body)
	var (
		eventName string
		dataLines []string
		frames    []string
	)
	done := time.After(2 * time.Second)

	for {
		select {
		case <-done:
			// timeout safeguard
			goto ASSERT
		default:
		}
		if !scanner.Scan() {
			// stream ended
			break
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			continue
		}
		if line == "" { // end of one SSE event
			if eventName == "message" {
				html := strings.Join(dataLines, "\n")
				frames = append(frames, html)
				if len(frames) >= 2 {
					break
				}
			}
			eventName = ""
			dataLines = nil
		}
	}

ASSERT:
	if len(frames) < 2 {
		t.Fatalf("expected at least 2 streamed message frames, got %d", len(frames))
	}
	first, second := frames[0], frames[1]
	if !strings.Contains(first, "Hello") {
		t.Fatalf("first frame should contain partial text 'Hello'\nfirst: %s", first)
	}
	if strings.Contains(first, "hx-swap-oob") {
		t.Fatalf("first frame should NOT include hx-swap-oob (only updates should)")
	}
	if !strings.Contains(second, "hx-swap-oob") {
		t.Fatalf("second frame should include hx-swap-oob for out-of-band update\nsecond: %s", second)
	}
	if !strings.Contains(second, "world") {
		t.Fatalf("second frame should contain subsequent partial 'world'\nsecond: %s", second)
	}
	if !(strings.Contains(first, "assistant-msg1") && strings.Contains(second, "assistant-msg1")) {
		t.Errorf("expected consistent id 'assistant-msg1' across frames")
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

	// Should contain message bubble (the actual div structure from the template)
	if !strings.Contains(htmlStr, "my-2 flex justify-end") {
		t.Errorf("HTML fragment should contain message bubble, got: %s", htmlStr)
	}
}

func TestSSEFiltersBySession(t *testing.T) {
	srv, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	cookie := &http.Cookie{Name: "session", Value: "sess_filter"}
	mySession := "ses_filter_123"
	srv.sessions[cookie.Value] = mySession

	// Upstream SSE: mix events from another session and ours
	upstreamMux := http.NewServeMux()
	upstreamMux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		if f, ok := w.(http.Flusher); ok {
			events := []string{
				// Other session should be ignored
				`{"type":"message.part.updated","properties":{"part":{"id":"o1","messageID":"omsg","sessionID":"ses_other","type":"text","text":"IGNORE_ME"}}}`,
				// Our session: first frame
				fmt.Sprintf(`{"type":"message.part.updated","properties":{"part":{"id":"p1","messageID":"msgF","sessionID":"%s","type":"text","text":"Keep"}}}`, mySession),
				// Other session again
				`{"type":"message.part.updated","properties":{"part":{"id":"o2","messageID":"omsg","sessionID":"ses_other","type":"text","text":"IGNORE2"}}}`,
				// Our session: second frame
				fmt.Sprintf(`{"type":"message.part.updated","properties":{"part":{"id":"p2","messageID":"msgF","sessionID":"%s","type":"text","text":" Me"}}}`, mySession),
			}
			for _, e := range events {
				fmt.Fprintf(w, "data: %s\n\n", e)
				f.Flush()
				time.Sleep(20 * time.Millisecond)
			}
		}
	})
	upstream := httptest.NewServer(upstreamMux)
	defer upstream.Close()

	// Point server to upstream stub
	hostport := strings.TrimPrefix(upstream.URL, "http://")
	parts := strings.Split(hostport, ":")
	if len(parts) < 2 {
		t.Fatalf("failed to parse upstream URL: %s", upstream.URL)
	}
	var pport int
	if _, err := fmt.Sscanf(parts[1], "%d", &pport); err != nil {
		t.Fatalf("failed to parse upstream port: %v", err)
	}
	srv.opencodePort = pport

	// Expose /events
	mux := http.NewServeMux()
	mux.HandleFunc("/events", srv.handleSSE)
	app := httptest.NewServer(mux)
	defer app.Close()

	// Connect
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", app.URL+"/events", nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect to SSE endpoint: %v", err)
	}
	defer resp.Body.Close()

	// Collect up to 2 frames
	scanner := bufio.NewScanner(resp.Body)
	var (
		eventName string
		dataLines []string
		frames    []string
	)
	done := time.After(2 * time.Second)
	for {
		select {
		case <-done:
			break
		default:
		}
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			continue
		}
		if line == "" {
			if eventName == "message" {
				frames = append(frames, strings.Join(dataLines, "\n"))
				if len(frames) >= 2 {
					break
				}
			}
			eventName = ""
			dataLines = nil
		}
	}

	if len(frames) < 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	all := strings.Join(frames, "\n---\n")
	if strings.Contains(all, "IGNORE_ME") || strings.Contains(all, "IGNORE2") {
		t.Fatalf("filtered frames should not contain other session content: %s", all)
	}
	if !strings.Contains(frames[0], "Keep") || !strings.Contains(frames[1], "Me") {
		t.Fatalf("expected our session content across frames: %v", frames)
	}
}

func TestSSEStreamsToolOutput(t *testing.T) {
	srv, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	cookie := &http.Cookie{Name: "session", Value: "sess_tool"}
	mySession := "ses_tool_123"
	srv.sessions[cookie.Value] = mySession

	upstreamMux := http.NewServeMux()
	upstreamMux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		if f, ok := w.(http.Flusher); ok {
			events := []string{
				// Initial text part
				fmt.Sprintf(`{"type":"message.part.updated","properties":{"part":{"id":"t1","messageID":"msgT","sessionID":"%s","type":"text","text":"Hello"}}}`, mySession),
				// Tool part with bash
				fmt.Sprintf(`{"type":"message.part.updated","properties":{"part":{"id":"t2","messageID":"msgT","sessionID":"%s","type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"echo hi"},"output":"hi"}}}}`, mySession),
			}
			for _, e := range events {
				fmt.Fprintf(w, "data: %s\n\n", e)
				f.Flush()
				time.Sleep(20 * time.Millisecond)
			}
		}
	})
	upstream := httptest.NewServer(upstreamMux)
	defer upstream.Close()

	hostport := strings.TrimPrefix(upstream.URL, "http://")
	parts := strings.Split(hostport, ":")
	if len(parts) < 2 {
		t.Fatalf("failed to parse upstream URL: %s", upstream.URL)
	}
	var pport int
	if _, err := fmt.Sscanf(parts[1], "%d", &pport); err != nil {
		t.Fatalf("failed to parse upstream port: %v", err)
	}
	srv.opencodePort = pport

	mux := http.NewServeMux()
	mux.HandleFunc("/events", srv.handleSSE)
	app := httptest.NewServer(mux)
	defer app.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", app.URL+"/events", nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect to SSE endpoint: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var (
		eventName string
		dataLines []string
		frames    []string
	)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			continue
		}
		if line == "" {
			if eventName == "message" {
				frames = append(frames, strings.Join(dataLines, "\n"))
				if len(frames) >= 2 {
					break
				}
			}
			eventName = ""
			dataLines = nil
		}
	}

	if len(frames) < 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	first, second := frames[0], frames[1]
	if !strings.Contains(first, "Hello") {
		t.Fatalf("first frame should contain text 'Hello'\n%s", first)
	}
	if !strings.Contains(second, "hx-swap-oob") {
		t.Fatalf("second frame should include hx-swap-oob\n%s", second)
	}
	if !strings.Contains(second, "Output:") {
		t.Fatalf("second frame should include tool output label\n%s", second)
	}
	if !strings.Contains(second, "echo hi") {
		t.Fatalf("second frame should include bash command\n%s", second)
	}
	if !strings.Contains(second, "hi") {
		t.Fatalf("second frame should include tool output body\n%s", second)
	}
}
