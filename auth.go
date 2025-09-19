package main

import (
	"context"
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

type authContextKey struct{}

// AuthContext holds authentication data for a request lifecycle.
type AuthContext struct {
	Session         *AuthSession
	IsAuthenticated bool
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

// withAuth attaches authentication state to the request context for downstream handlers.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.getAuthSession(r)
		authCtx := AuthContext{Session: session, IsAuthenticated: ok}
		ctx := context.WithValue(r.Context(), authContextKey{}, authCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authContext extracts authentication context from the request.
func authContext(r *http.Request) AuthContext {
	if ctx, ok := r.Context().Value(authContextKey{}).(AuthContext); ok {
		return ctx
	}
	return AuthContext{}
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
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authCtx := authContext(r); authCtx.IsAuthenticated {
			next.ServeHTTP(w, r)
			return
		}

		if s.isAuthenticated(r) {
			next.ServeHTTP(w, r)
			return
		}

		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

// generateSessionID creates a secure random session ID
func generateSessionID() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}
