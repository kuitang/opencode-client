package server

import (
	"opencode-chat/internal/sandbox"
)

// startOpencodeServer starts a real LocalDocker sandbox and assigns it to the server.
func (s *Server) startOpencodeServer() error {
	authConfig, err := sandbox.LoadAuthConfig()
	if err != nil {
		return err
	}
	sb := sandbox.NewLocalDockerSandbox()
	if err := sb.Start(authConfig); err != nil {
		return err
	}
	s.Sandbox = sb
	return nil
}

// stopOpencodeServer stops the sandbox if running.
func (s *Server) stopOpencodeServer() {
	if s != nil && s.Sandbox != nil {
		_ = s.Sandbox.Stop()
	}
}
