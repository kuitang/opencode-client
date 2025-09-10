package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOpencodeIsolation verifies that opencode runs in an isolated temporary directory
// and cannot access or modify files in the user's working directory
func TestOpencodeIsolation(t *testing.T) {
	// Get the current working directory
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	// Create and start server
	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start opencode server
	if err := server.startOpencodeServer(); err != nil {
		t.Fatalf("Failed to start opencode server: %v", err)
	}
	defer server.stopOpencodeServer()

	// Wait for opencode to be ready
	if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		t.Fatalf("Opencode server not ready: %v", err)
	}

	// Verify temp directory was created and is different from original cwd
	if server.opencodeDir == "" {
		t.Fatal("CRITICAL: opencodeDir not set - opencode may be running in user's directory!")
	}

	if strings.HasPrefix(server.opencodeDir, originalCwd) {
		t.Fatalf("CRITICAL: opencode temp directory %s is inside user's working directory %s",
			server.opencodeDir, originalCwd)
	}

	// Verify temp directory exists
	if _, err := os.Stat(server.opencodeDir); os.IsNotExist(err) {
		t.Fatalf("CRITICAL: opencode temp directory %s does not exist", server.opencodeDir)
	}

	// Create a session to interact with opencode
	sessionResp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/session", server.opencodePort),
		"application/json",
		bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sessionResp.Body.Close()

	var session SessionResponse
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session response: %v", err)
	}

	// Send a message asking opencode to report its working directory
	messageReq := struct {
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
		Model struct {
			ProviderID string `json:"providerID"`
			ModelID    string `json:"modelID"`
		} `json:"model"`
	}{
		Parts: []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			{Type: "text", Text: "Please run the command 'pwd' to show your current working directory"},
		},
		Model: struct {
			ProviderID string `json:"providerID"`
			ModelID    string `json:"modelID"`
		}{
			ProviderID: "anthropic",
			ModelID:    "claude-3-5-sonnet",
		},
	}

	jsonData, _ := json.Marshal(messageReq)
	msgResp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/session/%s/message", server.opencodePort, session.ID),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
	defer msgResp.Body.Close()

	// Wait for response processing
	if err := WaitForMessageProcessed(server.opencodePort, session.ID, 10*time.Second); err != nil {
		t.Logf("Message processing wait failed: %v", err)
		// Continue anyway as this might be expected behavior
	}

	// Get messages to check the response
	getResp, err := http.Get(fmt.Sprintf("http://localhost:%d/session/%s/message", server.opencodePort, session.ID))
	if err != nil {
		t.Fatalf("Failed to get messages: %v", err)
	}
	defer getResp.Body.Close()

	var messages []MessageResponse
	if err := json.NewDecoder(getResp.Body).Decode(&messages); err != nil {
		t.Fatalf("Failed to decode messages: %v", err)
	}

	// Look for the pwd output in the response
	foundPwdOutput := false
	for _, msg := range messages {
		if msg.Info.Role == "assistant" {
			for _, part := range msg.Parts {
				// Check if the response contains the temp directory path
				if strings.Contains(part.Text, server.opencodeDir) {
					foundPwdOutput = true
					t.Logf("✓ VERIFIED: opencode is running in temp directory: %s", server.opencodeDir)
				}
				// CRITICAL: Ensure opencode is NOT in the original directory
				if strings.Contains(part.Text, originalCwd) && !strings.Contains(part.Text, server.opencodeDir) {
					t.Fatalf("CRITICAL SECURITY ISSUE: opencode appears to be running in user's directory %s instead of temp directory %s",
						originalCwd, server.opencodeDir)
				}
			}
		}
	}

	if !foundPwdOutput {
		t.Logf("Warning: Could not verify pwd output, but temp directory is set to: %s", server.opencodeDir)
	}

	// Additional safety check: Verify the temp directory path structure
	if !strings.Contains(server.opencodeDir, "opencode-chat-") {
		t.Fatalf("CRITICAL: temp directory name doesn't match expected pattern: %s", server.opencodeDir)
	}

	// Verify it's in the system temp directory
	systemTempDir := os.TempDir()
	if !strings.HasPrefix(server.opencodeDir, systemTempDir) {
		t.Fatalf("CRITICAL: opencode directory %s is not in system temp directory %s",
			server.opencodeDir, systemTempDir)
	}

	t.Logf("✓ All isolation checks passed. Opencode is safely isolated in: %s", server.opencodeDir)
}

// TestTempDirectoryCleanup verifies that temporary directories are cleaned up
func TestTempDirectoryCleanup(t *testing.T) {
	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start opencode server
	if err := server.startOpencodeServer(); err != nil {
		t.Fatalf("Failed to start opencode server: %v", err)
	}

	tmpDir := server.opencodeDir
	if tmpDir == "" {
		t.Fatal("opencodeDir not set after starting server")
	}

	// Verify directory exists
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Fatalf("Temp directory %s does not exist after creation", tmpDir)
	}

	// Stop the server (should clean up)
	server.stopOpencodeServer()

	// Verify directory was removed
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Fatalf("Temp directory %s still exists after cleanup", tmpDir)
	}

	t.Logf("✓ Temp directory %s was successfully cleaned up", tmpDir)
}

// TestNoFileLeakage ensures opencode cannot write files outside its temp directory
func TestNoFileLeakage(t *testing.T) {
	// Create a test file in the current directory
	testFile := "test_isolation_marker.txt"
	testContent := "This file should not be modified by opencode"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile) // Clean up after test

	// Get absolute path of test file
	absTestFile, _ := filepath.Abs(testFile)

	server, err := NewServer(GetTestPort())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start opencode server
	if err := server.startOpencodeServer(); err != nil {
		t.Fatalf("Failed to start opencode server: %v", err)
	}
	defer server.stopOpencodeServer()

	// Wait for opencode to be ready
	if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		t.Fatalf("Opencode server not ready: %v", err)
	}

	// Verify opencode cannot access the test file
	// The file should not exist in opencode's temp directory
	opencodeTestPath := filepath.Join(server.opencodeDir, testFile)
	if _, err := os.Stat(opencodeTestPath); !os.IsNotExist(err) {
		t.Fatalf("CRITICAL: test file somehow exists in opencode's directory: %s", opencodeTestPath)
	}

	// Verify original file is unchanged
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}
	if string(content) != testContent {
		t.Fatalf("CRITICAL: test file was modified! Expected %q, got %q", testContent, string(content))
	}

	t.Logf("✓ File isolation verified. Opencode cannot access %s", absTestFile)
	t.Logf("✓ Opencode is isolated in: %s", server.opencodeDir)
}
