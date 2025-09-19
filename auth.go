package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

// AuthSession represents a user's authentication session
type AuthSession struct {
	Email     string
	CreatedAt time.Time
}

// isAuthenticated checks if the user has a valid auth session
func (s *Server) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie("auth_session")
	if err != nil {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if session exists in our auth sessions map
	_, exists := s.authSessions[cookie.Value]
	return exists
}

// getAuthSession retrieves the auth session for a request
func (s *Server) getAuthSession(r *http.Request) (*AuthSession, bool) {
	cookie, err := r.Cookie("auth_session")
	if err != nil {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.authSessions[cookie.Value]
	return session, exists
}

// createAuthSession creates a new auth session for a user
func (s *Server) createAuthSession(w http.ResponseWriter, email string) string {
	sessionID := generateSessionID()

	s.mu.Lock()
	s.authSessions[sessionID] = &AuthSession{
		Email:     email,
		CreatedAt: time.Now(),
	}
	s.mu.Unlock()

	// Set the auth cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7, // 7 days
	})

	return sessionID
}

// clearAuthSession removes the auth session
func (s *Server) clearAuthSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("auth_session")
	if err == nil {
		s.mu.Lock()
		delete(s.authSessions, cookie.Value)
		s.mu.Unlock()
	}

	// Clear the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// requireAuth is middleware that ensures the user is authenticated
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthenticated(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// generateSessionID creates a secure random session ID
func generateSessionID() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}