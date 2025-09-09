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
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type Server struct {
	opencodePort int
	opencodeCmd  *exec.Cmd
	opencodeDir  string // Temporary directory for opencode
	sessions     map[string]string // cookie -> opencode session ID
	mu           sync.RWMutex
	providers    []Provider
	defaultModel map[string]string
	templates    *template.Template
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

type SessionResponse struct {
	ID string `json:"id"`
}

type MessageInfo struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	ProviderID string `json:"providerID,omitempty"`
	ModelID    string `json:"modelID,omitempty"`
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
		Start string `json:"start,omitempty"`
		End   string `json:"end,omitempty"`
	} `json:"time,omitempty"`
}

type MessageResponse struct {
	Info  MessageInfo   `json:"info"`
	Parts []MessagePart `json:"parts"`
}

// LoggingResponseWriter wraps http.ResponseWriter to log all responses
type LoggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func NewLoggingResponseWriter(w http.ResponseWriter) *LoggingResponseWriter {
	return &LoggingResponseWriter{
		ResponseWriter: w,
		statusCode:     200,
		body:          &bytes.Buffer{},
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
func NewServer(opencodePort int) (*Server, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &Server{
		opencodePort: opencodePort,
		sessions:     make(map[string]string),
		templates:    tmpl,
	}, nil
}

func main() {
	port := flag.Int("port", 8080, "Port to serve HTTP")
	flag.Parse()

	log.Printf("Starting OpenCode Chat on port %d", *port)

	server := &Server{
		opencodePort: *port + 1000, // Offset by 1000 for opencode
		sessions:     make(map[string]string),
	}

	// Load templates with custom functions
	var err error
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}
	server.templates, err = template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		log.Fatalf("Failed to parse templates: %v", err)
	}
	log.Printf("Templates loaded successfully")

	// Start opencode server
	log.Printf("Starting opencode server on port %d", server.opencodePort)
	if err := server.startOpencodeServer(); err != nil {
		log.Fatalf("Failed to start opencode server: %v", err)
	}
	// Ensure cleanup happens even on panic
	defer func() {
		log.Println("Defer: Cleaning up opencode server")
		server.stopOpencodeServer()
	}()

	// Wait for opencode to be ready
	log.Printf("Waiting for opencode to be ready...")
	if err := waitForOpencodeReady(server.opencodePort, 10*time.Second); err != nil {
		server.stopOpencodeServer()
		log.Fatalf("Opencode server not ready: %v", err)
	}
	
	// CRITICAL: Verify opencode is running in the isolated directory
	if err := server.verifyOpencodeIsolation(); err != nil {
		server.stopOpencodeServer()
		log.Fatalf("CRITICAL SECURITY ERROR: %v", err)
	}

	// Load providers
	log.Printf("Loading providers from opencode...")
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
	http.HandleFunc("/messages", loggingMiddleware(server.handleMessages))
	http.HandleFunc("/models", loggingMiddleware(server.handleModels))
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
		log.Printf("Starting server on port %d (opencode on %d)\n", *port, server.opencodePort)
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
	
	// Note: opencode cleanup happens via defer
	log.Printf("Shutdown complete")
}

func (s *Server) startOpencodeServer() error {
	// Create a temporary directory for opencode to work in, including PID in name
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("opencode-chat-pid%d-*", os.Getpid()))
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	s.opencodeDir = tmpDir
	log.Printf("SECURITY: Created isolated temporary directory for opencode: %s", tmpDir)
	
	// Create a marker file to verify isolation
	markerPath := filepath.Join(tmpDir, ".opencode-isolation-marker")
	if err := os.WriteFile(markerPath, []byte("opencode should run here"), 0644); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to create isolation marker: %w", err)
	}
	
	// Verify we're NOT in the user's working directory
	userCwd, _ := os.Getwd()
	if strings.HasPrefix(tmpDir, userCwd) || strings.HasPrefix(userCwd, tmpDir) {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("CRITICAL SECURITY ERROR: temp directory %s overlaps with user directory %s", tmpDir, userCwd)
	}
	
	// Start opencode in the temporary directory
	s.opencodeCmd = exec.Command("opencode", "serve", "--port", fmt.Sprintf("%d", s.opencodePort))
	s.opencodeCmd.Dir = tmpDir // CRITICAL: Run opencode in the temp directory
	s.opencodeCmd.Stdout = os.Stdout
	s.opencodeCmd.Stderr = os.Stderr
	
	// Add environment variable to make it clear where opencode should run
	s.opencodeCmd.Env = append(os.Environ(), fmt.Sprintf("OPENCODE_WORKDIR=%s", tmpDir))
	
	if err := s.opencodeCmd.Start(); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to start opencode: %w", err)
	}
	
	log.Printf("SECURITY: opencode process started with PID %d in isolated directory %s", 
		s.opencodeCmd.Process.Pid, tmpDir)
	return nil
}

