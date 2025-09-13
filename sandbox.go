package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
)

// AuthConfig represents the authentication configuration for different providers
// Based on the structure found in ~/.local/share/opencode/auth.json
type AuthConfig struct {
	Type    string `json:"type"`              // "api" or "oauth"
	Key     string `json:"key,omitempty"`     // API key for "api" type
	Refresh string `json:"refresh,omitempty"` // Refresh token for "oauth" type
	Access  string `json:"access,omitempty"`  // Access token for "oauth" type
	Expires int64  `json:"expires,omitempty"` // Expiration timestamp for "oauth" type
}

// Sandbox interface defines the contract for code execution sandboxes
type Sandbox interface {
	// Start initializes the sandbox with the provided API keys configuration
	Start(apiKeys map[string]AuthConfig) error

	// OpencodeURL returns the HTTP URL for accessing the OpenCode REST API
	// This handles port translation and networking for the sandbox environment
	OpencodeURL() string

	// DownloadZip creates a zip archive of the sandbox working directory
	// Returns an io.ReadCloser that can be streamed directly to HTTP responses
	// The caller is responsible for closing the returned ReadCloser
	DownloadZip() (io.ReadCloser, error)

	// Stop gracefully shuts down the sandbox and cleans up resources
	Stop() error

	// IsRunning returns true if the sandbox is currently running
	IsRunning() bool
}

// loadAuthConfig loads the OpenCode authentication configuration from the standard location
// This is a temporary implementation for testing purposes - in production, auth config
// should be provided by the user rather than reading from the local filesystem
func loadAuthConfig() (map[string]AuthConfig, error) {
	// TESTING ONLY: Parse /home/kuitang/.local/share/opencode/auth.json
	// In the future, we will get this from the user via API or configuration
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	authPath := filepath.Join(homeDir, ".local", "share", "opencode", "auth.json")
	log.Printf("TESTING: Loading auth config from %s", authPath)

	authData, err := os.ReadFile(authPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read auth config: %w", err)
	}

	var authConfig map[string]AuthConfig
	if err := json.Unmarshal(authData, &authConfig); err != nil {
		return nil, fmt.Errorf("failed to parse auth config: %w", err)
	}

	log.Printf("TESTING: Loaded auth config for %d providers", len(authConfig))
	return authConfig, nil
}

// findFreePort finds an available TCP port on the local machine
func findFreePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("failed to get TCP address")
	}

	return addr.Port, nil
}

// createAuthFile creates a temporary auth.json file for the sandbox container
func createAuthFile(authConfig map[string]AuthConfig) (string, error) {
	tmpFile, err := os.CreateTemp("", "opencode-auth-*.json")
	if err != nil {
		return "", fmt.Errorf("failed to create temp auth file: %w", err)
	}
	defer tmpFile.Close()

	authData, err := json.MarshalIndent(authConfig, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal auth config: %w", err)
	}

	if _, err := tmpFile.Write(authData); err != nil {
		return "", fmt.Errorf("failed to write auth file: %w", err)
	}

	return tmpFile.Name(), nil
}
