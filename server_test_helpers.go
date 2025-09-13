package main

// Test-only helpers to start/stop the real sandbox using the same auth strategy as main.

// startOpencodeServer starts a real LocalDocker sandbox and assigns it to the server.
// Matches the legacy test method name for minimal changes.
func (s *Server) startOpencodeServer() error {
	authConfig, err := loadAuthConfig()
	if err != nil {
		return err
	}
	sb := NewLocalDockerSandbox()
	if err := sb.Start(authConfig); err != nil {
		return err
	}
	s.sandbox = sb
	return nil
}

// stopOpencodeServer stops the sandbox if running. Matches legacy test usage.
func (s *Server) stopOpencodeServer() {
	if s != nil && s.sandbox != nil {
		_ = s.sandbox.Stop()
	}
}
