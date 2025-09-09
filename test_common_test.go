package main

import (
	"math/rand"
	"net/http"
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
	// Use ports in range 20000-30000 for tests
	return 20000 + rand.Intn(10000)
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

	// Wait for opencode to be ready
	time.Sleep(2 * time.Second)

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
				"claude-3-5-sonnet": {ID: "claude-3-5-sonnet", Name: "Claude 3.5 Sonnet"},
				"claude-3-opus":     {ID: "claude-3-opus", Name: "Claude 3 Opus"},
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
		"anthropic": "claude-3-5-sonnet",
		"openai":    "gpt-4",
	}
}
