package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"
)

// TestConfig holds common test configuration
type TestConfig struct {
	BasePort int
}

// NewTestConfig creates a test configuration with a unique port
func NewTestConfig(basePort int) *TestConfig {
	return &TestConfig{
		BasePort: basePort,
	}
}

// GetTestPort returns a random port for testing to avoid conflicts
func GetTestPort() int {
	// Use ports 20000-30000 to avoid both system services and ephemeral ports (32768+)
	// Seed with current time to ensure randomness across runs
	rand.Seed(time.Now().UnixNano())
	return 20000 + rand.Intn(10000)
}

// WaitForOpencodeReady polls the opencode server until it's ready
func WaitForOpencodeReady(port int, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/session", port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("opencode server on port %d not ready after %v", port, timeout)
}

// WaitForMessageProcessed polls until a message appears in the session
func WaitForMessageProcessed(port int, sessionID string, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/session/%s/message", port, sessionID))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("message not processed in session %s after %v", sessionID, timeout)
}

// WaitForHTTPServerReady polls until the main HTTP server responds
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

// WaitForServerShutdown polls until the server stops responding
func WaitForServerShutdown(port int, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		_, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
		if err != nil {
			// Server is not responding - it's shut down
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("server on port %d still responding after %v", port, timeout)
}

// WaitForProcessCount polls until opencode process count reaches expected value
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

// countOpencodeProcesses counts running opencode processes
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

// WaitForSSEResponse waits for SSE response headers to be properly set
func WaitForSSEResponse(responseRecorder interface{}, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		// Check if headers are set by examining the response
		if recorder, ok := responseRecorder.(*httptest.ResponseRecorder); ok {
			if recorder.Result().Header.Get("Content-Type") == "text/event-stream" {
				return nil
			}
		}
		time.Sleep(10 * time.Millisecond) // Shorter sleep for header checking
	}
	return fmt.Errorf("SSE response headers not set after %v", timeout)
}

// StartTestServer creates and starts a test server with opencode
func StartTestServer(t *testing.T, port int) *Server {
	server, err := NewServer(port)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start opencode
	if err := server.startOpencodeServer(); err != nil {
		t.Fatalf("Failed to start opencode: %v", err)
	}

	// Wait for opencode to be ready with polling
	if err := WaitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		t.Fatalf("Opencode server not ready: %v", err)
	}

	// Load providers
	if err := server.loadProviders(); err != nil {
		t.Fatalf("Failed to load providers: %v", err)
	}

	return server
}

// CreateTestCookie creates a test HTTP cookie
func CreateTestCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		HttpOnly: true,
		Path:     "/",
	}
}

// GetTestProviders returns a set of test providers
func GetTestProviders() []Provider {
	return []Provider{
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
}

// GetTestDefaultModels returns test default model configuration
func GetTestDefaultModels() map[string]string {
	return map[string]string{
		"anthropic": "claude-3-5-haiku-20241022",
		"openai":    "gpt-4",
	}
}
