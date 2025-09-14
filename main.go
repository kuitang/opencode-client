package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed templates/*.html templates/tabs/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type Server struct {
	sandbox           Sandbox           // Sandbox instance for secure code execution
	sessions          map[string]string // cookie -> opencode session ID (for chat)
	selectedFiles     map[string]string // cookie -> currently selected file path
	workspaceSession  string            // Shared workspace session ID for file operations
	mu                sync.RWMutex
	providers         []Provider
	defaultModel      map[string]string
	templates         *template.Template
	codeUpdateLimiter *UpdateRateLimiter // Rate limiter for code tab SSE updates
}

type Provider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Models map[string]Model `json:"models"`
}

type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProvidersResponse struct {
	Providers []Provider        `json:"providers"`
	Default   map[string]string `json:"default"`
}

type ModelOption struct {
	Value string // provider/model format
	Label string // Display name
}

type SessionResponse struct {
	ID string `json:"id"`
}

type MessageInfo struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	SessionID  string `json:"sessionID,omitempty"`
	ProviderID string `json:"providerID,omitempty"`
	ModelID    string `json:"modelID,omitempty"`
	Time       struct {
		Created int64 `json:"created,omitempty"`
		Updated int64 `json:"updated,omitempty"`
	} `json:"time,omitempty"`
}

type MessagePart struct {
	ID        string                 `json:"id,omitempty"`
	MessageID string                 `json:"messageID,omitempty"`
	SessionID string                 `json:"sessionID,omitempty"`
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	Tool      string                 `json:"tool,omitempty"`
	CallID    string                 `json:"callID,omitempty"`
	State     map[string]interface{} `json:"state,omitempty"`
	Time      struct {
		Start int64 `json:"start,omitempty"`
		End   int64 `json:"end,omitempty"`
	} `json:"time,omitempty"`
}

type MessageResponse struct {
	Info  MessageInfo   `json:"info"`
	Parts []MessagePart `json:"parts"`
}

// FileNode represents a file or directory from OpenCode API
type FileNode struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Absolute string `json:"absolute"`
	Type     string `json:"type"` // "file" or "directory"
	Ignored  bool   `json:"ignored"`
}

// FileContent represents file content from OpenCode API
type FileContent struct {
	Content string `json:"content"`
}

// CodeTabData holds data for the code tab template
type CodeTabData struct {
	Files     []FileNode `json:"files"`
	FileCount int        `json:"fileCount"`
	LineCount int        `json:"lineCount"`
}

// MacChromeData holds data for the shared Mac OS chrome component
type MacChromeData struct {
	Title        string        `json:"title"`
	LeftContent  template.HTML `json:"leftContent"`
	RightContent template.HTML `json:"rightContent"`
	MainContent  template.HTML `json:"mainContent"`
}

// LoggingResponseWriter wraps http.ResponseWriter to log all responses
// UpdateRateLimiter implements a token bucket rate limiter for SSE updates
// It ensures immediate first update, then enforces minimum interval between subsequent updates
type UpdateRateLimiter struct {
	lastSent     time.Time
	pendingTimer *time.Timer
	mu           sync.Mutex
	minInterval  time.Duration
}

// NewUpdateRateLimiter creates a new rate limiter with specified minimum interval
func NewUpdateRateLimiter(interval time.Duration) *UpdateRateLimiter {
	return &UpdateRateLimiter{
		minInterval: interval,
	}
}

// TryUpdate attempts to execute the update function, respecting rate limits
// First update is immediate, subsequent updates are rate-limited to minInterval
// The context is used to cancel pending updates when the connection closes
func (u *UpdateRateLimiter) TryUpdate(ctx context.Context, doUpdate func()) {
	u.mu.Lock()
	defer u.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(u.lastSent)

	// Send immediately if this is first update or enough time has passed
	if u.lastSent.IsZero() || elapsed >= u.minInterval {
		u.lastSent = now
		// Execute in goroutine to avoid blocking, but check context first
		go func() {
			select {
			case <-ctx.Done():
				// Context cancelled, don't execute update
				return
			default:
				doUpdate()
			}
		}()
		return
	}

	// Cancel any pending timer
	if u.pendingTimer != nil {
		u.pendingTimer.Stop()
		u.pendingTimer = nil
	}

	// Schedule update for when minInterval has elapsed since lastSent
	remainingWait := u.minInterval - elapsed
	u.pendingTimer = time.AfterFunc(remainingWait, func() {
		// Check if context is still valid before executing
		select {
		case <-ctx.Done():
			// Context cancelled, don't execute update
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

type LoggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func NewLoggingResponseWriter(w http.ResponseWriter) *LoggingResponseWriter {
	return &LoggingResponseWriter{
		ResponseWriter: w,
		statusCode:     200,
		body:           &bytes.Buffer{},
	}
}

func (lw *LoggingResponseWriter) WriteHeader(code int) {
	lw.statusCode = code
	lw.ResponseWriter.WriteHeader(code)
}

func (lw *LoggingResponseWriter) Write(data []byte) (int, error) {
	lw.body.Write(data)
	return lw.ResponseWriter.Write(data)
}

func (lw *LoggingResponseWriter) LogResponse(method, path string) {
	bodyStr := lw.body.String()
	log.Printf("WIRE_OUT %s %s [%d]: %s", method, path, lw.statusCode, bodyStr)
}

// NewServer creates a new Server instance with properly initialized templates
func NewServer() (*Server, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &Server{
		sessions:          make(map[string]string),
		selectedFiles:     make(map[string]string),
		templates:         tmpl,
		codeUpdateLimiter: NewUpdateRateLimiter(200 * time.Millisecond),
	}, nil
}

func main() {
	port := flag.Int("port", 8080, "Port to serve HTTP")
	flag.Parse()

	log.Printf("Starting OpenCode Chat on port %d", *port)

	// Create server with templates
	server, err := NewServer()
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	log.Printf("Templates loaded successfully")

	// Initialize sandbox
	log.Printf("Initializing sandbox...")

	// Load auth configuration for testing
	// TODO: In production, get this from user configuration
	authConfig, err := loadAuthConfig()
	if err != nil {
		log.Fatalf("Failed to load auth config: %v", err)
	}

	// Create LocalDocker sandbox
	server.sandbox = NewLocalDockerSandbox()

	// Start sandbox with auth configuration
	if err := server.sandbox.Start(authConfig); err != nil {
		log.Fatalf("Failed to start sandbox: %v", err)
	}

	// Ensure cleanup happens even on panic
	defer func() {
		log.Println("Defer: Cleaning up sandbox")
		if err := server.sandbox.Stop(); err != nil {
			log.Printf("Error stopping sandbox: %v", err)
		}
	}()

	log.Printf("Sandbox ready at %s", server.sandbox.OpencodeURL())

	// Initialize workspace session for file operations
	log.Printf("Initializing workspace session...")
	if err := server.initWorkspaceSession(); err != nil {
		log.Fatalf("Failed to initialize workspace session: %v", err)
	}

	// Load providers from sandbox
	log.Printf("Loading providers from sandbox...")
	if err := server.loadProviders(); err != nil {
		log.Fatalf("Failed to load providers: %v", err)
	}
	log.Printf("Loaded %d providers", len(server.providers))

	// Logging middleware
	loggingMiddleware := func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Special handling for SSE endpoint - don't buffer the entire stream
			if r.URL.Path == "/events" {
				log.Printf("WIRE_OUT SSE connection started: %s %s", r.Method, r.URL.Path)
				handler(w, r)
				log.Printf("WIRE_OUT SSE connection ended: %s %s", r.Method, r.URL.Path)
				return
			}

			lw := NewLoggingResponseWriter(w)
			handler(lw, r)
			lw.LogResponse(r.Method, r.URL.Path)
		}
	}

	// Set up routes with logging
	http.HandleFunc("/", loggingMiddleware(server.handleIndex))
	http.HandleFunc("/send", loggingMiddleware(server.handleSend))
	http.HandleFunc("/events", loggingMiddleware(server.handleSSE))
	http.HandleFunc("/clear", loggingMiddleware(server.handleClear))
	http.HandleFunc("/download", loggingMiddleware(server.handleDownload))
	// Tab routes
	http.HandleFunc("/tab/preview", loggingMiddleware(server.handleTabPreview))
	http.HandleFunc("/tab/code", loggingMiddleware(server.handleTabCode))
	http.HandleFunc("/tab/terminal", loggingMiddleware(server.handleTabTerminal))
	http.HandleFunc("/tab/deployment", loggingMiddleware(server.handleTabDeployment))
	// Terminal proxy - handle all paths starting with /terminal/
	// Note: No logging middleware here as it interferes with WebSocket hijacking
	http.HandleFunc("/terminal/", server.handleTerminalProxy)
	// Preview proxy - handle all paths starting with /preview/
	http.HandleFunc("/preview/", loggingMiddleware(server.handlePreviewProxy))
	// Kill preview port handler
	http.HandleFunc("/kill-preview-port", loggingMiddleware(server.handleKillPreviewPort))
	// API routes
	http.HandleFunc("/tab/code/file", loggingMiddleware(server.handleFileContent))
	http.HandleFunc("/tab/code/filelist", loggingMiddleware(server.handleFileList))
	http.Handle("/static/", http.FileServer(http.FS(staticFS)))

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create HTTP server with context
	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", *port),
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting server on port %d (opencode at %s)\n", *port, server.sandbox.OpencodeURL())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	sig := <-sigChan
	log.Printf("\nReceived signal %v, shutting down gracefully...", sig)

	// Create a context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shutdown the HTTP server
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// Explicit sandbox cleanup (also happens via defer as backup)
	log.Printf("Cleaning up sandbox...")
	if err := server.sandbox.Stop(); err != nil {
		log.Printf("Error during explicit sandbox cleanup: %v", err)
	}

	log.Printf("Shutdown complete")
}

// waitForOpencodeReady polls the opencode server until it's ready
func waitForOpencodeReady(port int, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/session", port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("opencode server on port %d not ready after %v", port, timeout)
}

func (s *Server) loadProviders() error {
	resp, err := http.Get(fmt.Sprintf("%s/config/providers", s.sandbox.OpencodeURL()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var providersResp ProvidersResponse
	if err := json.NewDecoder(resp.Body).Decode(&providersResp); err != nil {
		return err
	}

	s.providers = providersResp.Providers
	s.defaultModel = providersResp.Default
	return nil
}

// getAllModels returns a sorted list of all available models
func (s *Server) getAllModels() []ModelOption {
	var models []ModelOption

	for _, provider := range s.providers {
		for _, model := range provider.Models {
			models = append(models, ModelOption{
				Value: fmt.Sprintf("%s/%s", provider.ID, model.ID),
				Label: fmt.Sprintf("%s - %s", provider.Name, model.Name),
			})
		}
	}

	// Sort alphabetically by value (provider/model)
	sort.Slice(models, func(i, j int) bool {
		return models[i].Value < models[j].Value
	})

	return models
}

// initWorkspaceSession creates a dedicated session for workspace operations
func (s *Server) initWorkspaceSession() error {
	url := fmt.Sprintf("%s/session", s.sandbox.OpencodeURL())
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

	var session SessionResponse
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

func (s *Server) getOrCreateSession(cookie string) (string, error) {
	// First check (with read lock)
	s.mu.RLock()
	sessionID, exists := s.sessions[cookie]
	s.mu.RUnlock()

	if exists {
		log.Printf("getOrCreateSession: found existing session %s for cookie %s", sessionID, cookie)
		return sessionID, nil
	}

	// Acquire write lock for session creation
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check: another goroutine might have created the session while we waited for the lock
	if sessionID, exists := s.sessions[cookie]; exists {
		log.Printf("getOrCreateSession: found existing session %s for cookie %s (double-check)", sessionID, cookie)
		return sessionID, nil
	}

	// Create new session
	url := fmt.Sprintf("%s/session", s.sandbox.OpencodeURL())
	log.Printf("getOrCreateSession: creating new session at %s", url)

	resp, err := http.Post(
		url,
		"application/json",
		bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		log.Printf("getOrCreateSession: failed to create session - %v", err)
		return "", err
	}
	defer resp.Body.Close()

	var session SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		log.Printf("getOrCreateSession: failed to decode session response - %v", err)
		return "", err
	}

	s.sessions[cookie] = session.ID
	log.Printf("getOrCreateSession: created new session %s for cookie %s", session.ID, cookie)
	return session.ID, nil
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err != nil {
		// Generate new cookie
		cookie = &http.Cookie{
			Name:     "session",
			Value:    fmt.Sprintf("sess_%d", time.Now().UnixNano()),
			HttpOnly: true,
			Path:     "/",
		}
		http.SetCookie(w, cookie)
	}

	// Ensure session exists
	sessionID, err := s.getOrCreateSession(cookie.Value)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Get existing messages
	messagesHTML := s.getMessagesHTML(sessionID)

	// Detect preview port for initial load
	ports := s.detectOpenPorts()
	var previewPort int
	if len(ports) > 0 {
		previewPort = ports[0]
		log.Printf("handleIndex: Detected preview port %d", previewPort)
	}

	// Prepare template data
	data := struct {
		Models       []ModelOption
		DefaultModel string
		MessagesHTML template.HTML
		PreviewPort  int
	}{
		Models:       s.getAllModels(),
		DefaultModel: "anthropic/claude-sonnet-4-20250514", // Default to Claude Sonnet 4
		MessagesHTML: template.HTML(messagesHTML),
		PreviewPort:  previewPort,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := s.templates.ExecuteTemplate(w, "index", data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Printf("Template error: %v", err)
	}
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleSend: received request")

	cookie, err := r.Cookie("session")
	if err != nil {
		// No cookie exists, create a new one
		log.Printf("handleSend: no session cookie, creating new one")
		cookie = &http.Cookie{
			Name:     "session",
			Value:    fmt.Sprintf("sess_%d", time.Now().UnixNano()),
			HttpOnly: true,
			Path:     "/",
		}
		http.SetCookie(w, cookie)
	}

	sessionID, err := s.getOrCreateSession(cookie.Value)
	if err != nil {
		log.Printf("handleSend: session error - %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	log.Printf("handleSend: using session %s", sessionID)

	message := r.FormValue("message")
	modelValue := r.FormValue("model") // Format: provider/model

	// Parse provider and model from combined format
	parts := strings.SplitN(modelValue, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "Invalid model format", http.StatusBadRequest)
		return
	}
	provider := parts[0]
	model := parts[1]

	log.Printf("handleSend: message=%q, provider=%q, model=%q", message, provider, model)

	if message == "" || provider == "" || model == "" {
		log.Printf("handleSend: missing fields")
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Use transformMessagePart for consistent rendering
	userPart := transformMessagePart(s.templates, MessagePart{
		Type: "text",
		Text: message,
	})

	msgData := MessageData{
		Alignment: "right",
		Text:      message,
		Provider:  provider,
		Model:     model,
		Parts:     []MessagePartData{userPart},
	}

	w.Header().Set("Content-Type", "text/html")
	if err := s.templates.ExecuteTemplate(w, "message", msgData); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	// Then send message to opencode (this will trigger SSE response)
	messageReq := struct {
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
		Model struct {
			ProviderID string `json:"providerID"`
			ModelID    string `json:"modelID"`
		} `json:"model"`
	}{
		Parts: []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			{Type: "text", Text: message},
		},
		Model: struct {
			ProviderID string `json:"providerID"`
			ModelID    string `json:"modelID"`
		}{
			ProviderID: provider,
			ModelID:    model,
		},
	}

	jsonData, _ := json.Marshal(messageReq)

	// Send async to not block the response
	go func() {
		url := fmt.Sprintf("%s/session/%s/message", s.sandbox.OpencodeURL(), sessionID)
		log.Printf("Sending message to opencode at %s", url)
		resp, err := http.Post(
			url,
			"application/json",
			bytes.NewBuffer(jsonData),
		)
		if err != nil {
			log.Printf("Failed to send message to opencode: %v", err)
			return
		}
		defer resp.Body.Close()
		log.Printf("Message sent to opencode, status: %d", resp.StatusCode)
	}()
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleSSE: new SSE connection")

	cookie, err := r.Cookie("session")
	if err != nil {
		log.Printf("handleSSE: no session cookie")
		http.Error(w, "No session", http.StatusBadRequest)
		return
	}

	sessionID, err := s.getOrCreateSession(cookie.Value)
	if err != nil {
		log.Printf("handleSSE: session error - %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	log.Printf("handleSSE: using session %s", sessionID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Connect to opencode SSE with context cancellation
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Monitor for client disconnect
	go func() {
		<-ctx.Done()
		// Context cancelled (client disconnected or request ended)
	}()

	client := &http.Client{Timeout: 0}
	sseURL := fmt.Sprintf("%s/event", s.sandbox.OpencodeURL())
	log.Printf("Connecting to OpenCode SSE at: %s", sseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		log.Printf("Failed to create SSE request: %v", err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to connect to OpenCode SSE: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("OpenCode SSE returned status: %d", resp.StatusCode)
		return
	}

	// Use bufio.Scanner - the idiomatic way to read SSE streams
	scanner := bufio.NewScanner(resp.Body)
	partsManager := NewMessagePartsManager()
	messageFirstSent := make(map[string]bool) // Track if first event sent for each message
	messageRoles := make(map[string]string)   // Track message roles

	log.Printf("Starting to read SSE stream from OpenCode")
	for scanner.Scan() {
		line := scanner.Text()

		// Log every line for debugging
		if line != "" {
			log.Printf("SSE line: %s", line)
		}

		// SSE lines starting with "data: " contain the actual data
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")
			var event map[string]interface{}
			if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
				log.Printf("SSE: Failed to parse JSON data: %v, raw data: %s", err, jsonData)
				continue // Skip invalid JSON
			}

			// Track message roles from message.updated events
			if event["type"] == "message.updated" {
				if props, ok := event["properties"].(map[string]interface{}); ok {
					if info, ok := props["info"].(map[string]interface{}); ok {
						if info["sessionID"] == sessionID {
							msgID, _ := info["id"].(string)
							role, _ := info["role"].(string)
							if msgID != "" && role != "" {
								messageRoles[msgID] = role
							}
						}
					}
				}
			}

			// Only process message.part.updated events for our session
			if event["type"] == "message.part.updated" {
				// Validate and extract message part data
				msgID, partID, part, err := ValidateAndExtractMessagePart(event, sessionID)
				if err != nil {
					// Skip invalid events (includes wrong session, missing IDs, etc.)
					continue
				}

				// Skip user messages - we only want to stream assistant messages
				if role, exists := messageRoles[msgID]; exists && role == "user" {
					continue
				}

				// Parse raw message part data and transform it to MessagePartData
				msgPart := parseRawMessagePart(partID, part)
				newPart := transformMessagePart(s.templates, msgPart)

				// Update the part in the manager
				if err := partsManager.UpdatePart(msgID, partID, newPart); err != nil {
					log.Printf("Failed to update part: %v", err)
					continue
				}

				// Build complete message from all parts
				completeParts := partsManager.GetParts(msgID)
				var completeText strings.Builder
				isStreaming := true

				var hasFileChanges bool
				for _, msgPart := range completeParts {
					if msgPart.Type == "text" {
						completeText.WriteString(msgPart.Content)
					} else if msgPart.Type == "tool" {
						completeText.WriteString("\n\n" + msgPart.Content)
						// Check if this tool might have created/modified files
						if strings.Contains(msgPart.Content, "created") ||
							strings.Contains(msgPart.Content, "wrote") ||
							strings.Contains(msgPart.Content, "saved") {
							hasFileChanges = true
						}
					} else if msgPart.Type == "step-finish" {
						isStreaming = false
						hasFileChanges = true // Assume files may have changed when step completes
					}
				}

				// Send SSE event to browser with complete message
				msgData := MessageData{
					ID:          fmt.Sprintf("assistant-%s", msgID),
					Alignment:   "left",
					Text:        completeText.String(),
					Parts:       completeParts,
					IsStreaming: isStreaming,
					HXSwapOOB:   messageFirstSent[msgID], // Use OOB for updates after first send
				}

				html, err := renderMessage(s.templates, msgData)
				if err == nil {
					html = strings.TrimSpace(html)

					// Send multi-line HTML using multiple data: lines (SSE standard)
					fmt.Fprintf(w, "event: message\n")
					lines := strings.Split(html, "\n")
					for _, line := range lines {
						fmt.Fprintf(w, "data: %s\n", line)
					}
					fmt.Fprintf(w, "\n") // Empty line to end the event
					flusher.Flush()

					// Log the SSE message sent to client
					log.Printf("WIRE_OUT SSE [msgID=%s]: %s", msgID, html)

					// Mark that we've sent the first event for this message
					if !messageFirstSent[msgID] {
						messageFirstSent[msgID] = true
					}
				}

				// Send code tab updates if files may have changed and streaming is finished
				// Use rate limiter to prevent UI flashing from rapid updates
				if hasFileChanges && !isStreaming {
					// Get current file selection for this session
					currentFile := ""
					s.mu.RLock()
					if s.selectedFiles != nil {
						currentFile = s.selectedFiles[cookie.Value]
					}
					s.mu.RUnlock()

					s.codeUpdateLimiter.TryUpdate(ctx, func() {
						s.sendCodeTabUpdates(w, flusher, currentFile)
					})
				}
			}
			// Skip user messages entirely - we only care about assistant messages
			// User messages will be handled by the normal POST/send flow
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("SSE scanner error: %v", err)
	}
	log.Printf("SSE stream ended for session %s", sessionID)
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err != nil {
		http.Error(w, "No session", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	sessionID, exists := s.sessions[cookie.Value]
	s.mu.RUnlock()

	if exists {
		// Delete the session from opencode
		req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/session/%s", s.sandbox.OpencodeURL(), sessionID), nil)
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Failed to delete session from opencode: %v", err)
		} else {
			resp.Body.Close()
		}
	}

	// Remove from our map
	s.mu.Lock()
	delete(s.sessions, cookie.Value)
	s.mu.Unlock()

	// Create new session
	_, err = s.getOrCreateSession(cookie.Value)
	if err != nil {
		http.Error(w, "Failed to create new session", http.StatusInternalServerError)
		return
	}

	// Return empty messages div
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, "<!-- Session cleared -->")
}

func (s *Server) getMessagesHTML(sessionID string) string {
	// Get messages from opencode
	resp, err := http.Get(fmt.Sprintf("%s/session/%s/message", s.sandbox.OpencodeURL(), sessionID))
	if err != nil {
		log.Printf("getMessagesHTML: Failed to fetch messages: %v", err)
		return ""
	}
	defer resp.Body.Close()

	var messages []MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		log.Printf("getMessagesHTML: Failed to decode messages: %v", err)
		return ""
	}

	log.Printf("getMessagesHTML: Got %d messages for session %s", len(messages), sessionID)

	var html strings.Builder
	for _, msg := range messages {
		// Transform all parts using the same rendering pipeline as SSE
		var parts []MessagePartData
		hasContent := false

		for _, part := range msg.Parts {
			transformedPart := transformMessagePart(s.templates, part)
			parts = append(parts, transformedPart)

			// Check if this message has any visible content
			if part.Type == "text" && part.Text != "" {
				hasContent = true
			} else if part.Type != "" {
				hasContent = true
			}
		}

		// Skip messages with no content
		if !hasContent {
			continue
		}

		alignment := "left"
		if msg.Info.Role == "user" {
			alignment = "right"
		}

		msgData := MessageData{
			ID:        msg.Info.ID,
			Alignment: alignment,
			Parts:     parts,
			Provider:  msg.Info.ProviderID,
			Model:     msg.Info.ModelID,
		}

		if err := s.templates.ExecuteTemplate(&html, "message", msgData); err != nil {
			log.Printf("getMessagesHTML: Template error: %v", err)
		}
	}

	result := html.String()
	log.Printf("getMessagesHTML: Generated %d bytes of HTML", len(result))
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Tab handler functions for HTMX requests
func (s *Server) handleTabPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	// Detect open ports in the sandbox
	ports := s.detectOpenPorts()
	var previewPort int
	if len(ports) > 0 {
		previewPort = ports[0]
		log.Printf("Preview: Detected open port %d", previewPort)
	}

	data := struct {
		PreviewPort int
	}{
		PreviewPort: previewPort,
	}

	if err := s.templates.ExecuteTemplate(w, "tab-preview", data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Printf("Tab preview template error: %v", err)
	}
}

func (s *Server) handleTabCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	// Fetch all files from OpenCode sandbox
	files, err := s.fetchAllFiles()
	if err != nil {
		log.Printf("Failed to fetch files from OpenCode: %v", err)
		// Continue with empty file list
		files = []FileNode{}
	}

	// Calculate line count using shell command
	lineCount := s.calculateLineCount()

	// Prepare data for template
	data := CodeTabData{
		Files:     files,
		FileCount: len(files),
		LineCount: lineCount,
	}

	if err := s.templates.ExecuteTemplate(w, "tab-code", data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Printf("Tab code template error: %v", err)
	}
}

func (s *Server) handleTabTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	data := map[string]interface{}{
		"GottyURL": "",
	}
	if s.sandbox != nil && s.sandbox.IsRunning() {
		gottyURL := s.sandbox.GottyURL()
		log.Printf("Terminal tab: sandbox is running, GottyURL=%q", gottyURL)
		data["GottyURL"] = gottyURL
	} else {
		log.Printf("Terminal tab: sandbox=%v, IsRunning=%v", s.sandbox != nil, s.sandbox != nil && s.sandbox.IsRunning())
	}
	if err := s.templates.ExecuteTemplate(w, "tab-terminal", data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Printf("Tab terminal template error: %v", err)
	}
}

func (s *Server) handleTabDeployment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	if err := s.templates.ExecuteTemplate(w, "tab-deployment", nil); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Printf("Tab deployment template error: %v", err)
	}
}

// handleTerminalProxy proxies requests to the gotty terminal
func (s *Server) handleTerminalProxy(w http.ResponseWriter, r *http.Request) {
	if s.sandbox == nil || !s.sandbox.IsRunning() {
		http.Error(w, "Terminal not available", http.StatusServiceUnavailable)
		return
	}

	gottyURL := s.sandbox.GottyURL()
	if gottyURL == "" {
		http.Error(w, "Terminal not supported for this sandbox type", http.StatusNotImplemented)
		return
	}

	// Parse the gotty URL
	target, err := url.Parse(gottyURL)
	if err != nil {
		log.Printf("Error parsing gotty URL: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Configure the proxy director with path rewriting
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host

		// Critical: Strip "/terminal" prefix for gotty
		originalPath := r.URL.Path
		if strings.HasPrefix(originalPath, "/terminal") {
			newPath := strings.TrimPrefix(originalPath, "/terminal")
			if newPath == "" {
				newPath = "/"
			}
			req.URL.Path = newPath
		} else {
			req.URL.Path = originalPath
		}

		// Preserve query parameters
		req.URL.RawQuery = r.URL.RawQuery

		// IMPORTANT: Set Origin header to match gotty's expectation
		// Gotty v1.0.1 checks that Origin matches the Host
		req.Header.Set("Origin", fmt.Sprintf("http://%s", target.Host))

		// Forward important headers for WebSocket upgrade
		if upgrade := r.Header.Get("Upgrade"); upgrade != "" {
			req.Header.Set("Upgrade", upgrade)
		}
		if connection := r.Header.Get("Connection"); connection != "" {
			req.Header.Set("Connection", connection)
		}
		// Forward WebSocket headers
		for key, values := range r.Header {
			if strings.HasPrefix(key, "Sec-Websocket-") {
				for _, value := range values {
					req.Header.Add(key, value)
				}
			}
		}

		// Log for debugging
		log.Printf("Terminal proxy: %s -> %s (Origin: %s)", originalPath, req.URL.Path, req.Header.Get("Origin"))
	}

	// Better error handling
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Terminal proxy error for %s: %v", r.URL.Path, err)
		http.Error(w, fmt.Sprintf("Terminal proxy error: %v", err), http.StatusBadGateway)
	}

	// Serve the request
	proxy.ServeHTTP(w, r)
}

// handlePreviewProxy proxies requests to user applications running in the sandbox
func (s *Server) handlePreviewProxy(w http.ResponseWriter, r *http.Request) {
	if s.sandbox == nil || !s.sandbox.IsRunning() {
		http.Error(w, "Sandbox not available", http.StatusServiceUnavailable)
		return
	}

	// Get the first available port
	ports := s.detectOpenPorts()
	if len(ports) == 0 {
		// Return a helpful message when no application is running
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `
			<div style="display: flex; align-items: center; justify-content: center; height: 100vh; font-family: system-ui, -apple-system, sans-serif;">
				<div style="text-align: center;">
					<h2 style="color: #666;">No Application Running</h2>
					<p style="color: #999;">Start a web server on any port to see it here.</p>
					<p style="color: #999; font-size: 0.9em;">Example: python -m http.server 5000</p>
				</div>
			</div>
		`)
		return
	}

	// Use the first detected port
	port := ports[0]

	// Get container IP for Docker sandbox, fall back to localhost for others
	containerIP := s.sandbox.ContainerIP()
	if containerIP == "" {
		containerIP = "localhost"
	}

	targetURL := fmt.Sprintf("http://%s:%d", containerIP, port)
	log.Printf("Preview proxy: forwarding to %s", targetURL)

	// Parse the target URL
	target, err := url.Parse(targetURL)
	if err != nil {
		log.Printf("Error parsing preview URL: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Modify the request to point to the application server
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.URL.Host = target.Host
		req.URL.Scheme = target.Scheme

		// Strip /preview/ prefix so user's app gets clean paths
		if strings.HasPrefix(req.URL.Path, "/preview/") {
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/preview")
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
		}

		// Handle WebSocket upgrade headers
		if req.Header.Get("Upgrade") == "websocket" {
			req.Header.Set("Origin", targetURL)
		}
	}

	// Add error handling
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Preview proxy error: %v", err)
		// Return a user-friendly error message
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `
			<div style="display: flex; align-items: center; justify-content: center; height: 100vh; font-family: system-ui, -apple-system, sans-serif;">
				<div style="text-align: center;">
					<h2 style="color: #d00;">Connection Error</h2>
					<p style="color: #666;">Unable to connect to application on port %d</p>
					<p style="color: #999; font-size: 0.9em;">The application may still be starting up. Try refreshing in a moment.</p>
				</div>
			</div>
		`, port)
	}

	// Serve the request
	proxy.ServeHTTP(w, r)
}

// handleKillPreviewPort handles killing a process listening on a specific port
func (s *Server) handleKillPreviewPort(w http.ResponseWriter, r *http.Request) {
	port := r.FormValue("port")
	if port == "" {
		http.Error(w, "Port parameter required", http.StatusBadRequest)
		return
	}

	log.Printf("handleKillPreviewPort: Killing process on port %s", port)

	s.mu.RLock()
	sessionID := s.workspaceSession
	s.mu.RUnlock()

	if sessionID == "" {
		http.Error(w, "No workspace session available", http.StatusInternalServerError)
		return
	}

	// Execute kill command via OpenCode shell API
	opencodeURL := s.sandbox.OpencodeURL()
	shellURL := fmt.Sprintf("%s/session/%s/shell", opencodeURL, sessionID)

	// Use the exact command format we tested
	command := fmt.Sprintf("kill $(lsof -t -i:%s) 2>/dev/null || true", port)

	payload := map[string]string{
		"agent":   "agent",
		"command": command,
	}

	reqBody, _ := json.Marshal(payload)
	resp, err := http.Post(shellURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("handleKillPreviewPort: Failed to execute kill command: %v", err)
		// Continue anyway to refresh the preview
	} else {
		resp.Body.Close()
	}

	// Small delay to ensure process is killed
	time.Sleep(500 * time.Millisecond)

	// Return the refreshed preview tab content
	s.handleTabPreview(w, r)
}

// fetchAllFiles recursively fetches all files from OpenCode sandbox
func (s *Server) fetchAllFiles() ([]FileNode, error) {
	if s.sandbox == nil || !s.sandbox.IsRunning() {
		return nil, fmt.Errorf("sandbox not available")
	}

	opencodeURL := s.sandbox.OpencodeURL()
	allFiles := []FileNode{}

	// Recursive function to fetch files from a directory
	var fetchDir func(path string) error
	fetchDir = func(path string) error {
		url := fmt.Sprintf("%s/file?path=%s", opencodeURL, path)
		resp, err := http.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		var nodes []FileNode
		if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
			return err
		}

		for _, node := range nodes {
			if node.Type == "file" {
				allFiles = append(allFiles, node)
			} else if node.Type == "directory" {
				// Recursively fetch files from subdirectory
				if err := fetchDir(node.Path); err != nil {
					log.Printf("Error fetching directory %s: %v", node.Path, err)
					// Continue with other directories even if one fails
				}
			}
		}
		return nil
	}

	// Start from root directory
	if err := fetchDir("."); err != nil {
		return nil, err
	}

	// Sort files in lexicographic order by path
	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].Path < allFiles[j].Path
	})

	return allFiles, nil
}

// detectOpenPorts uses lsof to find open listening ports in the sandbox
func (s *Server) detectOpenPorts() []int {
	if s.sandbox == nil || !s.sandbox.IsRunning() {
		return []int{}
	}

	s.mu.RLock()
	sessionID := s.workspaceSession
	s.mu.RUnlock()

	if sessionID == "" {
		log.Printf("detectOpenPorts: no workspace session available")
		return []int{}
	}

	// Find all user ports (excluding system ports)
	userPorts := s.findUserPorts(sessionID)

	// TODO: Add socat tunneling for localhost-only ports in future
	// For now, only return ports that are accessible from container IP

	return userPorts
}

// findUserPorts finds all listening ports excluding system services
func (s *Server) findUserPorts(sessionID string) []int {
	opencodeURL := s.sandbox.OpencodeURL()
	shellURL := fmt.Sprintf("%s/session/%s/shell", opencodeURL, sessionID)

	// Find all listening TCP ports
	command := `lsof -i -sTCP:LISTEN -P -n | grep -o ':[0-9]*' | sed 's/://' | sort -u`

	payload := map[string]string{
		"agent":   "agent",
		"command": command,
	}

	reqBody, _ := json.Marshal(payload)
	resp, err := http.Post(shellURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("findUserPorts: failed to execute command: %v", err)
		return []int{}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("findUserPorts: command failed with status %d", resp.StatusCode)
		return []int{}
	}

	var shellResult MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&shellResult); err != nil {
		log.Printf("findUserPorts: failed to decode shell response: %v", err)
		return []int{}
	}

	outputText := ""
	for _, part := range shellResult.Parts {
		if part.Type == "tool" && part.Tool == "bash" {
			if out, ok := part.State["output"].(string); ok {
				outputText = out
				log.Printf("findUserPorts: raw lsof output: %q", outputText)
				break
			}
		}
	}

	// Parse port numbers and filter out system ports
	ports := []int{}
	lines := strings.Split(strings.TrimSpace(outputText), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		port, err := strconv.Atoi(line)
		if err != nil {
			continue
		}

		// Filter out system services and our own services
		if port == 8080 || port == 8081 || port == 7681 || port < 1024 {
			continue
		}

		ports = append(ports, port)
	}

	log.Printf("findUserPorts: found user ports %v", ports)
	return ports
}

// TODO: Future socat tunneling functionality for localhost-only ports
/*
// isLocalhostOnly checks if a port is bound to localhost only (127.0.0.1)
func (s *Server) isLocalhostOnly(sessionID string, port int) bool {
	// Implementation commented out for now
	return false
}

// createSocatTunnel creates a socat tunnel for localhost-only ports
func (s *Server) createSocatTunnel(sessionID string, port int) {
	// Implementation commented out for now
}

// executeShellCommand helper function to execute shell commands via OpenCode API
func (s *Server) executeShellCommand(sessionID, command string) {
	// Implementation commented out for now
}
*/

