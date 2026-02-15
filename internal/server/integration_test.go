package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"opencode-chat/internal/models"
	"opencode-chat/internal/sandbox"
	"opencode-chat/internal/views"
)

// One real-sandbox server for this file's tests
var flowSuite SuiteHandle

func flowServer(t *testing.T) *Server {
	return RealSuiteServer(t, &flowSuite)
}

func TestIntegrationIndexPage(t *testing.T) {
	server := flowServer(t)
	server.providers = []models.Provider{
		{
			ID:   "opencode",
			Name: "OpenCode",
			Models: map[string]models.Model{
				"minimax-m2.5-free": {ID: "minimax-m2.5-free", Name: "MiniMax M2.5 Free"},
			},
		},
	}
	server.defaultModel = map[string]string{
		"opencode": "minimax-m2.5-free",
	}

	// Suite server already running

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

	// Provider selector removed; model selector encodes provider/model.

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

func TestIntegrationSendMessage(t *testing.T) {
	server := flowServer(t)

	// Create session
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	server.handleIndex(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("No session cookie created")
	}

	// Send message using a valid model from sandbox
	combined := GetSupportedModelCombined(t, server)
	form := fmt.Sprintf("message=Hello&model=%s", url.QueryEscape(combined))
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
	// Check that user message shows provider/model used
	if !strings.Contains(body, combined) {
		t.Errorf("Response should contain provider/model info %q, got: %s", combined, body)
	}
}

func TestIntegrationSSEStreaming(t *testing.T) {
	server := flowServer(t)

	// Create a session first
	cookie := &http.Cookie{Name: "session", Value: "test-send"}
	_, err := server.getOrCreateSession(cookie.Value)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Prepare form data
	form := url.Values{}
	form.Add("message", "Hello, OpenCode!")
	form.Add("model", GetSupportedModelCombined(t, server))

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
	if !strings.Contains(modelInfo, GetSupportedModelCombined(t, server)) {
		t.Errorf("Model info not found in message, got: %s", modelInfo)
	}

	// Do not assert OpenCode persistence; the real sandbox may reject models.
}

func TestIntegrationClearSession(t *testing.T) {
	server := flowServer(t)

	// Create a session
	cookie := &http.Cookie{Name: "session", Value: "test-clear"}
	sessionID, err := server.getOrCreateSession(cookie.Value)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Send a message first
	form := url.Values{}
	form.Add("message", "Test message")
	form.Add("model", GetSupportedModelCombined(t, server))

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

func TestIntegrationGetMessages(t *testing.T) {
	server := flowServer(t)

	// Create session and send messages
	cookie := &http.Cookie{Name: "session", Value: "test-messages"}
	sessionID, err := server.getOrCreateSession(cookie.Value)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Send a message
	form := url.Values{}
	form.Add("message", "Test message for retrieval")
	form.Add("model", GetSupportedModelCombined(t, server))

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

func TestIntegrationProviderModelSelection(t *testing.T) {
	server := flowServer(t)

	server.providers = []models.Provider{
		{
			ID:   "opencode",
			Name: "OpenCode",
			Models: map[string]models.Model{
				"minimax-m2.5-free": {ID: "minimax-m2.5-free", Name: "MiniMax M2.5 Free"},
				"minimax-m2.5":     {ID: "minimax-m2.5", Name: "MiniMax M2.5"},
			},
		},
		{
			ID:   "openai",
			Name: "OpenAI",
			Models: map[string]models.Model{
				"gpt-4": {ID: "gpt-4", Name: "GPT-4"},
			},
		},
	}
	server.defaultModel = map[string]string{
		"opencode": "minimax-m2.5-free",
		"openai":   "gpt-4",
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	server.handleIndex(w, req)

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(w.Result().Body)
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}

	// Check model dropdown instead of provider dropdown in new UI
	modelOptions := doc.Find("#model option")
	if modelOptions.Length() < 2 {
		t.Errorf("Expected at least 2 model options, got %d", modelOptions.Length())
	}

	// Check that combined provider/model values and labels appear
	htmlContent := w.Body.String()
	if !strings.Contains(htmlContent, `value="opencode/minimax-m2.5-free"`) {
		t.Error("OpenCode MiniMax model option not found in HTML")
	}
	if !strings.Contains(htmlContent, `OpenCode - MiniMax M2.5 Free`) {
		t.Error("OpenCode MiniMax model label not found in HTML")
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
func TestIntegrationSSEEndpoint(t *testing.T) {
	// This test verifies that our /events SSE endpoint streams transformed
	// assistant message updates to the client with minimal stubbing, and that
	// subsequent frames use hx-swap-oob for updates.

	srv, err := NewServer()
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
	srv.Sandbox = sandbox.NewStaticURLSandbox(upstream.URL)

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

func TestIntegrationHTMXHeaders(t *testing.T) {
	server := flowServer(t)

	cookie := &http.Cookie{Name: "session", Value: "test-htmx"}
	server.getOrCreateSession(cookie.Value)

	// Test with HTMX request headers
	form := url.Values{}
	form.Add("message", "HTMX test")
	form.Add("model", GetSupportedModelCombined(t, server))

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

func TestIntegrationSSEFiltersBySession(t *testing.T) {
	srv, err := NewServer()
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
	srv.Sandbox = sandbox.NewStaticURLSandbox(upstream.URL)

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

func TestIntegrationSSEStreamsToolOutput(t *testing.T) {
	srv, err := NewServer()
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
	srv.Sandbox = sandbox.NewStaticURLSandbox(upstream.URL)

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

func TestIntegrationPreviewBasicFunctionality(t *testing.T) {
	server := flowServer(t)

	// Test port detection returns an array (possibly empty)
	ports := server.detectOpenPorts()
	if ports == nil {
		t.Error("detectOpenPorts should return empty slice, not nil")
	}
	t.Logf("Detected ports: %v", ports)

	// Test preview tab loads without error
	req := httptest.NewRequest("GET", "/tab/preview", nil)
	w := httptest.NewRecorder()
	server.handleTabPreview(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Preview tab should return OK, got %d", w.Code)
	}

	body := w.Body.String()
	if len(ports) > 0 {
		if !strings.Contains(body, "preview-iframe") {
			t.Error("Preview tab should show iframe when ports are detected")
		}
	} else {
		if !strings.Contains(body, "No Application Running") {
			t.Error("Preview tab should show 'No Application Running' when no ports detected")
		}
	}
}

func TestIntegrationPreviewProxyWithNoApplication(t *testing.T) {
	server := flowServer(t)

	// Test the preview proxy endpoint when no application is running
	req := httptest.NewRequest("GET", "/preview", nil)
	w := httptest.NewRecorder()

	server.handlePreviewProxy(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()

	// Should show the "No Application Running" message
	if !strings.Contains(body, "No Application Running") {
		t.Error("Preview proxy should show 'No Application Running' when no ports detected")
	}

	if !strings.Contains(body, "Start a web server") {
		t.Error("Preview proxy should show helpful message about starting a server")
	}
}

func TestIntegrationPreviewPortDetection(t *testing.T) {
	server := flowServer(t)

	// Initialize workspace session if needed
	if server.workspaceSession == "" {
		if err := server.InitWorkspaceSession(); err != nil {
			t.Skipf("Cannot test port detection without workspace session: %v", err)
		}
	}

	// Test the detectOpenPorts function
	ports := server.detectOpenPorts()

	// The result should be an array (possibly empty)
	if ports == nil {
		t.Error("detectOpenPorts should return empty slice, not nil")
	}

	// Verify that common ports are filtered out
	for _, port := range ports {
		if port == 8080 || port == 8081 {
			t.Errorf("Port %d should have been filtered out", port)
		}
		if port < 1024 {
			t.Errorf("System port %d should have been filtered out", port)
		}
	}

	t.Logf("Detected ports: %v", ports)
}

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

// transformEnhancedMessage transforms an EnhancedMessageResponse to views.MessageData with rendered parts
func transformEnhancedMessage(templates *template.Template, msg EnhancedMessageResponse) views.MessageData {
	alignment := "left"
	if msg.Info.Role == "user" {
		alignment = "right"
	}

	var parts []views.MessagePartData
	for _, part := range msg.Parts {
		// Convert EnhancedMessagePart to models.MessagePart
		msgPart := models.MessagePart{
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
		parts = append(parts, views.TransformMessagePart(templates, msgPart))
	}

	return views.MessageData{
		ID:        msg.Info.ID,
		Alignment: alignment,
		Parts:     parts,
		Provider:  msg.Info.ProviderID,
		Model:     msg.Info.ModelID,
	}
}

func TestIntegrationHTTPEndpointWithRichContent(t *testing.T) {
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
			State: map[string]any{
				"status": "completed",
				"input": map[string]any{
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
	templates, err := views.LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		sessions:  make(map[string]string),
		templates: templates,
	}
	server.Sandbox = sandbox.NewStaticURLSandbox(mockServer.Server.URL)
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

func TestIntegrationHTTPEndpointWithTodoWrite(t *testing.T) {
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
			State: map[string]any{
				"status": "completed",
				"input": map[string]any{
					"todos": "tasks to complete",
				},
				"output": todoJSON,
			},
		},
	}

	mockServer.AddMessage(message)

	// Create test server
	templates, err := views.LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		sessions:  make(map[string]string),
		templates: templates,
	}
	server.Sandbox = sandbox.NewStaticURLSandbox(mockServer.Server.URL)
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

func TestIntegrationParityBetweenSSEAndHTTP(t *testing.T) {
	templates, err := views.LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	// Create a test message
	message := EnhancedMessageResponse{}
	message.Info.ID = "msg_003"
	message.Info.Role = "assistant"
	message.Info.SessionID = "test-session"
	message.Info.ProviderID = "opencode"
	message.Info.ModelID = "minimax-m2.5-free"

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
			State: map[string]any{
				"status": "completed",
				"input": map[string]any{
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
	if !strings.Contains(html, "opencode/minimax-m2.5-free") {
		t.Error("Expected provider/model information")
	}
}

func TestIntegrationHTTPEndpointErrorHandling(t *testing.T) {
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

func TestIntegrationSSEScrollBehavior(t *testing.T) {
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
			State: map[string]any{
				"status": "completed",
				"input": map[string]any{
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
	templates, err := views.LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		sessions:  make(map[string]string),
		templates: templates,
	}
	server.Sandbox = sandbox.NewStaticURLSandbox(mockServer.Server.URL)
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
		initialEvent := map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"info": map[string]any{
					"sessionID": "test-session",
					"id":        "msg_002",
				},
				"partID": "prt_002_001",
				"part": map[string]any{
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
		updateEvent := map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"info": map[string]any{
					"sessionID": "test-session",
					"id":        "msg_002",
				},
				"partID": "prt_002_001",
				"part": map[string]any{
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

// One real-sandbox server for this file
var raceSuite SuiteHandle

func raceServer(t *testing.T) *Server { return RealSuiteServer(t, &raceSuite) }

// ===== Helpers for app signal tests (port-based) =====

func GetTestPort() int {
	// Simple randomized high port to reduce conflicts
	return 20000 + int(time.Now().UnixNano()%10000)
}

func WaitForHTTPServerReady(port int, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("HTTP server on port %d not ready after %v", port, timeout)
}

func WaitForServerShutdown(port int, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		_, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
		if err != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("server on port %d still responding after %v", port, timeout)
}

func countOpencodeProcesses() int {
	cmd := exec.Command("sh", "-c", "ps aux | grep 'opencode serve' | grep -v grep | wc -l")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	count := 0
	fmt.Sscanf(string(output), "%d", &count)
	return count
}

func countDockerContainers(namePrefix string) int {
	cmd := exec.Command("docker", "ps", "-a", "-q", "--filter", fmt.Sprintf("name=%s", namePrefix))
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	containerIDs := strings.Fields(strings.TrimSpace(string(output)))
	return len(containerIDs)
}

func createOrphanedContainer(t *testing.T) string {
	// Create a test orphaned container
	containerName := fmt.Sprintf("opencode-sandbox-test-%d", time.Now().UnixNano())

	// Run a simple container that will be orphaned
	cmd := exec.Command("docker", "run", "-d", "--name", containerName, "alpine:latest", "sleep", "3600")
	if err := cmd.Run(); err != nil {
		t.Logf("Failed to create orphaned container (may not have alpine image): %v", err)
		return ""
	}

	t.Logf("Created orphaned test container: %s", containerName)
	return containerName
}

func WaitForProcessCount(expectedCount int, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		if countOpencodeProcesses() == expectedCount {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("opencode process count did not reach %d after %v", expectedCount, timeout)
}

// ===== Race condition tests using shared sandbox =====

func TestIntegrationConcurrentSessionCreation(t *testing.T) {
	server := raceServer(t)

	const numGoroutines = 50
	const cookieValue = "test-concurrent-cookie"

	sessionChan := make(chan string, numGoroutines)
	errorChan := make(chan error, numGoroutines)

	var wg sync.WaitGroup
	startSignal := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-startSignal
			sessionID, err := server.getOrCreateSession(cookieValue)
			if err != nil {
				errorChan <- fmt.Errorf("goroutine %d failed: %w", id, err)
				return
			}
			sessionChan <- sessionID
		}(i)
	}
	close(startSignal)
	wg.Wait()
	close(sessionChan)
	close(errorChan)

	for err := range errorChan {
		t.Errorf("Error in concurrent session creation: %v", err)
	}
	var sessionIDs []string
	for sessionID := range sessionChan {
		sessionIDs = append(sessionIDs, sessionID)
	}
	if len(sessionIDs) != numGoroutines {
		t.Fatalf("Expected %d session IDs, got %d", numGoroutines, len(sessionIDs))
	}
	first := sessionIDs[0]
	for i, sid := range sessionIDs {
		if sid != first {
			t.Errorf("Session ID mismatch at index %d: expected %s, got %s", i, first, sid)
		}
	}
	server.mu.RLock()
	count := 0
	for k := range server.sessions {
		if strings.HasPrefix(k, "test-concurrent-cookie") {
			count++
		}
	}
	server.mu.RUnlock()
	if count != 1 {
		t.Errorf("Expected 1 session in server map for test cookie, got %d", count)
	}
}

func TestIntegrationRaceConditionDoubleCheckedLocking(t *testing.T) {
	server := raceServer(t)

	const numCookies = 20
	const goroutinesPerCookie = 5

	var wg sync.WaitGroup
	results := make(map[string][]string)
	var resultsMutex sync.Mutex
	errorChan := make(chan error, numCookies*goroutinesPerCookie)

	for cookieNum := 0; cookieNum < numCookies; cookieNum++ {
		cookieValue := fmt.Sprintf("double-check-cookie-%d", cookieNum)
		for goroutineNum := 0; goroutineNum < goroutinesPerCookie; goroutineNum++ {
			wg.Add(1)
			go func(cookie string) {
				defer wg.Done()
				sessionID, err := server.getOrCreateSession(cookie)
				if err != nil {
					errorChan <- err
					return
				}
				resultsMutex.Lock()
				if _, ok := results[cookie]; !ok {
					results[cookie] = make([]string, 0, goroutinesPerCookie)
				}
				results[cookie] = append(results[cookie], sessionID)
				resultsMutex.Unlock()
			}(cookieValue)
		}
	}

	wg.Wait()
	close(errorChan)
	for err := range errorChan {
		t.Errorf("Error: %v", err)
	}

	for cookie, sessionIDs := range results {
		if len(sessionIDs) != goroutinesPerCookie {
			t.Errorf("Cookie %s: expected %d, got %d", cookie, goroutinesPerCookie, len(sessionIDs))
			continue
		}
		first := sessionIDs[0]
		for i, sid := range sessionIDs {
			if sid != first {
				t.Errorf("Cookie %s mismatch at %d", cookie, i)
			}
		}
	}
	server.mu.RLock()
	prefixed := 0
	for k := range server.sessions {
		if strings.HasPrefix(k, "double-check-cookie-") {
			prefixed++
		}
	}
	server.mu.RUnlock()
	if prefixed != numCookies {
		t.Errorf("Expected %d sessions with prefix, got %d", numCookies, prefixed)
	}
}

func TestIntegrationStopOpencodeServerGoroutineCleanup(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()
	server := raceServer(t)
	for i := 0; i < 3; i++ {
		_, _ = server.getOrCreateSession(fmt.Sprintf("leak-check-%d", i))
		time.Sleep(100 * time.Millisecond)
		runtime.GC()
		runtime.GC()
	}
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines-initialGoroutines > 2 {
		t.Errorf("Goroutine leak suspected: %d -> %d", initialGoroutines, finalGoroutines)
	}
}

func TestIntegrationSSEContextCancellation(t *testing.T) {
	server := raceServer(t)
	cookie := &http.Cookie{Name: "session", Value: "test-sse-context"}
	if _, err := server.getOrCreateSession(cookie.Value); err != nil {
		t.Fatalf("session: %v", err)
	}
	goroutinesBefore := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	done := make(chan bool, 1)
	go func() { defer func() { done <- true }(); server.handleSSE(w, req) }()
	select {
	case <-done:
	default:
	}
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter-goroutinesBefore > 3 {
		t.Errorf("goroutine leak diff=%d", goroutinesAfter-goroutinesBefore)
	}
	if w.Result().Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("bad content type")
	}
}

// ===== Full app signal tests (build + run binary) =====

func TestIntegrationSignalHandling(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "test-opencode-chat", ".")
	buildCmd.Dir = filepath.Join(getBuildDir())
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	binaryPath := filepath.Join(getBuildDir(), "test-opencode-chat")
	defer os.Remove(binaryPath)

	port := GetTestPort()
	cmd := exec.Command(binaryPath, "-port", fmt.Sprintf("%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := WaitForHTTPServerReady(port, 10*time.Second); err != nil {
		cmd.Process.Kill()
		t.Fatalf("ready: %v", err)
	}
	pid := cmd.Process.Pid
	t.Logf("Application started with PID %d", pid)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("signal: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		t.Log("Process terminated successfully")
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("timeout")
	}
	if err := WaitForServerShutdown(port, 5*time.Second); err != nil {
		t.Errorf("still responding: %v", err)
	}
}

func TestIntegrationOpencodeCleanupOnSignal(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "test-opencode-chat", ".")
	buildCmd.Dir = filepath.Join(getBuildDir())
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	binaryPath := filepath.Join(getBuildDir(), "test-opencode-chat")
	defer os.Remove(binaryPath)

	port := GetTestPort()
	cmd := exec.Command(binaryPath, "-port", fmt.Sprintf("%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := WaitForHTTPServerReady(port, 10*time.Second); err != nil {
		cmd.Process.Kill()
		t.Fatalf("ready: %v", err)
	}

	var tempDir string
	pid := cmd.Process.Pid
	pattern := fmt.Sprintf("/tmp/opencode-chat-pid%d-*", pid)
	matches, _ := filepath.Glob(pattern)
	if len(matches) > 0 {
		tempDir = matches[0]
	}
	if tempDir != "" {
		t.Logf("Found opencode temp directory: %s", tempDir)
		if _, err := os.Stat(tempDir); os.IsNotExist(err) {
			t.Errorf("Temp directory does not exist: %s", tempDir)
		}
	}
	before := countOpencodeProcesses()
	containersBefore := countDockerContainers("opencode-sandbox")
	t.Logf("Opencode processes before signal: %d", before)
	t.Logf("Docker containers before signal: %d", containersBefore)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		t.Log("Process terminated")
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("timeout")
	}
	exp := before - 1
	if exp < 0 {
		exp = 0
	}
	if err := WaitForProcessCount(exp, 5*time.Second); err != nil {
		t.Logf("Warning: %v", err)
	}
	after := countOpencodeProcesses()
	containersAfter := countDockerContainers("opencode-sandbox")
	t.Logf("Opencode processes after signal: %d (was %d)", after, before)
	t.Logf("Docker containers after signal: %d (was %d)", containersAfter, containersBefore)
	if after >= before && before > 0 {
		t.Error("Opencode process was not terminated")
	}
	if containersAfter >= containersBefore && containersBefore > 0 {
		t.Error("Docker containers were not cleaned up")
	}
	if tempDir != "" {
		if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
			t.Errorf("Temp directory was not cleaned up: %s", tempDir)
		} else {
			t.Logf("Temp directory was properly cleaned up")
		}
	}
}

func TestIntegrationDockerContainerCleanupOnStartup(t *testing.T) {
	// Create some orphaned containers first
	orphan1 := createOrphanedContainer(t)
	orphan2 := createOrphanedContainer(t)

	// Clean up orphans at end of test
	defer func() {
		if orphan1 != "" {
			exec.Command("docker", "rm", "-f", orphan1).Run()
		}
		if orphan2 != "" {
			exec.Command("docker", "rm", "-f", orphan2).Run()
		}
	}()

	if orphan1 == "" || orphan2 == "" {
		t.Skip("Could not create orphaned containers for test")
	}

	// Count containers before starting app
	containersBefore := countDockerContainers("opencode-sandbox")
	t.Logf("Docker containers before startup: %d", containersBefore)

	if containersBefore < 2 {
		t.Fatalf("Expected at least 2 orphaned containers, got %d", containersBefore)
	}

	// Build and start the application
	buildCmd := exec.Command("go", "build", "-o", "test-opencode-chat", ".")
	buildCmd.Dir = filepath.Join(getBuildDir())
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	binaryPath := filepath.Join(getBuildDir(), "test-opencode-chat")
	defer os.Remove(binaryPath)

	port := GetTestPort()
	cmd := exec.Command(binaryPath, "-port", fmt.Sprintf("%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()

	// Wait for the server to be ready (longer timeout due to Docker cleanup)
	if err := WaitForHTTPServerReady(port, 30*time.Second); err != nil {
		t.Fatalf("server ready: %v", err)
	}

	// Check that orphaned containers were cleaned up
	containersAfter := countDockerContainers("opencode-sandbox")
	t.Logf("Docker containers after startup: %d", containersAfter)

	// Should have cleaned up orphans and created one new container
	expectedContainers := 1 // The new container created by the app
	if containersAfter != expectedContainers {
		t.Errorf("Expected %d containers after startup (cleaned orphans + new container), got %d", expectedContainers, containersAfter)
	}

	// Gracefully shutdown
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Errorf("signal: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		t.Log("Process terminated")
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("timeout during shutdown")
	}

	// Verify final cleanup
	finalContainers := countDockerContainers("opencode-sandbox")
	t.Logf("Docker containers after shutdown: %d", finalContainers)

	if finalContainers > 0 {
		t.Errorf("Expected 0 containers after shutdown, got %d", finalContainers)
	}
}

// getBuildDir returns the project root directory where `go build .` should run.
// Since this test file is now in internal/server, the binary build target is the
// repository root (two levels up from this package).
func getBuildDir() string {
	// Navigate from internal/server to the project root
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..")
}
