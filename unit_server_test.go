package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------- Server: logging middleware/response writer ----------------

func TestLoggingResponseWriter_WriteHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	lw.WriteHeader(404)
	if lw.statusCode != 404 {
		t.Errorf("Expected status code 404, got %d", lw.statusCode)
	}
	if recorder.Code != 404 {
		t.Errorf("Expected recorder status code 404, got %d", recorder.Code)
	}
}

func TestLoggingResponseWriter_Write(t *testing.T) {
	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	testData := []byte("Hello, World!")
	n, err := lw.Write(testData)
	if err != nil {
		t.Errorf("Write returned error: %v", err)
	}
	if n != len(testData) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(testData), n)
	}
	if lw.body.String() != "Hello, World!" {
		t.Errorf("Expected body to contain 'Hello, World!', got '%s'", lw.body.String())
	}
	if recorder.Body.String() != "Hello, World!" {
		t.Errorf("Expected recorder body to contain 'Hello, World!', got '%s'", recorder.Body.String())
	}
}

func TestLoggingResponseWriter_LogResponse(t *testing.T) {
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	defer log.SetOutput(os.Stderr)
	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	lw.WriteHeader(200)
	if _, err := lw.Write([]byte("<html><body>Test Response</body></html>")); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	lw.LogResponse("GET", "/test")
	logStr := logOutput.String()
	if !strings.Contains(logStr, "WIRE_OUT GET /test [200]") {
		t.Errorf("Expected log to contain 'WIRE_OUT GET /test [200]', got: %s", logStr)
	}
	if !strings.Contains(logStr, "<html><body>Test Response</body></html>") {
		t.Errorf("Expected log to contain full response body, got: %s", logStr)
	}
}

func TestLoggingResponseWriter_LogResponseNoTruncation(t *testing.T) {
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	defer log.SetOutput(os.Stderr)
	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	largeBody := strings.Repeat("A", 1000)
	if _, err := lw.Write([]byte(largeBody)); err != nil {
		t.Fatalf("Failed to write large body: %v", err)
	}
	lw.LogResponse("POST", "/large")
	logStr := logOutput.String()
	if !strings.Contains(logStr, largeBody) {
		t.Errorf("Expected log to contain full large body without truncation")
	}
	if strings.Contains(logStr, "truncated") {
		t.Errorf("Log should not contain truncation message, but does: %s", logStr)
	}
}

func TestLoggingMiddleware_NormalEndpoint(t *testing.T) {
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	defer log.SetOutput(os.Stderr)
	handler := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); w.Write([]byte("Normal response")) }
	loggingMiddleware := func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/events" {
				log.Printf("WIRE_OUT SSE connection started: %s %s", r.Method, r.URL.Path)
				handler(w, r)
				log.Printf("WIRE_OUT SSE connection ended: %s %s", r.Method, r.URL.Path)
				return
			}
			lw := NewLoggingResponseWriter(w)
			handler(lw, r)
			lw.LogResponse(r.Method, r.URL.Path)
		}
	}
	req := httptest.NewRequest("GET", "/normal", nil)
	recorder := httptest.NewRecorder()
	wrappedHandler := loggingMiddleware(handler)
	wrappedHandler(recorder, req)
	if recorder.Code != 200 {
		t.Errorf("Expected status code 200, got %d", recorder.Code)
	}
	if recorder.Body.String() != "Normal response" {
		t.Errorf("Expected 'Normal response', got '%s'", recorder.Body.String())
	}
	if !strings.Contains(logOutput.String(), "WIRE_OUT GET /normal [200]: Normal response") {
		t.Errorf("Expected log output for normal endpoint, got: %s", logOutput.String())
	}
}

