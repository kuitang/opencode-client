package main

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

// SuiteHandle manages a single shared Server + Sandbox per test file.
type SuiteHandle struct {
	once   sync.Once
	server *Server
	err    error
}

var (
	suiteRegistryMu sync.Mutex
	suiteRegistry   []*SuiteHandle
)

// registerSuite records a suite so it can be cleaned up in TestMain.
func registerSuite(h *SuiteHandle) {
	suiteRegistryMu.Lock()
	suiteRegistry = append(suiteRegistry, h)
	suiteRegistryMu.Unlock()
}

// RealSuiteServer initializes a single real-sandbox Server for the file and returns it.
// It uses the same auth strategy as main (loadAuthConfig + LocalDockerSandbox),
// waits for the sandbox to be ready, and loads providers.
func RealSuiteServer(t testing.TB, h *SuiteHandle) *Server {
	t.Helper()
	h.once.Do(func() {
		var s *Server
		s, h.err = NewServer()
		if h.err != nil {
			return
		}
		h.err = s.startOpencodeServer()
		if h.err != nil {
			return
		}
		// Wait until /session responds
		base := s.sandbox.OpencodeURL()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := http.Get(fmt.Sprintf("%s/session", base))
			if err == nil {
				resp.Body.Close()
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		// Load providers for model selection helpers
		h.err = s.loadProviders()
		if h.err != nil {
			return
		}
		h.server = s
		registerSuite(h)
	})
	if h.err != nil {
		t.Fatalf("suite init failed: %v", h.err)
	}
	return h.server
}

// WaitForOpencodeReadyURL polls the sandbox base URL until session endpoint is up
func WaitForOpencodeReadyURL(baseURL string, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		resp, err := http.Get(fmt.Sprintf("%s/session", baseURL))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("opencode at %s not ready after %v", baseURL, timeout)
}

// WaitForMessageProcessedURL waits for a message to appear via the sandbox API using base URL
func WaitForMessageProcessedURL(baseURL, sessionID string, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		resp, err := http.Get(fmt.Sprintf("%s/session/%s/message", baseURL, sessionID))
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

// GetSupportedModelCombined returns a provider/model string supported by the current sandbox
func GetSupportedModelCombined(t *testing.T, s *Server) string {
	// Prefer sandbox-reported defaults
	if len(s.defaultModel) > 0 {
		for provider, model := range s.defaultModel {
			if provider != "" && model != "" {
				return fmt.Sprintf("%s/%s", provider, model)
			}
		}
	}
	// Fallback: first provider's first model
	for _, p := range s.providers {
		for _, m := range p.Models {
			return fmt.Sprintf("%s/%s", p.ID, m.ID)
		}
	}
	t.Fatalf("No providers/models available from sandbox")
	return ""
}
