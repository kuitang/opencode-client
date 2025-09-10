package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestConcurrentSessionCreation tests the race condition fix in getOrCreateSession
// where multiple goroutines calling with the same cookie should only create one session
func TestConcurrentSessionCreation(t *testing.T) {
	server := StartTestServer(t, GetTestPort())
	defer server.stopOpencodeServer()

	const numGoroutines = 50
	const cookieValue = "test-concurrent-cookie"

	// Channel to collect session IDs from all goroutines
	sessionChan := make(chan string, numGoroutines)
	errorChan := make(chan error, numGoroutines)

	// Start all goroutines simultaneously
	var wg sync.WaitGroup
	startSignal := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Wait for start signal to maximize concurrency
			<-startSignal

			sessionID, err := server.getOrCreateSession(cookieValue)
			if err != nil {
				errorChan <- fmt.Errorf("goroutine %d failed: %w", id, err)
				return
			}
			sessionChan <- sessionID
		}(i)
	}

	// Release all goroutines at once
	close(startSignal)
	wg.Wait()
	close(sessionChan)
	close(errorChan)

	// Check for errors
	for err := range errorChan {
		t.Errorf("Error in concurrent session creation: %v", err)
	}

	// Collect all session IDs
	var sessionIDs []string
	for sessionID := range sessionChan {
		sessionIDs = append(sessionIDs, sessionID)
	}

	// Verify we got responses from all goroutines
	if len(sessionIDs) != numGoroutines {
		t.Fatalf("Expected %d session IDs, got %d", numGoroutines, len(sessionIDs))
	}

	// Verify all session IDs are the same (only one session was created)
	firstSessionID := sessionIDs[0]
	for i, sessionID := range sessionIDs {
		if sessionID != firstSessionID {
			t.Errorf("Session ID mismatch at index %d: expected %s, got %s", i, firstSessionID, sessionID)
		}
	}

	// Verify only one session exists in the server's map
	server.mu.RLock()
	sessionCount := len(server.sessions)
	actualSessionID := server.sessions[cookieValue]
	server.mu.RUnlock()

	if sessionCount != 1 {
		t.Errorf("Expected 1 session in server map, got %d", sessionCount)
	}

	if actualSessionID != firstSessionID {
		t.Errorf("Session in map (%s) doesn't match returned session (%s)", actualSessionID, firstSessionID)
	}

	t.Logf("✓ All %d goroutines got the same session ID: %s", numGoroutines, firstSessionID)
}

// TestRaceConditionDoubleCheckedLocking specifically tests the double-checked locking pattern
func TestRaceConditionDoubleCheckedLocking(t *testing.T) {
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

	// Test multiple different cookies concurrently to stress the locking mechanism
	const numCookies = 20
	const goroutinesPerCookie = 5

	var wg sync.WaitGroup
	results := make(map[string][]string) // cookie -> slice of session IDs
	resultsMutex := sync.Mutex{}
	errorChan := make(chan error, numCookies*goroutinesPerCookie)

	for cookieNum := 0; cookieNum < numCookies; cookieNum++ {
		cookieValue := fmt.Sprintf("double-check-cookie-%d", cookieNum)
		results[cookieValue] = make([]string, 0, goroutinesPerCookie)

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
				results[cookie] = append(results[cookie], sessionID)
				resultsMutex.Unlock()
			}(cookieValue)
		}
	}

	wg.Wait()
	close(errorChan)

	// Check for errors
	for err := range errorChan {
		t.Errorf("Error in double-checked locking test: %v", err)
	}

	// Verify each cookie has consistent session IDs
	for cookie, sessionIDs := range results {
		if len(sessionIDs) != goroutinesPerCookie {
			t.Errorf("Cookie %s: expected %d session IDs, got %d", cookie, goroutinesPerCookie, len(sessionIDs))
			continue
		}

		// All session IDs for this cookie should be the same
		firstSessionID := sessionIDs[0]
		for i, sessionID := range sessionIDs {
			if sessionID != firstSessionID {
				t.Errorf("Cookie %s: session ID mismatch at index %d: expected %s, got %s", cookie, i, firstSessionID, sessionID)
			}
		}
	}

	// Verify the server has exactly numCookies sessions
	server.mu.RLock()
	actualSessionCount := len(server.sessions)
	server.mu.RUnlock()

	if actualSessionCount != numCookies {
		t.Errorf("Expected %d sessions in server map, got %d", numCookies, actualSessionCount)
	}

	t.Logf("✓ Double-checked locking test passed: %d cookies, %d goroutines each", numCookies, goroutinesPerCookie)
}

// TestStopOpencodeServerGoroutineCleanup tests that no goroutines leak when stopping the server
func TestStopOpencodeServerGoroutineCleanup(t *testing.T) {
	// Record initial goroutine count
	initialGoroutines := runtime.NumGoroutine()

	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start and stop the server multiple times to test for leaks
	for i := 0; i < 3; i++ {
		// Start opencode
		err = server.startOpencodeServer()
		if err != nil {
			t.Fatalf("Failed to start opencode (iteration %d): %v", i, err)
		}

		// Wait for it to be ready
		if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
			server.stopOpencodeServer() // Clean up on error
			t.Fatalf("Opencode server not ready (iteration %d): %v", i, err)
		}

		// Stop the server
		server.stopOpencodeServer()

		// Give some time for cleanup
		time.Sleep(100 * time.Millisecond)

		// Force garbage collection to ensure any pending finalizers run
		runtime.GC()
		runtime.GC() // Run twice to be thorough
	}

	// Check final goroutine count after a brief wait
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	finalGoroutines := runtime.NumGoroutine()

	// Allow for some tolerance as the test framework itself may create goroutines
	goroutineDiff := finalGoroutines - initialGoroutines
	if goroutineDiff > 2 { // Allow small variance for test framework
		t.Errorf("Goroutine leak detected: started with %d, ended with %d (diff: %d)",
			initialGoroutines, finalGoroutines, goroutineDiff)
	}

	t.Logf("✓ Goroutine cleanup test passed: %d -> %d goroutines (diff: %d)",
		initialGoroutines, finalGoroutines, goroutineDiff)
}