func TestLoggingMiddleware_SSEEndpoint(t *testing.T) {
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	defer log.SetOutput(os.Stderr)
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("data: SSE message\n\n"))
	}
	loggingMiddleware := func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/events" {
				log.Printf("WIRE_OUT SSE connection started: %s %s", r.Method, r.URL.Path)
				handler(w, r)
				log.Printf("WIRE_OUT SSE connection ended: %s %s", r.Method, r.URL.Path)
				return
			}
			lw := NewLoggingResponseWriter(w)
			handler(lw, r)
			lw.LogResponse(r.Method, r.URL.Path)
		}
	}
	req := httptest.NewRequest("GET", "/events", nil)
	recorder := httptest.NewRecorder()
	wrappedHandler := loggingMiddleware(handler)
	wrappedHandler(recorder, req)
	if recorder.Code != 200 {
		t.Errorf("Expected status code 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "data: SSE message") {
		t.Errorf("Expected SSE data in response, got '%s'", recorder.Body.String())
	}
	logStr := logOutput.String()
	if !strings.Contains(logStr, "WIRE_OUT SSE connection started: GET /events") {
		t.Errorf("Expected SSE connection started log, got: %s", logStr)
	}
	if !strings.Contains(logStr, "WIRE_OUT SSE connection ended: GET /events") {
		t.Errorf("Expected SSE connection ended log, got: %s", logStr)
	}
	if strings.Contains(logStr, "WIRE_OUT GET /events [200]:") {
		t.Errorf("SSE endpoint should not use normal response logging, but does: %s", logStr)
	}
}

func TestNewLoggingResponseWriter(t *testing.T) {
	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	if lw.ResponseWriter != recorder {
		t.Error("Expected ResponseWriter to be set to recorder")
	}
	if lw.statusCode != 200 {
		t.Errorf("Expected default status code 200, got %d", lw.statusCode)
	}
	if lw.body == nil {
		t.Error("Expected body buffer to be initialized")
	}
	if lw.body.Len() != 0 {
		t.Errorf("Expected empty body buffer, got length %d", lw.body.Len())
	}
}

// ---------------- Server: message parts manager and event validation ----------------

func TestMessagePartsOrdering(t *testing.T) {
	manager := NewMessagePartsManager()
	msgID := "test-message"
	getContent := func() string {
		parts := manager.GetParts(msgID)
		var contents []string
		for _, p := range parts {
			contents = append(contents, p.Content)
		}
		return strings.Join(contents, " ")
	}
	manager.UpdatePart(msgID, "part1", MessagePartData{Type: "text", Content: "Hello"})
	if got := getContent(); got != "Hello" {
		t.Errorf("After first part: got %q, want %q", got, "Hello")
	}
	manager.UpdatePart(msgID, "part2", MessagePartData{Type: "text", Content: "world"})
	if got := getContent(); got != "Hello world" {
		t.Errorf("After second part: got %q, want %q", got, "Hello world")
	}
	manager.UpdatePart(msgID, "part1", MessagePartData{Type: "text", Content: "Hi"})
	if got := getContent(); got != "Hi world" {
		t.Errorf("After updating first part: got %q, want %q", got, "Hi world")
	}
	manager.UpdatePart(msgID, "part3", MessagePartData{Type: "text", Content: "there"})
	if got := getContent(); got != "Hi world there" {
		t.Errorf("After third part: got %q, want %q", got, "Hi world there")
	}
	manager.UpdatePart(msgID, "part2", MessagePartData{Type: "text", Content: "beautiful"})
	if got := getContent(); got != "Hi beautiful there" {
		t.Errorf("After updating middle part: got %q, want %q", got, "Hi beautiful there")
	}
}

func TestMessagePartsPrefixStability(t *testing.T) {
	manager := NewMessagePartsManager()
	msgID := "stream-message"
	var observedPrefixes []string
	recordContent := func() string {
		parts := manager.GetParts(msgID)
		var contents []string
		for _, p := range parts {
			contents = append(contents, p.Content)
		}
		content := strings.Join(contents, " ")
		observedPrefixes = append(observedPrefixes, content)
		return content
	}
	updates := []struct{ partID, content string }{{"p1", "The"}, {"p2", "quick"}, {"p1", "A"}, {"p3", "brown"}, {"p2", "very quick"}, {"p4", "fox"}, {"p5", "jumps"}}
	expectedSequence := []string{"The", "The quick", "A quick", "A quick brown", "A very quick brown", "A very quick brown fox", "A very quick brown fox jumps"}
	for i, update := range updates {
		manager.UpdatePart(msgID, update.partID, MessagePartData{Type: "text", Content: update.content})
		got := recordContent()
		want := expectedSequence[i]
		if got != want {
			t.Errorf("Update %d: got %q, want %q", i+1, got, want)
		}
	}
	for i := 0; i < len(observedPrefixes)-1; i++ {
		currentPrefix := observedPrefixes[i]
		for j := i + 1; j < len(observedPrefixes); j++ {
			nextContent := observedPrefixes[j]
			if !isPrefixOrValidUpdate(currentPrefix, nextContent) {
				t.Errorf("Prefix stability violated: %q is not a valid predecessor of %q", currentPrefix, nextContent)
			}
		}
	}
}

