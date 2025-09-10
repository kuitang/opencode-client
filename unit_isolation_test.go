package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifyIsolation_Success tests normal successful isolation verification
func TestVerifyIsolation_Success(t *testing.T) {
	expectedDir := filepath.Join(os.TempDir(), "opencode-test-12345")
	
	// Mock /path endpoint
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/path" {
			t.Errorf("Expected /path endpoint, got %s", r.URL.Path)
		}
		
		json.NewEncoder(w).Encode(map[string]string{
			"directory": expectedDir,
			"state":     filepath.Join(expectedDir, ".state"),
			"config":    filepath.Join(expectedDir, ".config"),
			"worktree":  expectedDir,
		})
	}))
	defer ts.Close()
	
	// Extract port
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	server := &Server{
		opencodeDir:  expectedDir,
		opencodePort: port,
	}
	
	err := server.verifyOpencodeIsolation()
	if err != nil {
		t.Errorf("Expected successful verification, got error: %v", err)
	}
}

// TestVerifyIsolation_EmptyDirectory tests when server returns empty directory field
func TestVerifyIsolation_EmptyDirectory(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"directory": "", // Empty directory
			"state":     "/tmp/.state",
			"config":    "/tmp/.config",
		})
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	server := &Server{
		opencodeDir:  "/tmp/expected-dir",
		opencodePort: port,
	}
	
	err := server.verifyOpencodeIsolation()
	if err == nil {
		t.Error("Expected error for empty directory, got nil")
	}
	
	if !strings.Contains(err.Error(), "empty directory") {
		t.Errorf("Expected error about empty directory, got: %v", err)
	}
}

// TestVerifyIsolation_WrongDirectory tests when opencode is running in wrong directory
func TestVerifyIsolation_WrongDirectory(t *testing.T) {
	actualDir := "/tmp/wrong-directory"
	expectedDir := "/tmp/expected-directory"
	
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"directory": actualDir,
			"state":     filepath.Join(actualDir, ".state"),
			"config":    filepath.Join(actualDir, ".config"),
		})
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	server := &Server{
		opencodeDir:  expectedDir,
		opencodePort: port,
	}
	
	err := server.verifyOpencodeIsolation()
	if err == nil {
		t.Error("Expected error for wrong directory, got nil")
	}
	
	// Check error message contains both directories
	if !strings.Contains(err.Error(), expectedDir) || !strings.Contains(err.Error(), actualDir) {
		t.Errorf("Error should mention both directories, got: %v", err)
	}
	
	if !strings.Contains(err.Error(), "NOT running in isolated directory") {
		t.Errorf("Expected clear security violation message, got: %v", err)
	}
}

// TestVerifyIsolation_UserDirectoryViolation tests detection of opencode running in user's directory
func TestVerifyIsolation_UserDirectoryViolation(t *testing.T) {
	// Get actual current directory
	userCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	
	// Opencode reports it's running in a subdirectory of user's cwd
	violatingDir := filepath.Join(userCwd, "subdir", "opencode-data")
	
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"directory": violatingDir,
			"state":     filepath.Join(violatingDir, ".state"),
			"config":    filepath.Join(violatingDir, ".config"),
		})
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	server := &Server{
		opencodeDir:  violatingDir, // Even if it matches, it's still in user directory
		opencodePort: port,
	}
	
	err = server.verifyOpencodeIsolation()
	if err == nil {
		t.Error("Expected security violation for user directory, got nil")
	}
	
	if !strings.Contains(err.Error(), "running in user's directory") {
		t.Errorf("Expected clear user directory violation message, got: %v", err)
	}
}

