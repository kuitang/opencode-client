package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

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
	defer log.SetOutput(os.Stderr) // Restore default stderr output

	recorder := httptest.NewRecorder()
	lw := NewLoggingResponseWriter(recorder)
	lw.WriteHeader(200)
	_, err := lw.Write([]byte("<html><body>Test Response</body></html>"))
	if err != nil {
		t.Fatalf("Failed to write to logging response writer: %v", err)
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

	// Create a large response body (>500 characters)
	largeBody := strings.Repeat("A", 1000)
	_, err := lw.Write([]byte(largeBody))
	if err != nil {
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

	// Create a simple handler that writes a response
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("Normal response"))
	}

	// Create the logging middleware
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

	// Check response
	if recorder.Code != 200 {
		t.Errorf("Expected status code 200, got %d", recorder.Code)
	}
	if recorder.Body.String() != "Normal response" {
		t.Errorf("Expected 'Normal response', got '%s'", recorder.Body.String())
	}

	// Check log output
	logStr := logOutput.String()
	if !strings.Contains(logStr, "WIRE_OUT GET /normal [200]: Normal response") {
		t.Errorf("Expected log output for normal endpoint, got: %s", logStr)
	}
}

func TestLoggingMiddleware_SSEEndpoint(t *testing.T) {
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	defer log.SetOutput(os.Stderr)

	// Create an SSE handler
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("data: SSE message\n\n"))
	}

	// Create the logging middleware
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

	// Check response
	if recorder.Code != 200 {
		t.Errorf("Expected status code 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "data: SSE message") {
		t.Errorf("Expected SSE data in response, got '%s'", recorder.Body.String())
	}

	// Check log output - should have SSE-specific logging, not normal response logging
	logStr := logOutput.String()
	if !strings.Contains(logStr, "WIRE_OUT SSE connection started: GET /events") {
		t.Errorf("Expected SSE connection started log, got: %s", logStr)
	}
	if !strings.Contains(logStr, "WIRE_OUT SSE connection ended: GET /events") {
		t.Errorf("Expected SSE connection ended log, got: %s", logStr)
	}
	// Should NOT contain normal response logging
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
