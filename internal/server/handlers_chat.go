package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"opencode-chat/internal/auth"
	"opencode-chat/internal/models"
	"opencode-chat/internal/sse"
	"opencode-chat/internal/views"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	cookie, _ := s.getSessionCookie(w, r)

	sessionID, err := s.getOrCreateSession(cookie.Value)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	authCtx := auth.GetAuthContext(r)

	messagesHTML := s.getMessagesHTML(sessionID)

	ports := s.detectOpenPorts()
	var previewPort int
	if len(ports) > 0 {
		previewPort = ports[0]
		log.Printf("handleIndex: Detected preview port %d", previewPort)
	}

	userInitial := ""
	if authCtx.IsAuthenticated && authCtx.Session != nil && len(authCtx.Session.Email) > 0 {
		userInitial = strings.ToUpper(string(authCtx.Session.Email[0]))
	}

	data := struct {
		Models          []models.ModelOption
		DefaultModel    string
		MessagesHTML    template.HTML
		PreviewPort     int
		IsAuthenticated bool
		UserInitial     string
	}{
		Models:          s.getAllModels(),
		DefaultModel:    "opencode/minimax-m2.5-free",
		MessagesHTML:    template.HTML(messagesHTML),
		PreviewPort:     previewPort,
		IsAuthenticated: authCtx.IsAuthenticated,
		UserInitial:     userInitial,
	}

	s.renderHTML(w, "index", data)
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleSend: received request")

	cookie, isNew := s.getSessionCookie(w, r)
	if isNew {
		log.Printf("handleSend: created new session cookie")
	}

	sessionID, err := s.getOrCreateSession(cookie.Value)
	if err != nil {
		log.Printf("handleSend: session error - %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	log.Printf("handleSend: using session %s", sessionID)

	message := r.FormValue("message")
	modelValue := r.FormValue("model")

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

	userPart := views.TransformMessagePart(s.templates, models.MessagePart{
		Type: "text",
		Text: message,
	})

	msgData := views.MessageData{
		Alignment: "right",
		Text:      message,
		Provider:  provider,
		Model:     model,
		Parts:     []views.MessagePartData{userPart},
	}

	s.renderHTML(w, "message", msgData)

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

	go func() {
		url := fmt.Sprintf("%s/session/%s/message", s.Sandbox.OpencodeURL(), sessionID)
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

	cookie, _ := s.getSessionCookie(w, r)

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

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		<-ctx.Done()
	}()

	client := &http.Client{Timeout: 0}
	sseURL := fmt.Sprintf("%s/event", s.Sandbox.OpencodeURL())
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

	scanner := bufio.NewScanner(resp.Body)
	partsManager := sse.NewMessagePartsManager()
	messageFirstSent := make(map[string]bool)
	messageRoles := make(map[string]string)

	log.Printf("Starting to read SSE stream from OpenCode")
	for scanner.Scan() {
		line := scanner.Text()

		if line != "" {
			log.Printf("SSE line: %s", line)
		}

		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")
			var event map[string]any
			if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
				log.Printf("SSE: Failed to parse JSON data: %v, raw data: %s", err, jsonData)
				continue
			}

			if event["type"] == "message.updated" {
				if props, ok := event["properties"].(map[string]any); ok {
					if info, ok := props["info"].(map[string]any); ok {
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

			if event["type"] == "message.part.updated" {
				msgID, partID, part, err := sse.ValidateAndExtractMessagePart(event, sessionID)
				if err != nil {
					continue
				}

				if role, exists := messageRoles[msgID]; exists && role == "user" {
					continue
				}

				msgPart := views.ParseRawMessagePart(partID, part)
				newPart := views.TransformMessagePart(s.templates, msgPart)

				if err := partsManager.UpdatePart(msgID, partID, newPart); err != nil {
					log.Printf("Failed to update part: %v", err)
					continue
				}

				completeParts := partsManager.GetParts(msgID)
				var completeText strings.Builder
				isStreaming := true

				var hasFileChanges bool
				for _, mp := range completeParts {
					if mp.Type == "text" {
						completeText.WriteString(mp.Content)
					} else if mp.Type == "tool" {
						completeText.WriteString("\n\n" + mp.Content)
						if strings.Contains(mp.Content, "created") ||
							strings.Contains(mp.Content, "wrote") ||
							strings.Contains(mp.Content, "saved") {
							hasFileChanges = true
						}
					} else if mp.Type == "step-finish" {
						isStreaming = false
						hasFileChanges = true
					}
				}

				msgData := views.MessageData{
					ID:          fmt.Sprintf("assistant-%s", msgID),
					Alignment:   "left",
					Text:        completeText.String(),
					Parts:       completeParts,
					IsStreaming: isStreaming,
					HXSwapOOB:  messageFirstSent[msgID],
				}

				html, err := views.RenderMessage(s.templates, msgData)
				if err == nil {
					html = strings.TrimSpace(html)

					fmt.Fprintf(w, "event: message\n")
					lines := strings.Split(html, "\n")
					for _, l := range lines {
						fmt.Fprintf(w, "data: %s\n", l)
					}
					fmt.Fprintf(w, "\n")
					flusher.Flush()

					log.Printf("WIRE_OUT SSE [msgID=%s]: %s", msgID, html)

					if !messageFirstSent[msgID] {
						messageFirstSent[msgID] = true
					}
				}

				if hasFileChanges && !isStreaming {
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

			if event["type"] == "question.asked" {
				if props, ok := event["properties"].(map[string]any); ok {
					qSessionID, _ := props["sessionID"].(string)
					if qSessionID != sessionID {
						continue
					}

					requestID, _ := props["id"].(string)

					// Re-marshal to struct for clean extraction
					propsJSON, err := json.Marshal(props)
					if err != nil {
						log.Printf("SSE: Failed to marshal question properties: %v", err)
						continue
					}
					var qr models.QuestionRequest
					if err := json.Unmarshal(propsJSON, &qr); err != nil {
						log.Printf("SSE: Failed to parse question request: %v", err)
						continue
					}

					formData := struct {
						RequestID string
						Questions []models.QuestionInfo
					}{
						RequestID: requestID,
						Questions: qr.Questions,
					}

					html, err := s.renderHTMLToString("question", formData)
					if err != nil {
						log.Printf("SSE: Failed to render question template: %v", err)
						continue
					}

					html = strings.TrimSpace(html)
					fmt.Fprintf(w, "event: message\n")
					for _, l := range strings.Split(html, "\n") {
						fmt.Fprintf(w, "data: %s\n", l)
					}
					fmt.Fprintf(w, "\n")
					flusher.Flush()

					log.Printf("WIRE_OUT SSE question [requestID=%s]: rendered %d bytes", requestID, len(html))
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("SSE scanner error: %v", err)
	}
	log.Printf("SSE stream ended for session %s", sessionID)
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	cookie, _ := s.getSessionCookie(w, r)

	s.mu.RLock()
	sessionID, exists := s.sessions[cookie.Value]
	s.mu.RUnlock()

	if exists {
		req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/session/%s", s.Sandbox.OpencodeURL(), sessionID), nil)
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Failed to delete session from opencode: %v", err)
		} else {
			resp.Body.Close()
		}
	}

	s.mu.Lock()
	delete(s.sessions, cookie.Value)
	s.mu.Unlock()

	_, err := s.getOrCreateSession(cookie.Value)
	if err != nil {
		http.Error(w, "Failed to create new session", http.StatusInternalServerError)
		return
	}

	s.renderHTML(w, "session-cleared", nil)
}

func (s *Server) getMessagesHTML(sessionID string) string {
	resp, err := http.Get(fmt.Sprintf("%s/session/%s/message", s.Sandbox.OpencodeURL(), sessionID))
	if err != nil {
		log.Printf("getMessagesHTML: Failed to fetch messages: %v", err)
		return ""
	}
	defer resp.Body.Close()

	var messages []models.MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		log.Printf("getMessagesHTML: Failed to decode messages: %v", err)
		return ""
	}

	log.Printf("getMessagesHTML: Got %d messages for session %s", len(messages), sessionID)

	var html strings.Builder
	for _, msg := range messages {
		var msgParts []views.MessagePartData
		hasContent := false

		for _, part := range msg.Parts {
			transformedPart := views.TransformMessagePart(s.templates, part)
			msgParts = append(msgParts, transformedPart)

			if part.Type == "text" && part.Text != "" {
				hasContent = true
			} else if part.Type != "" {
				hasContent = true
			}
		}

		if !hasContent {
			continue
		}

		alignment := "left"
		if msg.Info.Role == "user" {
			alignment = "right"
		}

		msgData := views.MessageData{
			ID:        msg.Info.ID,
			Alignment: alignment,
			Parts:     msgParts,
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
