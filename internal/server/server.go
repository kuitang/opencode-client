package server

import (
	"bytes"
	"context"
	"html/template"
	"log"
	"net/http"
	"sync"
	"time"

	"opencode-chat/internal/auth"
	"opencode-chat/internal/models"
	"opencode-chat/internal/sandbox"
	"opencode-chat/internal/views"
)

// UpdateRateLimiter implements a token bucket rate limiter for SSE updates.
// It ensures immediate first update, then enforces minimum interval between subsequent updates.
type UpdateRateLimiter struct {
	lastSent     time.Time
	pendingTimer *time.Timer
	mu           sync.Mutex
	minInterval  time.Duration
}

// NewUpdateRateLimiter creates a new rate limiter with specified minimum interval.
func NewUpdateRateLimiter(interval time.Duration) *UpdateRateLimiter {
	return &UpdateRateLimiter{
		minInterval: interval,
	}
}

// TryUpdate attempts to execute the update function, respecting rate limits.
// First update is immediate, subsequent updates are rate-limited to minInterval.
// The context is used to cancel pending updates when the connection closes.
func (u *UpdateRateLimiter) TryUpdate(ctx context.Context, doUpdate func()) {
	u.mu.Lock()
	defer u.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(u.lastSent)

	if u.lastSent.IsZero() || elapsed >= u.minInterval {
		u.lastSent = now
		go func() {
			select {
			case <-ctx.Done():
				return
			default:
				doUpdate()
			}
		}()
		return
	}

	if u.pendingTimer != nil {
		u.pendingTimer.Stop()
		u.pendingTimer = nil
	}

	remainingWait := u.minInterval - elapsed
	u.pendingTimer = time.AfterFunc(remainingWait, func() {
		select {
		case <-ctx.Done():
			u.mu.Lock()
			u.pendingTimer = nil
			u.mu.Unlock()
			return
		default:
			u.mu.Lock()
			u.lastSent = time.Now()
			u.pendingTimer = nil
			u.mu.Unlock()
			doUpdate()
		}
	})
}

// Server is the main application server.
type Server struct {
	Sandbox           sandbox.Sandbox
	sessions          map[string]string             // cookie -> opencode session ID (for chat)
	authSessions      map[string]*auth.AuthSession  // cookie -> auth session
	selectedFiles     map[string]string             // cookie -> currently selected file path
	workspaceSession  string                        // Shared workspace session ID for file operations
	mu                sync.RWMutex
	providers         []models.Provider
	defaultModel      map[string]string
	templates         *template.Template
	codeUpdateLimiter *UpdateRateLimiter
}

// NewServer creates a new Server instance with properly initialized templates.
func NewServer() (*Server, error) {
	tmpl, err := views.LoadTemplates()
	if err != nil {
		return nil, err
	}

	return &Server{
		sessions:          make(map[string]string),
		authSessions:      make(map[string]*auth.AuthSession),
		selectedFiles:     make(map[string]string),
		templates:         tmpl,
		codeUpdateLimiter: NewUpdateRateLimiter(200 * time.Millisecond),
	}, nil
}

// renderHTML sets the HTML content type header and executes the template.
func (s *Server) renderHTML(w http.ResponseWriter, tmplName string, data any) {
	w.Header().Set("Content-Type", "text/html")
	if err := s.templates.ExecuteTemplate(w, tmplName, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Printf("Template error rendering %s: %v", tmplName, err)
	}
}

// httpError logs and sends an HTTP error response.
func (s *Server) httpError(w http.ResponseWriter, message string, code int) {
	log.Printf("HTTP %d: %s", code, message)
	http.Error(w, message, code)
}

// renderHTMLToString renders a template to a string.
func (s *Server) renderHTMLToString(tmplName string, data any) (string, error) {
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, tmplName, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
