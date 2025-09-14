package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// One real-sandbox server for this file
var raceSuite SuiteHandle

func raceServer(t *testing.T) *Server { return RealSuiteServer(t, &raceSuite) }

// ===== Helpers for app signal tests (port-based) =====

func GetTestPort() int {
	// Simple randomized high port to reduce conflicts
	return 20000 + int(time.Now().UnixNano()%10000)
}

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

func WaitForServerShutdown(port int, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		_, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
		if err != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("server on port %d still responding after %v", port, timeout)
}

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

func countDockerContainers(namePrefix string) int {
	cmd := exec.Command("docker", "ps", "-a", "-q", "--filter", fmt.Sprintf("name=%s", namePrefix))
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	containerIDs := strings.Fields(strings.TrimSpace(string(output)))
	return len(containerIDs)
}

func createOrphanedContainer(t *testing.T) string {
	// Create a test orphaned container
	containerName := fmt.Sprintf("opencode-sandbox-test-%d", time.Now().UnixNano())

	// Run a simple container that will be orphaned
	cmd := exec.Command("docker", "run", "-d", "--name", containerName, "alpine:latest", "sleep", "3600")
	if err := cmd.Run(); err != nil {
		t.Logf("Failed to create orphaned container (may not have alpine image): %v", err)
		return ""
	}

	t.Logf("Created orphaned test container: %s", containerName)
	return containerName
}

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

// ===== Race condition tests using shared sandbox =====

func TestConcurrentSessionCreation(t *testing.T) {
	server := raceServer(t)

	const numGoroutines = 50
	const cookieValue = "test-concurrent-cookie"

	sessionChan := make(chan string, numGoroutines)
	errorChan := make(chan error, numGoroutines)

	var wg sync.WaitGroup
	startSignal := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-startSignal
			sessionID, err := server.getOrCreateSession(cookieValue)
			if err != nil {
				errorChan <- fmt.Errorf("goroutine %d failed: %w", id, err)
				return
			}
			sessionChan <- sessionID
		}(i)
	}
	close(startSignal)
	wg.Wait()
	close(sessionChan)
	close(errorChan)

	for err := range errorChan {
		t.Errorf("Error in concurrent session creation: %v", err)
	}
	var sessionIDs []string
	for sessionID := range sessionChan {
		sessionIDs = append(sessionIDs, sessionID)
	}
	if len(sessionIDs) != numGoroutines {
		t.Fatalf("Expected %d session IDs, got %d", numGoroutines, len(sessionIDs))
	}
	first := sessionIDs[0]
	for i, sid := range sessionIDs {
		if sid != first {
			t.Errorf("Session ID mismatch at index %d: expected %s, got %s", i, first, sid)
		}
	}
	server.mu.RLock()
	count := 0
	for k := range server.sessions {
		if strings.HasPrefix(k, "test-concurrent-cookie") {
			count++
		}
	}
	server.mu.RUnlock()
	if count != 1 {
		t.Errorf("Expected 1 session in server map for test cookie, got %d", count)
	}
}

func TestRaceConditionDoubleCheckedLocking(t *testing.T) {
	server := raceServer(t)

	const numCookies = 20
	const goroutinesPerCookie = 5

	var wg sync.WaitGroup
	results := make(map[string][]string)
	var resultsMutex sync.Mutex
	errorChan := make(chan error, numCookies*goroutinesPerCookie)

	for cookieNum := 0; cookieNum < numCookies; cookieNum++ {
		cookieValue := fmt.Sprintf("double-check-cookie-%d", cookieNum)
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
				if _, ok := results[cookie]; !ok {
					results[cookie] = make([]string, 0, goroutinesPerCookie)
				}
				results[cookie] = append(results[cookie], sessionID)
				resultsMutex.Unlock()
			}(cookieValue)
		}
	}

	wg.Wait()
	close(errorChan)
	for err := range errorChan {
		t.Errorf("Error: %v", err)
	}

	for cookie, sessionIDs := range results {
		if len(sessionIDs) != goroutinesPerCookie {
			t.Errorf("Cookie %s: expected %d, got %d", cookie, goroutinesPerCookie, len(sessionIDs))
			continue
		}
		first := sessionIDs[0]
		for i, sid := range sessionIDs {
			if sid != first {
				t.Errorf("Cookie %s mismatch at %d", cookie, i)
			}
		}
	}
	server.mu.RLock()
	prefixed := 0
	for k := range server.sessions {
		if strings.HasPrefix(k, "double-check-cookie-") {
			prefixed++
		}
	}
	server.mu.RUnlock()
	if prefixed != numCookies {
		t.Errorf("Expected %d sessions with prefix, got %d", numCookies, prefixed)
	}
}

func TestStopOpencodeServerGoroutineCleanup(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()
	server := raceServer(t)
	for i := 0; i < 3; i++ {
		_, _ = server.getOrCreateSession(fmt.Sprintf("leak-check-%d", i))
		time.Sleep(100 * time.Millisecond)
		runtime.GC()
		runtime.GC()
	}
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines-initialGoroutines > 2 {
		t.Errorf("Goroutine leak suspected: %d -> %d", initialGoroutines, finalGoroutines)
	}
}

