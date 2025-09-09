package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestSignalHandling verifies that the application properly cleans up on SIGINT/SIGTERM
func TestSignalHandling(t *testing.T) {
	// Build the application
	buildCmd := exec.Command("go", "build", "-o", "test-opencode-chat", ".")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build application: %v", err)
	}
	defer os.Remove("test-opencode-chat")

	// Start the application
	port := GetTestPort()
	cmd := exec.Command("./test-opencode-chat", "-port", fmt.Sprintf("%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start application: %v", err)
	}
	
	// Wait for the server to be ready
	start := time.Now()
	var resp *http.Response
	for time.Since(start) < 10*time.Second {
		var err error
		resp, err = http.Get(fmt.Sprintf("http://localhost:%d/", port))
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if resp == nil {
		t.Fatalf("Server not responding after 10 seconds")
	}
	resp.Body.Close()
	
	// Get the process ID
	pid := cmd.Process.Pid
	t.Logf("Application started with PID %d", pid)
	
	// Send SIGINT (Ctrl-C)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("Failed to send SIGINT: %v", err)
	}
	
	// Wait for graceful shutdown (should complete within 10 seconds)
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	
	select {
	case err := <-done:
		if err != nil {
			// Check if it's an expected exit code (interrupted)
			if exitErr, ok := err.(*exec.ExitError); ok {
				// Exit code 2 is common for interrupt, 0 for graceful shutdown
				if exitErr.ExitCode() != 0 && exitErr.ExitCode() != 2 {
					t.Errorf("Unexpected exit code: %d", exitErr.ExitCode())
				}
			} else {
				t.Errorf("Process ended with error: %v", err)
			}
		}
		t.Log("Process terminated successfully")
	case <-time.After(10 * time.Second):
		// Force kill if not terminated
		cmd.Process.Kill()
		t.Fatal("Process did not terminate within 10 seconds after SIGINT")
	}
	
	// Verify the server is no longer responding
	time.Sleep(500 * time.Millisecond)
	_, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
	if err == nil {
		t.Error("Server is still responding after shutdown")
	}
}

// TestOpencodeCleanupOnSignal verifies opencode process and temp directory are cleaned up
func TestOpencodeCleanupOnSignal(t *testing.T) {
	// Build the application
	buildCmd := exec.Command("go", "build", "-o", "test-opencode-chat", ".")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build application: %v", err)
	}
	defer os.Remove("test-opencode-chat")

	// Start the application
	port := GetTestPort()
	cmd := exec.Command("./test-opencode-chat", "-port", fmt.Sprintf("%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start application: %v", err)
	}
	
	// Wait for the server to be ready
	start := time.Now()
	for time.Since(start) < 10*time.Second {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	
	// Find temp directory by looking for one with our PID
	var tempDir string
	pid := cmd.Process.Pid
	pattern := fmt.Sprintf("/tmp/opencode-chat-pid%d-*", pid)
	matches, _ := filepath.Glob(pattern)
	if len(matches) > 0 {
		tempDir = matches[0]
	}
	
	if tempDir != "" {
		t.Logf("Found opencode temp directory: %s", tempDir)
		
		// Verify it exists
		if _, err := os.Stat(tempDir); os.IsNotExist(err) {
			t.Errorf("Temp directory does not exist: %s", tempDir)
		}
	}
	
	// Check for opencode processes before signal
	opencodeCountBefore := countOpencodeProcesses()
	t.Logf("Opencode processes before signal: %d", opencodeCountBefore)
	
	// Send SIGTERM
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Failed to send SIGTERM: %v", err)
	}
	
	// Wait for shutdown
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	
	select {
	case <-done:
		t.Log("Process terminated")
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("Process did not terminate within 10 seconds")
	}
	
	// Give a moment for cleanup
	time.Sleep(1 * time.Second)
	
	// Check for opencode processes after signal
	opencodeCountAfter := countOpencodeProcesses()
	t.Logf("Opencode processes after signal: %d", opencodeCountAfter)
	
	if opencodeCountAfter >= opencodeCountBefore && opencodeCountBefore > 0 {
		t.Error("Opencode process was not terminated")
	}
	
	// Verify temp directory was cleaned up
	if tempDir != "" {
		if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
			t.Errorf("Temp directory was not cleaned up: %s", tempDir)
		} else {
			t.Logf("✓ Temp directory was properly cleaned up")
		}
	}
}

// Helper function to find temp directory in output
func findTempDirInOutput(output string) string {
	// Look for pattern like "Created isolated temporary directory for opencode: /tmp/opencode-chat-"
	prefix := "Created isolated temporary directory for opencode: "
	if idx := findStringInOutput(output, prefix); idx != -1 {
		start := idx + len(prefix)
		end := start
		for end < len(output) && output[end] != '\n' && output[end] != '\r' {
			end++
		}
		return output[start:end]
	}
	return ""
}

func findStringInOutput(output, search string) int {
	for i := 0; i <= len(output)-len(search); i++ {
		if output[i:i+len(search)] == search {
			return i
		}
	}
	return -1
}

// Helper function to count opencode processes
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