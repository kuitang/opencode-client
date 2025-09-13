//go:build !prod
// +build !prod

package main

// TestServerWrapper wraps Server to provide backward-compatible fields for tests
type TestServerWrapper struct {
	*Server
	opencodePort int
	opencodeDir  string
}

// WrapForTest wraps a Server with test-compatible fields
func WrapForTest(s *Server) *TestServerWrapper {
	port, _ := getSandboxPort(s)
	return &TestServerWrapper{
		Server:       s,
		opencodePort: port,
		opencodeDir:  getSandboxDir(s),
	}
}

// UpdateTestFields updates the test fields from current sandbox state
func (w *TestServerWrapper) UpdateTestFields() {
	if w.Server != nil && w.Server.sandbox != nil {
		port, _ := getSandboxPort(w.Server)
		w.opencodePort = port
		w.opencodeDir = getSandboxDir(w.Server)
	}
}