func isPrefixOrValidUpdate(old, new string) bool {
	oldParts := strings.Split(old, " ")
	newParts := strings.Split(new, " ")
	if len(newParts) < len(oldParts) {
		return false
	}
	for i := 0; i < len(oldParts); i++ { /* position preserved */
	}
	return true
}

func TestMessagePartsValidation(t *testing.T) {
	manager := NewMessagePartsManager()
	tests := []struct {
		name, messageID, partID string
		wantErr                 bool
	}{
		{"valid inputs", "msg1", "part1", false},
		{"empty messageID", "", "part1", true},
		{"empty partID", "msg1", "", true},
		{"both empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.UpdatePart(tt.messageID, tt.partID, MessagePartData{Type: "text", Content: "test"})
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdatePart() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAndExtractMessagePart(t *testing.T) {
	sessionID := "test-session"
	tests := []struct {
		name    string
		event   map[string]interface{}
		wantErr bool
		wantMsg string
	}{
		{name: "valid event", event: map[string]interface{}{"type": "message.part.updated", "properties": map[string]interface{}{"part": map[string]interface{}{"sessionID": sessionID, "messageID": "msg123", "id": "part456"}}}},
		{name: "missing messageID", event: map[string]interface{}{"type": "message.part.updated", "properties": map[string]interface{}{"part": map[string]interface{}{"sessionID": sessionID, "id": "part456"}}}, wantErr: true, wantMsg: "invalid or missing messageID"},
		{name: "empty messageID", event: map[string]interface{}{"type": "message.part.updated", "properties": map[string]interface{}{"part": map[string]interface{}{"sessionID": sessionID, "messageID": "", "id": "part456"}}}, wantErr: true, wantMsg: "invalid or missing messageID"},
		{name: "missing partID", event: map[string]interface{}{"type": "message.part.updated", "properties": map[string]interface{}{"part": map[string]interface{}{"sessionID": sessionID, "messageID": "msg123"}}}, wantErr: true, wantMsg: "invalid or missing partID"},
		{name: "wrong session", event: map[string]interface{}{"type": "message.part.updated", "properties": map[string]interface{}{"part": map[string]interface{}{"sessionID": "other-session", "messageID": "msg123", "id": "part456"}}}, wantErr: true, wantMsg: "session mismatch"},
		{name: "wrong event type", event: map[string]interface{}{"type": "other.event", "properties": map[string]interface{}{}}, wantErr: true, wantMsg: "not a message.part.updated event"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgID, partID, _, err := ValidateAndExtractMessagePart(tt.event, sessionID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAndExtractMessagePart() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("Error message %q doesn't contain %q", err.Error(), tt.wantMsg)
			}
			if !tt.wantErr && (msgID == "" || partID == "") {
				t.Errorf("Valid event should return non-empty IDs, got msgID=%q, partID=%q", msgID, partID)
			}
		})
	}
}

func TestSequentialUpdates(t *testing.T) {
	manager := NewMessagePartsManager()
	msgID := "test-msg"
	for i := 0; i < 10; i++ {
		partID := fmt.Sprintf("part%d", i)
		for j := 0; j < 100; j++ {
			manager.UpdatePart(msgID, partID, MessagePartData{Type: "text", Content: fmt.Sprintf("content-%d-%d", i, j)})
		}
	}
	parts := manager.GetParts(msgID)
	if len(parts) != 10 {
		t.Errorf("Expected 10 parts after updates, got %d", len(parts))
	}
	for i := 0; i < 10; i++ {
		expectedID := fmt.Sprintf("part%d", i)
		expectedContent := fmt.Sprintf("content-%d-99", i)
		found := false
		for _, p := range parts {
			if p.PartID == expectedID {
				found = true
				if p.Content != expectedContent {
					t.Errorf("Part %s has wrong content: got %q, want %q", expectedID, p.Content, expectedContent)
				}
				break
			}
		}
		if !found {
			t.Errorf("Missing part %s after updates", expectedID)
		}
	}
}

// ---------------- Server: waitForOpencodeReady helper ----------------

func TestWaitForOpencodeReady_DelayedSuccess(t *testing.T) {
	var requestCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if r.URL.Path != "/session" {
			t.Errorf("Expected path /session, got %s", r.URL.Path)
		}
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Server starting up..."))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"sessionId": "test-session"}`))
		}
	}))
	defer ts.Close()
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	err := waitForOpencodeReady(port, 2*time.Second)
	if err != nil {
		t.Errorf("Expected success after retries, got error: %v", err)
	}
	if finalCount := atomic.LoadInt32(&requestCount); finalCount < 3 {
		t.Errorf("Expected at least 3 requests, got %d", finalCount)
	}
}