func TestSSEContextCancellation(t *testing.T) {
	server := raceServer(t)
	cookie := &http.Cookie{Name: "session", Value: "test-sse-context"}
	if _, err := server.getOrCreateSession(cookie.Value); err != nil {
		t.Fatalf("session: %v", err)
	}
	goroutinesBefore := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	done := make(chan bool, 1)
	go func() { defer func() { done <- true }(); server.handleSSE(w, req) }()
	select {
	case <-done:
	default:
	}
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter-goroutinesBefore > 3 {
		t.Errorf("goroutine leak diff=%d", goroutinesAfter-goroutinesBefore)
	}
	if w.Result().Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("bad content type")
	}
}

// ===== Full app signal tests (build + run binary) =====

func TestSignalHandling(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "test-opencode-chat", ".")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	defer os.Remove("test-opencode-chat")

	port := GetTestPort()
	cmd := exec.Command("./test-opencode-chat", "-port", fmt.Sprintf("%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := WaitForHTTPServerReady(port, 10*time.Second); err != nil {
		cmd.Process.Kill()
		t.Fatalf("ready: %v", err)
	}
	pid := cmd.Process.Pid
	t.Logf("Application started with PID %d", pid)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("signal: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		t.Log("Process terminated successfully")
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("timeout")
	}
	if err := WaitForServerShutdown(port, 5*time.Second); err != nil {
		t.Errorf("still responding: %v", err)
	}
}

func TestOpencodeCleanupOnSignal(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "test-opencode-chat", ".")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	defer os.Remove("test-opencode-chat")

	port := GetTestPort()
	cmd := exec.Command("./test-opencode-chat", "-port", fmt.Sprintf("%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := WaitForHTTPServerReady(port, 10*time.Second); err != nil {
		cmd.Process.Kill()
		t.Fatalf("ready: %v", err)
	}

	var tempDir string
	pid := cmd.Process.Pid
	pattern := fmt.Sprintf("/tmp/opencode-chat-pid%d-*", pid)
	matches, _ := filepath.Glob(pattern)
	if len(matches) > 0 {
		tempDir = matches[0]
	}
	if tempDir != "" {
		t.Logf("Found opencode temp directory: %s", tempDir)
		if _, err := os.Stat(tempDir); os.IsNotExist(err) {
			t.Errorf("Temp directory does not exist: %s", tempDir)
		}
	}
	before := countOpencodeProcesses()
	containersBefore := countDockerContainers("opencode-sandbox")
	t.Logf("Opencode processes before signal: %d", before)
	t.Logf("Docker containers before signal: %d", containersBefore)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		t.Log("Process terminated")
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("timeout")
	}
	exp := before - 1
	if exp < 0 {
		exp = 0
	}
	if err := WaitForProcessCount(exp, 5*time.Second); err != nil {
		t.Logf("Warning: %v", err)
	}
	after := countOpencodeProcesses()
	containersAfter := countDockerContainers("opencode-sandbox")
	t.Logf("Opencode processes after signal: %d (was %d)", after, before)
	t.Logf("Docker containers after signal: %d (was %d)", containersAfter, containersBefore)
	if after >= before && before > 0 {
		t.Error("Opencode process was not terminated")
	}
	if containersAfter >= containersBefore && containersBefore > 0 {
		t.Error("Docker containers were not cleaned up")
	}
	if tempDir != "" {
		if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
			t.Errorf("Temp directory was not cleaned up: %s", tempDir)
		} else {
			t.Logf("✓ Temp directory was properly cleaned up")
		}
	}
}

func TestDockerContainerCleanupOnStartup(t *testing.T) {
	// Create some orphaned containers first
	orphan1 := createOrphanedContainer(t)
	orphan2 := createOrphanedContainer(t)

	// Clean up orphans at end of test
	defer func() {
		if orphan1 != "" {
			exec.Command("docker", "rm", "-f", orphan1).Run()
		}
		if orphan2 != "" {
			exec.Command("docker", "rm", "-f", orphan2).Run()
		}
	}()

	if orphan1 == "" || orphan2 == "" {
		t.Skip("Could not create orphaned containers for test")
	}

	// Count containers before starting app
	containersBefore := countDockerContainers("opencode-sandbox")
	t.Logf("Docker containers before startup: %d", containersBefore)

	if containersBefore < 2 {
		t.Fatalf("Expected at least 2 orphaned containers, got %d", containersBefore)
	}

	// Build and start the application
	buildCmd := exec.Command("go", "build", "-o", "test-opencode-chat", ".")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	defer os.Remove("test-opencode-chat")

	port := GetTestPort()
	cmd := exec.Command("./test-opencode-chat", "-port", fmt.Sprintf("%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()

	// Wait for the server to be ready (longer timeout due to Docker cleanup)
	if err := WaitForHTTPServerReady(port, 30*time.Second); err != nil {
		t.Fatalf("server ready: %v", err)
	}

	// Check that orphaned containers were cleaned up
	containersAfter := countDockerContainers("opencode-sandbox")
	t.Logf("Docker containers after startup: %d", containersAfter)

	// Should have cleaned up orphans and created one new container
	expectedContainers := 1 // The new container created by the app
	if containersAfter != expectedContainers {
		t.Errorf("Expected %d containers after startup (cleaned orphans + new container), got %d", expectedContainers, containersAfter)
	}

	// Gracefully shutdown
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Errorf("signal: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		t.Log("Process terminated")
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("timeout during shutdown")
	}

	// Verify final cleanup
	finalContainers := countDockerContainers("opencode-sandbox")
	t.Logf("Docker containers after shutdown: %d", finalContainers)

	if finalContainers > 0 {
		t.Errorf("Expected 0 containers after shutdown, got %d", finalContainers)
	}
}