// calculateLineCount runs wc -l command via OpenCode shell to get total line count
func (s *Server) calculateLineCount() int {
	if s.sandbox == nil || !s.sandbox.IsRunning() {
		return 0
	}

	// Use workspace session for file operations
	s.mu.RLock()
	sessionID := s.workspaceSession
	s.mu.RUnlock()

	if sessionID == "" {
		log.Printf("calculateLineCount: no workspace session available")
		return 0
	}

	opencodeURL := s.sandbox.OpencodeURL()
	shellURL := fmt.Sprintf("%s/session/%s/shell", opencodeURL, sessionID)

	payload := map[string]string{
		"agent":   "agent",
		"command": "find . -type f -exec wc -l {} + 2>/dev/null | tail -1 | awk '{print $1}'",
	}

	payloadJSON, _ := json.Marshal(payload)
	shellResp, err := http.Post(shellURL, "application/json", bytes.NewReader(payloadJSON))
	if err != nil {
		log.Printf("Failed to run shell command: %v", err)
		return 0
	}
	defer shellResp.Body.Close()

	// Parse response to extract line count
	var shellResult MessageResponse
	if err := json.NewDecoder(shellResp.Body).Decode(&shellResult); err != nil {
		log.Printf("Failed to decode shell response: %v", err)
		return 0
	}

	// Extract line count from output
	for _, part := range shellResult.Parts {
		if part.Type == "tool" && part.Tool == "bash" {
			if output, ok := part.State["output"].(string); ok {
				// Parse the number from output
				output = strings.TrimSpace(output)
				var lineCount int
				fmt.Sscanf(output, "%d", &lineCount)
				return lineCount
			}
		}
	}

	return 0
}