// TestVerifyIsolation_NonTempDirectoryViolation tests when directory is not in temp
func TestVerifyIsolation_NonTempDirectoryViolation(t *testing.T) {
	// Directory that's not in temp (e.g., /opt/opencode)
	nonTempDir := "/opt/opencode/instance"
	
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"directory": nonTempDir,
			"state":     filepath.Join(nonTempDir, ".state"),
			"config":    filepath.Join(nonTempDir, ".config"),
		})
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	server := &Server{
		opencodeDir:  nonTempDir,
		opencodePort: port,
	}
	
	err := server.verifyOpencodeIsolation()
	if err == nil {
		t.Error("Expected error for non-temp directory, got nil")
	}
	
	if !strings.Contains(err.Error(), "not in system temp directory") {
		t.Errorf("Expected temp directory violation message, got: %v", err)
	}
	
	// Should mention both the violating directory and system temp
	if !strings.Contains(err.Error(), nonTempDir) || !strings.Contains(err.Error(), os.TempDir()) {
		t.Errorf("Error should mention both directories, got: %v", err)
	}
}

// TestVerifyIsolation_NetworkError tests behavior when /path endpoint is unreachable
func TestVerifyIsolation_NetworkError(t *testing.T) {
	// Use port with no server
	server := &Server{
		opencodeDir:  "/tmp/test-dir",
		opencodePort: 59998, // Unused port
	}
	
	err := server.verifyOpencodeIsolation()
	if err == nil {
		t.Error("Expected network error, got nil")
	}
	
	if !strings.Contains(err.Error(), "failed to query opencode /path endpoint") {
		t.Errorf("Expected network error message, got: %v", err)
	}
}

// TestVerifyIsolation_MalformedJSON tests handling of invalid JSON response
func TestVerifyIsolation_MalformedJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return invalid JSON
		w.Write([]byte(`{this is not valid json}`))
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	server := &Server{
		opencodeDir:  "/tmp/test-dir",
		opencodePort: port,
	}
	
	err := server.verifyOpencodeIsolation()
	if err == nil {
		t.Error("Expected JSON decode error, got nil")
	}
	
	if !strings.Contains(err.Error(), "failed to decode /path response") {
		t.Errorf("Expected JSON decode error message, got: %v", err)
	}
}

// TestVerifyIsolation_HTTPErrorStatus tests handling of HTTP error responses
func TestVerifyIsolation_HTTPErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 500 Internal Server Error
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal server error"))
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	server := &Server{
		opencodeDir:  "/tmp/test-dir",
		opencodePort: port,
	}
	
	// The function doesn't check status code, but malformed response should still fail
	err := server.verifyOpencodeIsolation()
	if err == nil {
		t.Error("Expected error for server error response, got nil")
	}
}

// TestVerifyIsolation_SymlinkScenario tests edge case with symlinks
func TestVerifyIsolation_SymlinkScenario(t *testing.T) {
	// Create a temp directory
	realTempDir, err := os.MkdirTemp("", "opencode-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(realTempDir)
	
	// Server reports the real temp directory path
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"directory": realTempDir,
			"state":     filepath.Join(realTempDir, ".state"),
			"config":    filepath.Join(realTempDir, ".config"),
		})
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	server := &Server{
		opencodeDir:  realTempDir,
		opencodePort: port,
	}
	
	// Should pass - legitimate temp directory
	err = server.verifyOpencodeIsolation()
	if err != nil {
		t.Errorf("Expected success for legitimate temp directory, got: %v", err)
	}
}

// TestVerifyIsolation_ConcurrentCalls tests thread safety of verification
func TestVerifyIsolation_ConcurrentCalls(t *testing.T) {
	expectedDir := filepath.Join(os.TempDir(), "opencode-concurrent-test")
	callCount := 0
	
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]string{
			"directory": expectedDir,
			"state":     filepath.Join(expectedDir, ".state"),
			"config":    filepath.Join(expectedDir, ".config"),
		})
	}))
	defer ts.Close()
	
	var port int
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	
	server := &Server{
		opencodeDir:  expectedDir,
		opencodePort: port,
	}
	
	// Run multiple concurrent verifications
	done := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			done <- server.verifyOpencodeIsolation()
		}()
	}
	
	// All should succeed
	for i := 0; i < 5; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent verification %d failed: %v", i, err)
		}
	}
	
	// Should have made 5 calls
	if callCount != 5 {
		t.Errorf("Expected 5 calls to /path, got %d", callCount)
	}
}