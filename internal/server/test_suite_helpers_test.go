package server

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"opencode-chat/internal/models"
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
		base := s.Sandbox.OpencodeURL()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := http.Get(fmt.Sprintf("%s/session", base))
			if err == nil {
				resp.Body.Close()
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
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

// GetSupportedModelCombined returns a provider/model string supported by the current sandbox.
func GetSupportedModelCombined(t *testing.T, s *Server) string {
	if len(s.defaultModel) > 0 {
		for provider, model := range s.defaultModel {
			if provider != "" && model != "" {
				return fmt.Sprintf("%s/%s", provider, model)
			}
		}
	}
	for _, p := range s.providers {
		for _, m := range p.Models {
			return fmt.Sprintf("%s/%s", p.ID, m.ID)
		}
	}
	t.Fatalf("No providers/models available from sandbox")
	return ""
}

// SetProviders sets provider data on the server for testing.
func (s *Server) SetProviders(providers []models.Provider, defaultModel map[string]string) {
	s.providers = providers
	s.defaultModel = defaultModel
}
