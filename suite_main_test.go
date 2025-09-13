package main

import (
	"os"
	"testing"
)

// TestMain cleans up any file-scoped suite servers/sandboxes after all tests run.
func TestMain(m *testing.M) {
	code := m.Run()
	// Cleanup
	suiteRegistryMu.Lock()
	suites := append([]*SuiteHandle(nil), suiteRegistry...)
	suiteRegistryMu.Unlock()
	for _, h := range suites {
		if h != nil && h.server != nil {
			h.server.stopOpencodeServer()
		}
	}
	os.Exit(code)
}