// TestSSEContextCancellation tests that SSE handlers properly exit when context is cancelled
func TestSSEContextCancellation(t *testing.T) {
	server := StartTestServer(t, GetTestPort())
	defer server.stopOpencodeServer()

	// Create session
	cookie := &http.Cookie{Name: "session", Value: "test-sse-context"}
	_, err := server.getOrCreateSession(cookie.Value)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Record goroutines before starting SSE connections
	goroutinesBefore := runtime.NumGoroutine()

	// Test context cancellation with a short timeout to simulate client disconnect
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Create a test request with context
	req := httptest.NewRequest("GET", "/events", nil)
	req = req.WithContext(ctx)
	req.AddCookie(cookie)

	// Use a response recorder to capture the response
	w := httptest.NewRecorder()

	// Start the SSE handler in a goroutine (since it blocks)
	handlerDone := make(chan bool, 1)
	go func() {
		defer func() { handlerDone <- true }()
		server.handleSSE(w, req)
	}()

	// Wait for either the handler to complete or timeout
	select {
	case <-handlerDone:
		t.Logf("✓ SSE handler completed (likely due to context cancellation)")
	case <-time.After(3 * time.Second):
		t.Error("SSE handler did not exit within expected time after context cancellation")
	}

	// Give time for cleanup
	time.Sleep(500 * time.Millisecond)
	runtime.GC()

	// Check for goroutine leaks
	goroutinesAfter := runtime.NumGoroutine()
	goroutineDiff := goroutinesAfter - goroutinesBefore

	if goroutineDiff > 3 { // Allow some tolerance for test framework overhead
		t.Errorf("Potential goroutine leak in SSE handler: %d -> %d (diff: %d)",
			goroutinesBefore, goroutinesAfter, goroutineDiff)
	}

	// Verify response headers were set correctly before context cancellation
	resp := w.Result()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
	}

	t.Logf("✓ SSE context cancellation test passed: %d -> %d goroutines (diff: %d)",
		goroutinesBefore, goroutinesAfter, goroutineDiff)
}

// TestSSEMultipleClientDisconnects tests multiple concurrent SSE connections and disconnections
func TestSSEMultipleClientDisconnects(t *testing.T) {
	server := StartTestServer(t, GetTestPort())
	defer server.stopOpencodeServer()

	// Set up HTTP test server
	mux := http.NewServeMux()
	mux.HandleFunc("/events", server.handleSSE)
	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	const numClients = 10
	var wg sync.WaitGroup
	errors := make(chan error, numClients)

	// Record initial goroutine count
	initialGoroutines := runtime.NumGoroutine()

	// Start multiple SSE clients
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			// Create unique session for each client
			cookieValue := fmt.Sprintf("multi-sse-client-%d", clientID)
			_, err := server.getOrCreateSession(cookieValue)
			if err != nil {
				errors <- fmt.Errorf("client %d: failed to create session: %w", clientID, err)
				return
			}

			// Create context with random timeout to simulate real disconnections
			timeout := time.Duration(100+clientID*50) * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			// Connect to SSE
			req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/events", nil)
			if err != nil {
				errors <- fmt.Errorf("client %d: failed to create request: %w", clientID, err)
				return
			}

			cookie := &http.Cookie{Name: "session", Value: cookieValue}
			req.AddCookie(cookie)

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				// Context timeout is expected
				if ctx.Err() == context.DeadlineExceeded {
					t.Logf("Client %d: disconnected due to timeout as expected", clientID)
					return
				}
				errors <- fmt.Errorf("client %d: failed to connect: %w", clientID, err)
				return
			}
			defer resp.Body.Close()

			// Read until context cancellation
			buffer := make([]byte, 1024)
			for {
				_, err := resp.Body.Read(buffer)
				if err != nil {
					// Expected due to context cancellation
					break
				}
			}
		}(i)
	}

	// Wait for all clients to finish
	wg.Wait()
	close(errors)

	// Check for unexpected errors
	for err := range errors {
		t.Errorf("Unexpected error: %v", err)
	}

	// Give time for cleanup
	time.Sleep(1 * time.Second)
	runtime.GC()

	// Check for goroutine leaks
	finalGoroutines := runtime.NumGoroutine()
	goroutineDiff := finalGoroutines - initialGoroutines

	if goroutineDiff > 5 { // Allow tolerance for multiple clients
		t.Errorf("Potential goroutine leak with multiple SSE clients: %d -> %d (diff: %d)",
			initialGoroutines, finalGoroutines, goroutineDiff)
	}

	t.Logf("✓ Multiple SSE clients test passed: %d clients, %d -> %d goroutines (diff: %d)",
		numClients, initialGoroutines, finalGoroutines, goroutineDiff)
}