func (s *Server) stopOpencodeServer() {
	// Prevent double cleanup
	if s.opencodeCmd == nil && s.opencodeDir == "" {
		return
	}
	
	// First, stop the opencode process completely
	processStoppedSuccessfully := true
	if s.opencodeCmd != nil && s.opencodeCmd.Process != nil {
		log.Printf("Stopping opencode server (PID: %d)", s.opencodeCmd.Process.Pid)
		// Try graceful shutdown first
		if err := s.opencodeCmd.Process.Signal(os.Interrupt); err != nil {
			log.Printf("Failed to send interrupt signal: %v", err)
			processStoppedSuccessfully = false
		}
		
		// Give it 2 seconds to gracefully shutdown
		done := make(chan error, 1)
		go func() {
			defer close(done) // Ensure channel is closed to prevent leaks
			done <- s.opencodeCmd.Wait()
		}()
		
		select {
		case err := <-done:
			if err != nil {
				log.Printf("Opencode server exited with error: %v", err)
			} else {
				log.Println("Opencode server stopped gracefully")
			}
		case <-time.After(2 * time.Second):
			log.Println("Force killing opencode server")
			if err := s.opencodeCmd.Process.Kill(); err != nil {
				log.Printf("Failed to force kill process: %v", err)
				processStoppedSuccessfully = false
			} else {
				// Wait for the kill to complete and drain the done channel
				<-done
			}
		}
		s.opencodeCmd = nil // Prevent double cleanup
	}
	
	// Only clean up directory after process is stopped (or we tried our best)
	if s.opencodeDir != "" {
		if processStoppedSuccessfully {
			log.Printf("Cleaning up temporary directory: %s", s.opencodeDir)
		} else {
			log.Printf("WARNING: Process may still be running, attempting directory cleanup anyway: %s", s.opencodeDir)
		}
		
		if err := os.RemoveAll(s.opencodeDir); err != nil {
			log.Printf("Warning: failed to clean up temp directory %s: %v", s.opencodeDir, err)
		}
		s.opencodeDir = "" // Prevent double cleanup
	}
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

// verifyOpencodeIsolation verifies opencode is running in the isolated temporary directory
func (s *Server) verifyOpencodeIsolation() error {
	
	// Call the /path endpoint to get opencode's current working directory
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/path", s.opencodePort))
	if err != nil {
		return fmt.Errorf("failed to query opencode /path endpoint: %w", err)
	}
	defer resp.Body.Close()
	
	// The /path endpoint returns: {"state":"...", "config":"...", "worktree":"...", "directory":"..."}
	var pathResponse struct {
		State     string `json:"state"`
		Config    string `json:"config"`
		Worktree  string `json:"worktree"`
		Directory string `json:"directory"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&pathResponse); err != nil {
		return fmt.Errorf("failed to decode /path response: %w", err)
	}
	
	// Check if directory is empty
	if pathResponse.Directory == "" {
		return fmt.Errorf("opencode /path endpoint returned empty directory - opencode may not be running correctly")
	}
	
	// Verify the directory matches our temporary directory
	if pathResponse.Directory != s.opencodeDir {
		return fmt.Errorf("opencode is NOT running in isolated directory! Expected: %s, Got: %s", 
			s.opencodeDir, pathResponse.Directory)
	}
	
	// Additional safety check: ensure it's not in the user's working directory
	userCwd, _ := os.Getwd()
	if strings.HasPrefix(pathResponse.Directory, userCwd) {
		return fmt.Errorf("opencode is running in user's directory %s instead of isolated temp directory", 
			pathResponse.Directory)
	}
	
	// Verify it's in the system temp directory
	systemTempDir := os.TempDir()
	if !strings.HasPrefix(pathResponse.Directory, systemTempDir) {
		return fmt.Errorf("opencode directory %s is not in system temp directory %s", 
			pathResponse.Directory, systemTempDir)
	}
	
	log.Printf("✓ SECURITY VERIFIED: opencode is correctly isolated in: %s", pathResponse.Directory)
	log.Printf("  - State directory: %s", pathResponse.State)
	log.Printf("  - Config directory: %s", pathResponse.Config)
	log.Printf("  - Working directory: %s", pathResponse.Directory)
	return nil
}

func (s *Server) loadProviders() error {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/config/providers", s.opencodePort))
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

func (s *Server) getOrCreateSession(cookie string) (string, error) {
	s.mu.RLock()
	sessionID, exists := s.sessions[cookie]
	s.mu.RUnlock()

	if exists {
		log.Printf("getOrCreateSession: found existing session %s for cookie %s", sessionID, cookie)
		return sessionID, nil
	}

	// Create new session
	url := fmt.Sprintf("http://localhost:%d/session", s.opencodePort)
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

	s.mu.Lock()
	s.sessions[cookie] = session.ID
	s.mu.Unlock()

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

	// Get default provider
	defaultProvider := "anthropic"
	if len(s.providers) > 0 {
		for _, p := range s.providers {
			if p.ID == "anthropic" {
				defaultProvider = "anthropic"
				break
			}
		}
	}

	// Get models for default provider
	var defaultModels []Model
	for _, p := range s.providers {
		if p.ID == defaultProvider {
			for _, m := range p.Models {
				defaultModels = append(defaultModels, m)
			}
			break
		}
	}

	// Prepare template data
	data := struct {
		Providers       []Provider
		DefaultProvider string
		DefaultModels   []Model
		DefaultModel    string
		MessagesHTML    template.HTML
	}{
		Providers:       s.providers,
		DefaultProvider: defaultProvider,
		DefaultModels:   defaultModels,
		DefaultModel:    s.defaultModel[defaultProvider],
		MessagesHTML:    template.HTML(messagesHTML),
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
		log.Printf("handleSend: no session cookie - %v", err)
		http.Error(w, "No session", http.StatusBadRequest)
		return
	}

	sessionID, err := s.getOrCreateSession(cookie.Value)
	if err != nil {
		log.Printf("handleSend: session error - %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	log.Printf("handleSend: using session %s", sessionID)

	message := r.FormValue("message")
	provider := r.FormValue("provider")
	model := r.FormValue("model")

	log.Printf("handleSend: message=%q, provider=%q, model=%q", message, provider, model)

	if message == "" || provider == "" || model == "" {
		log.Printf("handleSend: missing fields")
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Render user message with unified renderer
	renderedHTML := renderText(message)
	
	msgData := MessageData{
		Alignment: "right",
		Text:      message,
		Provider:  provider,
		Model:     model,
		Parts: []MessagePartData{{
			Type:         "text",
			Content:      message,  // Keep original text
			RenderedHTML: renderedHTML,
		}},
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
		url := fmt.Sprintf("http://localhost:%d/session/%s/message", s.opencodePort, sessionID)
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

	// Connect to opencode SSE
	client := &http.Client{Timeout: 0}
	sseURL := fmt.Sprintf("http://localhost:%d/event", s.opencodePort)
	log.Printf("Connecting to OpenCode SSE at: %s", sseURL)
	req, _ := http.NewRequest("GET", sseURL, nil)
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

				var newPart MessagePartData
				// Update the specific part
				switch part["type"] {
				case "text":
					if text, ok := part["text"].(string); ok {
						// Render with unified renderer
						renderedHTML := renderText(text)
						
						newPart = MessagePartData{
							Type:         "text",
							Content:      text,  // Keep original text
							RenderedHTML: renderedHTML,
						}
					}
				case "reasoning":
					if text, ok := part["text"].(string); ok {
						reasoningText := fmt.Sprintf("🤔 Reasoning:\n%s", text)
						newPart = MessagePartData{
							Type:    "reasoning",
							Content: reasoningText,
							// No RenderedHTML - will use .Content in <pre> tag
						}
					}
				case "tool":
					// Store tool information for rendering
					toolName, _ := part["tool"].(string)
					if state, ok := part["state"].(map[string]interface{}); ok {
						status, _ := state["status"].(string)
						input, _ := state["input"].(map[string]interface{})
						output, _ := state["output"].(string)
						
						// Use the new renderToolDetails function
						renderedHTML := renderToolDetails(s.templates, toolName, status, input, output)
						
						// Create a simple text fallback for non-HTML contexts
						var toolContent strings.Builder
						toolContent.WriteString(fmt.Sprintf("Tool: %s (Status: %s)", toolName, status))
						if len(input) > 0 {
							toolContent.WriteString("\nInput: ")
							for key, value := range input {
								toolContent.WriteString(fmt.Sprintf("%s=%v ", key, value))
							}
						}
						if output != "" {
							toolContent.WriteString("\nOutput:\n" + output)
						}
						
						newPart = MessagePartData{
							Type:         "tool",
							Content:      toolContent.String(),
							RenderedHTML: renderedHTML,
						}
					}
				case "file":
					filename, _ := part["filename"].(string)
					url, _ := part["url"].(string)
					newPart = MessagePartData{
						Type:    "file",
						Content: fmt.Sprintf("📁 File: %s\nURL: %s", filename, url),
					}
				case "snapshot":
					newPart = MessagePartData{
						Type:    "snapshot",
						Content: "📸 Snapshot taken",
					}
				case "patch":
					newPart = MessagePartData{
						Type:    "patch", 
						Content: "🔧 Code patch applied",
					}
				case "agent":
					newPart = MessagePartData{
						Type:    "agent",
						Content: "🤖 Agent action",
					}
				case "step-start":
					// Render as status badge (block-level for new line)
					badgeHTML := template.HTML(`<div class="flex items-center gap-2 px-3 py-1 bg-yellow-100 text-yellow-800 rounded-full text-sm my-2 w-fit">
						<span>▶️</span>
						<span>Step started</span>
					</div>`)
					newPart = MessagePartData{
						Type:         "step-start",
						Content:      "▶️ Step started",
						RenderedHTML: badgeHTML,
					}
				case "step-finish":
					// Mark message as complete - render as status badge (block-level for new line)
					badgeHTML := template.HTML(`<div class="flex items-center gap-2 px-3 py-1 bg-green-100 text-green-800 rounded-full text-sm my-2 w-fit">
						<span>✅</span>
						<span>Step completed</span>
					</div>`)
					newPart = MessagePartData{
						Type:         "step-finish",
						Content:      "✅ Step completed",
						RenderedHTML: badgeHTML,
					}
				}

				// Update the part in the manager
				if err := partsManager.UpdatePart(msgID, partID, newPart); err != nil {
					log.Printf("Failed to update part: %v", err)
					continue
				}

				// Build complete message from all parts
				completeParts := partsManager.GetParts(msgID)
				var completeText strings.Builder
				isStreaming := true
				
				for _, msgPart := range completeParts {
					if msgPart.Type == "text" {
						completeText.WriteString(msgPart.Content)
					} else if msgPart.Type == "tool" {
						completeText.WriteString("\n\n" + msgPart.Content)
					} else if msgPart.Type == "step-finish" {
						isStreaming = false
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
		req, _ := http.NewRequest("DELETE", fmt.Sprintf("http://localhost:%d/session/%s", s.opencodePort, sessionID), nil)
		client := &http.Client{}
		client.Do(req)
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
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/session/%s/message", s.opencodePort, sessionID))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var messages []MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return ""
	}

	var html strings.Builder
	for _, msg := range messages {
		text := ""
		for _, part := range msg.Parts {
			if part.Type == "text" {
				text += part.Text
			}
		}

		if text == "" {
			continue
		}

		alignment := "left"
		if msg.Info.Role == "user" {
			alignment = "right"
		}

		msgData := MessageData{
			Alignment: alignment,
			Text:      text,
			Provider:  msg.Info.ProviderID,
			Model:     msg.Info.ModelID,
		}

		if msgHTML, err := renderMessage(s.templates, msgData); err == nil {
			html.WriteString(msgHTML)
		}
	}

	return html.String()
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	providerID := r.URL.Query().Get("provider")
	if providerID == "" {
		http.Error(w, "Provider required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html")

	// Find provider and return options
	for _, p := range s.providers {
		if p.ID == providerID {
			defaultModel := s.defaultModel[providerID]
			for modelID, model := range p.Models {
				selected := ""
				if modelID == defaultModel {
					selected = "selected"
				}
				fmt.Fprintf(w, `<option value="%s" %s>%s</option>`, modelID, selected, model.Name)
			}
			return
		}
	}
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err != nil {
		http.Error(w, "No session", http.StatusBadRequest)
		return
	}

	sessionID, err := s.getOrCreateSession(cookie.Value)
	if err != nil {
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}

	// Get messages from opencode
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/session/%s/message", s.opencodePort, sessionID))
	if err != nil {
		http.Error(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var messages []MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		http.Error(w, "Failed to parse messages", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")

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

		if err := s.templates.ExecuteTemplate(w, "message", msgData); err != nil {
			log.Printf("Template error in handleMessages: %v", err)
		}
	}
}