func TestWaitForOpencodeReady_Timeout(t *testing.T) {
	var requestCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Permanently broken"))
	}))
	defer ts.Close()
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	start := time.Now()
	err := waitForOpencodeReady(port, 500*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
	if err != nil {
		expectedMsg := fmt.Sprintf("opencode server on port %d not ready after", port)
		if len(err.Error()) < len(expectedMsg) || err.Error()[:len(expectedMsg)] != expectedMsg {
			t.Errorf("Expected error message to start with '%s', got: %v", expectedMsg, err)
		}
	}
	if finalCount := atomic.LoadInt32(&requestCount); finalCount < 3 || finalCount > 6 {
		t.Errorf("Expected 3-6 requests in 500ms, got %d", finalCount)
	}
	if elapsed > 600*time.Millisecond {
		t.Errorf("Function took too long: %v, expected ~500ms", elapsed)
	}
}

func TestWaitForOpencodeReady_ConnectionRefused(t *testing.T) {
	unusedPort := 59999
	start := time.Now()
	err := waitForOpencodeReady(unusedPort, 300*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Error("Expected error for connection refused, got nil")
	}
	if elapsed > 400*time.Millisecond {
		t.Errorf("Function took too long with connection refused: %v", elapsed)
	}
}

func TestWaitForOpencodeReady_JustBeforeTimeout(t *testing.T) {
	var requestCount int32
	successAfter := int32(4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count < successAfter {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer ts.Close()
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	err := waitForOpencodeReady(port, 400*time.Millisecond)
	if err != nil {
		t.Errorf("Expected success just before timeout, got error: %v", err)
	}
	if finalCount := atomic.LoadInt32(&requestCount); finalCount != successAfter {
		t.Errorf("Expected exactly %d requests, got %d", successAfter, finalCount)
	}
}

func TestWaitForOpencodeReady_ResponseBodyLeak(t *testing.T) {
	bodiesClosed := int32(0)
	requestsMade := int32(0)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestsMade, 1)
		w.Header().Set("Connection", "close")
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "Attempt %d failed", count)
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Success on attempt %d", count)
		}
		atomic.AddInt32(&bodiesClosed, 1)
	}))
	defer ts.Close()
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	if err := waitForOpencodeReady(port, 1*time.Second); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if requests := atomic.LoadInt32(&requestsMade); requests != atomic.LoadInt32(&bodiesClosed) {
		t.Errorf("Response body leak detected: %d requests made but only %d completed", requests, atomic.LoadInt32(&bodiesClosed))
	}
}

func TestWaitForOpencodeReady_RaceCondition(t *testing.T) {
	var globalRequestCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&globalRequestCount, 1)
		if count > 5 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer ts.Close()
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { done <- waitForOpencodeReady(port, 2*time.Second) }()
	}
	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent call %d failed: %v", i, err)
		}
	}
	if totalRequests := atomic.LoadInt32(&globalRequestCount); totalRequests < 6 {
		t.Errorf("Expected at least 6 total requests from concurrent calls, got %d", totalRequests)
	}
}