// sendCodeTabUpdates sends combined file stats and dropdown updates via SSE
func (s *Server) sendCodeTabUpdates(w http.ResponseWriter, flusher http.Flusher, currentPath string) {
	// Fetch all files once
	files, err := s.fetchAllFiles()
	if err != nil {
		log.Printf("Failed to fetch files for code tab update: %v", err)
		return
	}

	// Calculate line count
	lineCount := s.calculateLineCount()

	// Prepare combined data for template
	data := struct {
		Files       []FileNode
		FileCount   int
		LineCount   int
		CurrentPath string
	}{
		Files:       files,
		FileCount:   len(files),
		LineCount:   lineCount,
		CurrentPath: currentPath,
	}

	// Render the combined OOB update template
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, "code-updates-oob", data); err != nil {
		log.Printf("Failed to render code updates OOB: %v", err)
		return
	}

	// Send as single SSE event
	fmt.Fprintf(w, "event: code-updates\n")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for _, line := range lines {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprintf(w, "\n")
	flusher.Flush()
	log.Printf("Sent code tab update: %d files, %d lines", data.FileCount, data.LineCount)
}

// handleFileList returns updated file dropdown options preserving current selection
func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	// Get current selection from query parameter
	currentPath := r.URL.Query().Get("current")
	optionsOnly := r.URL.Query().Get("options_only") == "true"

	// Fetch all files from OpenCode sandbox
	files, err := s.fetchAllFiles()
	if err != nil {
		log.Printf("Failed to fetch files from OpenCode: %v", err)
		files = []FileNode{}
	}

	// Calculate line count for manual refresh
	lineCount := s.calculateLineCount()

	// Prepare data for template with current selection and counts
	data := struct {
		Files       []FileNode
		FileCount   int
		LineCount   int
		CurrentPath string
	}{
		Files:       files,
		FileCount:   len(files),
		LineCount:   lineCount,
		CurrentPath: currentPath,
	}

	w.Header().Set("Content-Type", "text/html")

	// If options_only is true, return options with OOB counter updates
	if optionsOnly {
		if err := s.templates.ExecuteTemplate(w, "file-options-with-counts", data); err != nil {
			log.Printf("Failed to render file options with counts: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
		}
	} else {
		// Return the full select element
		if err := s.templates.ExecuteTemplate(w, "file-dropdown", data); err != nil {
			log.Printf("Failed to render file dropdown: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
		}
	}
}

// handleFileContent fetches file content from OpenCode API and returns HTML
func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	// Get filepath from query parameter
	filepath := r.URL.Query().Get("path")

	// Save selected file for this session
	if cookie, err := r.Cookie("session"); err == nil && filepath != "" {
		s.mu.Lock()
		if s.selectedFiles == nil {
			s.selectedFiles = make(map[string]string)
		}
		s.selectedFiles[cookie.Value] = filepath
		s.mu.Unlock()
		log.Printf("handleFileContent: saved selected file %s for session %s", filepath, cookie.Value)
	}

	w.Header().Set("Content-Type", "text/html")

	if filepath == "" {
		// Return placeholder template
		if err := s.templates.ExecuteTemplate(w, "code-placeholder", nil); err != nil {
			log.Printf("Failed to render code placeholder: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
		}
		return
	}

	if s.sandbox == nil || !s.sandbox.IsRunning() {
		http.Error(w, "Sandbox not available", http.StatusServiceUnavailable)
		return
	}

	opencodeURL := s.sandbox.OpencodeURL()
	url := fmt.Sprintf("%s/file/content?path=%s", opencodeURL, filepath)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Failed to fetch file content: %v", err)
		http.Error(w, "Failed to fetch file content", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("OpenCode returned status %d for file %s", resp.StatusCode, filepath)
		data := struct {
			Filepath string
		}{
			Filepath: filepath,
		}
		if err := s.templates.ExecuteTemplate(w, "code-error", data); err != nil {
			log.Printf("Failed to render code error: %v", err)
			http.Error(w, "Template error", http.StatusInternalServerError)
		}
		return
	}

	var fileContent FileContent
	if err := json.NewDecoder(resp.Body).Decode(&fileContent); err != nil {
		log.Printf("Failed to decode file content: %v", err)
		http.Error(w, "Failed to decode file content", http.StatusInternalServerError)
		return
	}

	// Split content into lines for template
	lines := strings.Split(fileContent.Content, "\n")
	data := struct {
		Filepath string
		Lines    []string
	}{
		Filepath: filepath,
		Lines:    lines,
	}

	if err := s.templates.ExecuteTemplate(w, "code-content", data); err != nil {
		log.Printf("Failed to render code content: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// handleDownload streams a zip file of the sandbox working directory to the client
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleDownload: Starting zip download")

	// Check if sandbox is available and running
	if s.sandbox == nil {
		http.Error(w, "Sandbox not available", http.StatusServiceUnavailable)
		return
	}

	if !s.sandbox.IsRunning() {
		http.Error(w, "Sandbox not running", http.StatusServiceUnavailable)
		return
	}

	// Get zip stream from sandbox
	zipReader, err := s.sandbox.DownloadZip()
	if err != nil {
		log.Printf("handleDownload: Failed to create zip: %v", err)
		http.Error(w, "Failed to create zip archive", http.StatusInternalServerError)
		return
	}
	defer zipReader.Close()

	// Set headers for zip download
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=opencode-workspace.zip")
	w.Header().Set("Cache-Control", "no-cache")

	// Stream the zip file directly to the client
	// This avoids loading the entire zip into memory
	bytesWritten, err := io.Copy(w, zipReader)
	if err != nil {
		log.Printf("handleDownload: Failed to stream zip: %v", err)
		return
	}

	log.Printf("handleDownload: Successfully streamed %d bytes", bytesWritten)
}
