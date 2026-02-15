package server

import (
	"net/http"
	"strings"
	"time"

	"opencode-chat/internal/auth"
)

// isAuthenticated checks if the user has a valid auth session.
func (s *Server) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie("auth_session")
	if err != nil {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.authSessions[cookie.Value]
	return exists
}

// getAuthSession retrieves the auth session for a request.
func (s *Server) getAuthSession(r *http.Request) (*auth.AuthSession, bool) {
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
		authCtx := auth.AuthContext{Session: session, IsAuthenticated: ok}
		ctx := auth.SetAuthContext(r.Context(), authCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAuth is middleware that ensures the user is authenticated.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authCtx := auth.GetAuthContext(r); authCtx.IsAuthenticated {
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

// createAuthSession creates a new auth session for a user.
func (s *Server) createAuthSession(w http.ResponseWriter, email string) string {
	sessionID := auth.GenerateSessionID()

	s.mu.Lock()
	s.authSessions[sessionID] = &auth.AuthSession{
		Email:     email,
		CreatedAt: time.Now(),
	}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7,
	})

	return sessionID
}

// clearAuthSession removes the auth session.
func (s *Server) clearAuthSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("auth_session")
	if err == nil {
		s.mu.Lock()
		delete(s.authSessions, cookie.Value)
		s.mu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func (s *Server) handleLoginGET(w http.ResponseWriter, r *http.Request) {
	authCtx := auth.GetAuthContext(r)
	if authCtx.IsAuthenticated {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}

	hasContent := r.URL.Query().Get("hasContent") == "true"

	data := struct {
		HasContent bool
	}{
		HasContent: hasContent,
	}

	s.renderHTML(w, "login", data)
}

func (s *Server) handleLoginPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	email := r.FormValue("email")
	s.createAuthSession(w, email)

	hasContent := r.URL.Query().Get("hasContent") == "true"
	if hasContent {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearAuthSession(w, r)
	s.renderHTML(w, "logout", nil)
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	authCtx := auth.GetAuthContext(r)
	authSession := authCtx.Session

	userInitial := "U"
	if authSession != nil && len(authSession.Email) > 0 {
		userInitial = strings.ToUpper(string(authSession.Email[0]))
	}

	type Project struct {
		ID        string
		Name      string
		Type      string
		Language  string
		FileCount int
		LineCount int
		CreatedAt string
		UpdatedAt string
	}

	projects := []Project{
		{
			ID:        "proj1",
			Name:      "E-Commerce Platform",
			Type:      "Web",
			Language:  "TypeScript",
			FileCount: 156,
			LineCount: 12453,
			CreatedAt: "Jan 5, 2025",
			UpdatedAt: "2 hours ago",
		},
		{
			ID:        "proj2",
			Name:      "User Auth Service",
			Type:      "API",
			Language:  "Go",
			FileCount: 42,
			LineCount: 3567,
			CreatedAt: "Dec 28, 2024",
			UpdatedAt: "Yesterday",
		},
		{
			ID:        "proj3",
			Name:      "MCP Tool",
			Type:      "MCP",
			Language:  "Python",
			FileCount: 23,
			LineCount: 1890,
			CreatedAt: "Jan 2, 2025",
			UpdatedAt: "3 days ago",
		},
		{
			ID:        "proj4",
			Name:      "Sales Analytics",
			Type:      "Data Science",
			Language:  "Python",
			FileCount: 18,
			LineCount: 2134,
			CreatedAt: "Dec 15, 2024",
			UpdatedAt: "1 week ago",
		},
	}

	data := struct {
		UserEmail   string
		UserInitial string
		Projects    []Project
	}{
		UserEmail:   authSession.Email,
		UserInitial: userInitial,
		Projects:    projects,
	}

	s.renderHTML(w, "projects", data)
}
