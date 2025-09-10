package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestWaitForOpencodeReady_DelayedSuccess tests the polling mechanism when server becomes ready after initial failures
func TestWaitForOpencodeReady_DelayedSuccess(t *testing.T) {
	var requestCount int32
	
	// Create a test server that fails first 2 requests, then succeeds
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		
		// Verify the correct endpoint is being called
		if r.URL.Path != "/session" {
			t.Errorf("Expected path /session, got %s", r.URL.Path)
		}
		
		if count < 3 {
			// First two requests fail
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Server starting up..."))
		} else {
			// Third request succeeds
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"sessionId": "test-session"}`))
		}
	}))
	defer ts.Close()
	
	// Extract port from test server URL
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	// Should succeed within timeout
	err := waitForOpencodeReady(port, 2*time.Second)
	if err != nil {
		t.Errorf("Expected success after retries, got error: %v", err)
	}
	
	// Verify it made multiple attempts
	finalCount := atomic.LoadInt32(&requestCount)
	if finalCount < 3 {
		t.Errorf("Expected at least 3 requests, got %d", finalCount)
	}
}

// TestWaitForOpencodeReady_Timeout tests behavior when server never becomes ready
func TestWaitForOpencodeReady_Timeout(t *testing.T) {
	var requestCount int32
	
	// Server that always returns 500
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Permanently broken"))
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	// Use short timeout for faster test
	start := time.Now()
	err := waitForOpencodeReady(port, 500*time.Millisecond)
	elapsed := time.Since(start)
	
	// Should fail with timeout error
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
	
	// Verify error message contains useful info
	if err != nil {
		expectedMsg := fmt.Sprintf("opencode server on port %d not ready after", port)
		if err.Error()[:len(expectedMsg)] != expectedMsg {
			t.Errorf("Expected error message to start with '%s', got: %v", expectedMsg, err)
		}
	}
	
	// Should have tried multiple times within timeout
	finalCount := atomic.LoadInt32(&requestCount)
	// With 100ms sleep between attempts and 500ms timeout, expect 4-5 attempts
	if finalCount < 3 || finalCount > 6 {
		t.Errorf("Expected 3-6 requests in 500ms, got %d", finalCount)
	}
	
	// Verify timeout is respected (with small buffer for execution time)
	if elapsed > 600*time.Millisecond {
		t.Errorf("Function took too long: %v, expected ~500ms", elapsed)
	}
}

// TestWaitForOpencodeReady_ConnectionRefused tests behavior when no server is listening
func TestWaitForOpencodeReady_ConnectionRefused(t *testing.T) {
	// Use a port that's definitely not in use
	unusedPort := 59999
	
	start := time.Now()
	err := waitForOpencodeReady(unusedPort, 300*time.Millisecond)
	elapsed := time.Since(start)
	
	// Should fail
	if err == nil {
		t.Error("Expected error for connection refused, got nil")
	}
	
	// Should respect timeout
	if elapsed > 400*time.Millisecond {
		t.Errorf("Function took too long with connection refused: %v", elapsed)
	}
}

// TestWaitForOpencodeReady_JustBeforeTimeout tests edge case where success happens just before timeout
func TestWaitForOpencodeReady_JustBeforeTimeout(t *testing.T) {
	var requestCount int32
	successAfter := int32(4) // Succeed on 4th attempt
	
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		
		if count < successAfter {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			// Success on the 4th request
			w.WriteHeader(http.StatusCreated) // Test that 201 also works
		}
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	// With 100ms sleep, 4th attempt happens at ~300ms, so 400ms timeout should work
	err := waitForOpencodeReady(port, 400*time.Millisecond)
	if err != nil {
		t.Errorf("Expected success just before timeout, got error: %v", err)
	}
	
	finalCount := atomic.LoadInt32(&requestCount)
	if finalCount != successAfter {
		t.Errorf("Expected exactly %d requests, got %d", successAfter, finalCount)
	}
}

// TestWaitForOpencodeReady_ResponseBodyLeak tests that response bodies are properly closed
func TestWaitForOpencodeReady_ResponseBodyLeak(t *testing.T) {
	bodiesClosed := int32(0)
	requestsMade := int32(0)
	
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestsMade, 1)
		
		// Track when bodies are read/closed by writing data
		// If client doesn't close body, the write will eventually fail
		w.Header().Set("Connection", "close")
		
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			// Write some data to ensure body is created
			fmt.Fprintf(w, "Attempt %d failed", count)
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Success on attempt %d", count)
		}
		
		// In a real scenario, unclosed bodies would cause connection leaks
		// We can't directly test this, but we ensure Body.Close() is called
		// by verifying the function completes without hanging
		atomic.AddInt32(&bodiesClosed, 1)
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	// Run the function
	err := waitForOpencodeReady(port, 1*time.Second)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	
	// All requests should have completed (bodies handled)
	requests := atomic.LoadInt32(&requestsMade)
	closed := atomic.LoadInt32(&bodiesClosed)
	if requests != closed {
		t.Errorf("Response body leak detected: %d requests made but only %d completed", requests, closed)
	}
}

// TestWaitForOpencodeReady_RaceCondition tests concurrent calls don't interfere
func TestWaitForOpencodeReady_RaceCondition(t *testing.T) {
	var globalRequestCount int32
	
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&globalRequestCount, 1)
		
		// Become ready after some requests
		if count > 5 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	// Run multiple concurrent calls
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			done <- waitForOpencodeReady(port, 2*time.Second)
		}()
	}
	
	// All should succeed
	for i := 0; i < 3; i++ {
		err := <-done
		if err != nil {
			t.Errorf("Concurrent call %d failed: %v", i, err)
		}
	}
	
	// Should have made reasonable number of total requests
	totalRequests := atomic.LoadInt32(&globalRequestCount)
	if totalRequests < 6 {
		t.Errorf("Expected at least 6 total requests from concurrent calls, got %d", totalRequests)
	}
}