package main

import (
	"fmt"
	"testing"
	"time"
)

func TestServerStartup(t *testing.T) {
	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test opencode server startup
	err = server.startOpencodeServer()
	if err != nil {
		t.Fatalf("Failed to start opencode server: %v", err)
	}
	defer server.stopOpencodeServer()

	// Wait for server to be ready
	if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		t.Fatalf("Opencode server not ready: %v", err)
	}

	// Test loading providers
	err = server.loadProviders()
	if err != nil {
		t.Fatalf("Failed to load providers: %v", err)
	}

	if len(server.providers) == 0 {
		t.Error("No providers loaded")
	}

	// Check for anthropic provider
	found := false
	for _, p := range server.providers {
		if p.ID == "anthropic" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Anthropic provider not found")
	}
}

func TestSessionManagement(t *testing.T) {
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

	// Test session creation
	sessionID, err := server.getOrCreateSession("test-cookie-1")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	if sessionID == "" {
		t.Error("Empty session ID returned")
	}

	// Test session retrieval
	sessionID2, err := server.getOrCreateSession("test-cookie-1")
	if err != nil {
		t.Fatalf("Failed to get existing session: %v", err)
	}

	if sessionID != sessionID2 {
		t.Errorf("Different session IDs returned: %s vs %s", sessionID, sessionID2)
	}

	// Test different cookie gets different session
	sessionID3, err := server.getOrCreateSession("test-cookie-2")
	if err != nil {
		t.Fatalf("Failed to create second session: %v", err)
	}

	if sessionID == sessionID3 {
		t.Error("Same session ID for different cookies")
	}
}

func TestConcurrentSessions(t *testing.T) {
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

	// Test concurrent session creation
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			cookie := fmt.Sprintf("concurrent-%d", id)
			_, err := server.getOrCreateSession(cookie)
			if err != nil {
				t.Errorf("Failed to create session %d: %v", id, err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Check all sessions were created
	if len(server.sessions) != 10 {
		t.Errorf("Expected 10 sessions, got %d", len(server.sessions))
	}
}
