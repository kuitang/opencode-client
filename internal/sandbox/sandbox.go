package sandbox

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"opencode-chat/internal/models"
)

// Sandbox interface defines the contract for code execution sandboxes
type Sandbox interface {
	// Start initializes the sandbox with the provided API keys configuration
	Start(apiKeys map[string]models.AuthConfig) error

	// OpencodeURL returns the HTTP URL for accessing the OpenCode REST API
	OpencodeURL() string

	// GottyURL returns the HTTP URL for accessing the Gotty terminal interface
	// Returns empty string if terminal is not available for this sandbox type
	GottyURL() string

	// DownloadZip creates a zip archive of the sandbox working directory
	DownloadZip() (io.ReadCloser, error)

	// Stop gracefully shuts down the sandbox and cleans up resources
	Stop() error

	// IsRunning returns true if the sandbox is currently running
	IsRunning() bool

	// ContainerIP returns the IP address of the sandbox container (for Docker)
	ContainerIP() string
}

// LoadAuthConfig loads the OpenCode authentication configuration from the standard location
func LoadAuthConfig() (map[string]models.AuthConfig, error) {
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

	var authConfig map[string]models.AuthConfig
	if err := json.Unmarshal(authData, &authConfig); err != nil {
		return nil, fmt.Errorf("failed to parse auth config: %w", err)
	}

	log.Printf("TESTING: Loaded auth config for %d providers", len(authConfig))
	return authConfig, nil
}

// FindFreePort finds an available TCP port on the local machine
func FindFreePort() (int, error) {
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

// CreateAuthFile creates a temporary auth.json file for the sandbox container
func CreateAuthFile(authConfig map[string]models.AuthConfig) (string, error) {
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
