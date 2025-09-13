package main

import (
	"fmt"
	"strings"
)

// getSandboxPort returns the upstream port for tests that still rely on ports
func getSandboxPort(s *Server) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("nil server")
	}
	if s != nil && s.sandbox != nil {
		// Try to parse host:port from URL
		var host string
		var p int
		if _, err := fmt.Sscanf(s.sandbox.OpencodeURL(), "http://%[^:]:%d", &host, &p); err == nil {
			return p, nil
		}
		// Fallback: last colon-separated token
		parts := strings.Split(s.sandbox.OpencodeURL(), ":")
		if len(parts) > 1 {
			if _, err := fmt.Sscanf(parts[len(parts)-1], "%d", &p); err == nil {
				return p, nil
			}
		}
	}
	return 0, fmt.Errorf("no sandbox port available")
}

// getSandboxDir returns the test isolation directory if set
func getSandboxDir(s *Server) string {
	// Not available without accessing sandbox internals; return empty.
	return ""
}
