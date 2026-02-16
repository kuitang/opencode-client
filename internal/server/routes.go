package server

import (
	"net/http"

	"opencode-chat/internal/middleware"
	"opencode-chat/internal/templates"
)

// RegisterRoutes creates and configures the HTTP mux with Go 1.22+ method routing.
func (s *Server) RegisterRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Main pages
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /login", s.handleLoginGET)
	mux.HandleFunc("POST /login", s.handleLoginPOST)
	mux.HandleFunc("GET /logout", s.handleLogout)
	mux.HandleFunc("GET /projects", s.handleProjects)

	// Chat
	mux.HandleFunc("POST /send", s.handleSend)
	mux.HandleFunc("GET /events", s.handleSSE)
	mux.HandleFunc("POST /clear", s.handleClear)
	mux.HandleFunc("GET /download", s.handleDownload)

	// Question tool
	mux.HandleFunc("POST /question/{requestID}/reply", s.handleQuestionReply)
	mux.HandleFunc("POST /question/{requestID}/reject", s.handleQuestionReject)

	// Tabs
	mux.HandleFunc("GET /tab/preview", s.handleTabPreview)
	mux.HandleFunc("GET /tab/code", s.handleTabCode)
	mux.HandleFunc("GET /tab/terminal", s.handleTabTerminal)
	mux.HandleFunc("GET /tab/deployment", s.handleTabDeployment)

	// File API
	mux.HandleFunc("GET /tab/code/file", s.handleFileContent)
	mux.HandleFunc("GET /tab/code/filelist", s.handleFileList)

	// Proxies (no method constraint — websocket needs all methods)
	mux.Handle("/terminal/", http.HandlerFunc(s.handleTerminalProxy))
	mux.HandleFunc("/preview/", s.handlePreviewProxy)
	mux.HandleFunc("POST /kill-preview-port", s.handleKillPreviewPort)

	// Static files
	mux.Handle("GET /static/", http.FileServer(http.FS(templates.StaticFS)))

	return mux
}

// WrapWithMiddleware applies standard middleware to the mux.
func (s *Server) WrapWithMiddleware(mux *http.ServeMux) http.Handler {
	// Apply auth context middleware, then logging
	var handler http.Handler = mux
	handler = s.withAuth(handler)
	handler = middleware.LoggingMiddleware(handler)
	return handler
}
