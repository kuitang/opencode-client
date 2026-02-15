package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"opencode-chat/internal/models"
)

// getSessionCookie retrieves or creates a session cookie.
func (s *Server) getSessionCookie(w http.ResponseWriter, r *http.Request) (*http.Cookie, bool) {
	cookie, err := r.Cookie("session")
	if err != nil {
		cookie = &http.Cookie{
			Name:     "session",
			Value:    fmt.Sprintf("sess_%d", time.Now().UnixNano()),
			HttpOnly: true,
			Path:     "/",
		}
		http.SetCookie(w, cookie)
		return cookie, true
	}
	return cookie, false
}

// getOrCreateSession retrieves an existing OpenCode session or creates a new one.
func (s *Server) getOrCreateSession(cookie string) (string, error) {
	s.mu.RLock()
	sessionID, exists := s.sessions[cookie]
	s.mu.RUnlock()

	if exists {
		log.Printf("getOrCreateSession: found existing session %s for cookie %s", sessionID, cookie)
		return sessionID, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionID, exists := s.sessions[cookie]; exists {
		log.Printf("getOrCreateSession: found existing session %s for cookie %s (double-check)", sessionID, cookie)
		return sessionID, nil
	}

	log.Printf("getOrCreateSession: creating new session at %s/session", s.Sandbox.OpencodeURL())

	resp, err := s.opencodePost("/session", map[string]string{})
	if err != nil {
		log.Printf("getOrCreateSession: failed to create session - %v", err)
		return "", err
	}
	defer resp.Body.Close()

	var session models.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		log.Printf("getOrCreateSession: failed to decode session response - %v", err)
		return "", err
	}

	s.sessions[cookie] = session.ID
	log.Printf("getOrCreateSession: created new session %s for cookie %s", session.ID, cookie)
	return session.ID, nil
}

// InitWorkspaceSession creates a dedicated session for workspace operations.
func (s *Server) InitWorkspaceSession() error {
	url := fmt.Sprintf("%s/session", s.Sandbox.OpencodeURL())
	log.Printf("initWorkspaceSession: creating workspace session at %s", url)

	payload := map[string]string{
		"title": "Workspace Operations",
	}
	jsonData, _ := json.Marshal(payload)

	resp, err := http.Post(
		url,
		"application/json",
		bytes.NewReader(jsonData),
	)
	if err != nil {
		log.Printf("initWorkspaceSession: failed to create session - %v", err)
		return err
	}
	defer resp.Body.Close()

	var session models.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		log.Printf("initWorkspaceSession: failed to decode session response - %v", err)
		return err
	}

	s.mu.Lock()
	s.workspaceSession = session.ID
	s.mu.Unlock()

	log.Printf("initWorkspaceSession: created workspace session %s", session.ID)
	return nil
}

// LoadProviders loads providers from the sandbox. Public wrapper for main.
func (s *Server) LoadProviders() error {
	return s.loadProviders()
}
