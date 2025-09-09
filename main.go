package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type Server struct {
	opencodePort int
	opencodeCmd  *exec.Cmd
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
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type MessageResponse struct {
	Info  MessageInfo   `json:"info"`
	Parts []MessagePart `json:"parts"`
}

type MessageData struct {
	ID        string
	Alignment string
	Text      string
	Provider  string
	Model     string
	HXSwapOOB bool
}

// NewServer creates a new Server instance with properly initialized templates
func NewServer(opencodePort int) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
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

	// Load templates
	var err error
	server.templates, err = template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		log.Fatalf("Failed to parse templates: %v", err)
	}
	log.Printf("Templates loaded successfully")

	// Start opencode server
	log.Printf("Starting opencode server on port %d", server.opencodePort)
	if err := server.startOpencodeServer(); err != nil {
		log.Fatalf("Failed to start opencode server: %v", err)
	}
	defer server.stopOpencodeServer()

	// Wait for opencode to be ready
	log.Printf("Waiting for opencode to be ready...")
	time.Sleep(2 * time.Second)

	// Load providers
	log.Printf("Loading providers from opencode...")
	if err := server.loadProviders(); err != nil {
		log.Fatalf("Failed to load providers: %v", err)
	}
	log.Printf("Loaded %d providers", len(server.providers))

	// Start opencode server
	if err := server.startOpencodeServer(); err != nil {
		log.Fatalf("Failed to start opencode server: %v", err)
	}
	defer server.stopOpencodeServer()

	// Wait for opencode to be ready
	time.Sleep(2 * time.Second)

	// Load providers
	if err := server.loadProviders(); err != nil {
		log.Fatalf("Failed to load providers: %v", err)
	}

	// Set up routes
	http.HandleFunc("/", server.handleIndex)
	http.HandleFunc("/send", server.handleSend)
	http.HandleFunc("/events", server.handleSSE)
	http.HandleFunc("/clear", server.handleClear)
	http.HandleFunc("/messages", server.handleMessages)
	http.HandleFunc("/models", server.handleModels)
	http.Handle("/static/", http.FileServer(http.FS(staticFS)))

	log.Printf("Starting server on port %d (opencode on %d)\n", *port, server.opencodePort)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func (s *Server) startOpencodeServer() error {
	s.opencodeCmd = exec.Command("opencode", "serve", "--port", fmt.Sprintf("%d", s.opencodePort))
	s.opencodeCmd.Stdout = os.Stdout
	s.opencodeCmd.Stderr = os.Stderr
	return s.opencodeCmd.Start()
}

func (s *Server) stopOpencodeServer() {
	if s.opencodeCmd != nil && s.opencodeCmd.Process != nil {
		s.opencodeCmd.Process.Kill()
	}
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

	// Render user message using template
	msgData := MessageData{
		Alignment: "right",
		Text:      message,
		Provider:  provider,
		Model:     model,
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
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/event", s.opencodePort), nil)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	reader := resp.Body
	buf := make([]byte, 4096)
	currentMessage := ""

	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("SSE read error: %v", err)
			}
			break
		}

		data := string(buf[:n])
		lines := strings.Split(data, "\n")

		for _, line := range lines {
			if strings.HasPrefix(line, "data: ") {
				jsonData := strings.TrimPrefix(line, "data: ")
				var event map[string]interface{}
				if err := json.Unmarshal([]byte(jsonData), &event); err == nil {
					log.Printf("SSE event type: %v", event["type"])
					if event["type"] == "message.part.updated" {
						if props, ok := event["properties"].(map[string]interface{}); ok {
							if part, ok := props["part"].(map[string]interface{}); ok {
								log.Printf("SSE part sessionID: %v, our sessionID: %v", part["sessionID"], sessionID)
								if part["sessionID"] == sessionID && part["type"] == "text" {
									if text, ok := part["text"].(string); ok {
										currentMessage = text
										// Send update using template
										msgData := MessageData{
											ID:        fmt.Sprintf("assistant-%s", part["messageID"]),
											Alignment: "left",
											Text:      currentMessage,
										}

										var buf bytes.Buffer
										if err := s.templates.ExecuteTemplate(&buf, "message", msgData); err == nil {
											// SSE data must be on a single line - replace newlines with spaces
											html := strings.ReplaceAll(buf.String(), "\n", " ")
											html = strings.TrimSpace(html)
											fmt.Fprintf(w, "event: message\n")
											fmt.Fprintf(w, "data: %s\n\n", html)
											flusher.Flush()
										}
									}
								}
							}
						}
					} else if event["type"] == "message.updated" {
						if props, ok := event["properties"].(map[string]interface{}); ok {
							if info, ok := props["info"].(map[string]interface{}); ok {
								if info["sessionID"] == sessionID && info["role"] == "assistant" {
									// Add model info
									provider := info["providerID"]
									model := info["modelID"]
									messageID := info["id"]

									// Use template with HXSwapOOB to update the existing message with final content + metadata
									msgData := MessageData{
										ID:        fmt.Sprintf("assistant-%s", messageID),
										Alignment: "left",
										Text:      currentMessage,
										Provider:  fmt.Sprintf("%v", provider),
										Model:     fmt.Sprintf("%v", model),
										HXSwapOOB: true,
									}

									var buf bytes.Buffer
									if err := s.templates.ExecuteTemplate(&buf, "message", msgData); err == nil {
										// SSE data must be on a single line - replace newlines with spaces
										html := strings.ReplaceAll(buf.String(), "\n", " ")
										html = strings.TrimSpace(html)
										fmt.Fprintf(w, "event: message\n")
										fmt.Fprintf(w, "data: %s\n\n", html)
										flusher.Flush()
									}
									currentMessage = ""
								}
							}
						}
					}
				}
			}
		}
	}
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

		var buf bytes.Buffer
		if err := s.templates.ExecuteTemplate(&buf, "message", msgData); err == nil {
			html.WriteString(buf.String())
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

		if err := s.templates.ExecuteTemplate(w, "message", msgData); err != nil {
			log.Printf("Template error in handleMessages: %v", err)
		}
	}
}
