package server

import (
	"log"
	"net/http"

	"opencode-chat/internal/models"
)

func (s *Server) handleTabPreview(w http.ResponseWriter, r *http.Request) {
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

	s.renderHTML(w, "tab-preview", data)
}

func (s *Server) handleTabCode(w http.ResponseWriter, r *http.Request) {
	files, err := s.fetchAllFiles()
	if err != nil {
		log.Printf("Failed to fetch files from OpenCode: %v", err)
		files = []models.FileNode{}
	}

	lineCount := s.calculateLineCount()

	data := models.CodeTabData{
		Files:     files,
		FileCount: len(files),
		LineCount: lineCount,
	}

	s.renderHTML(w, "tab-code", data)
}

func (s *Server) handleTabTerminal(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"GottyURL": "",
	}
	if s.Sandbox != nil && s.Sandbox.IsRunning() {
		gottyURL := s.Sandbox.GottyURL()
		log.Printf("Terminal tab: sandbox is running, GottyURL=%q", gottyURL)
		data["GottyURL"] = gottyURL
	} else {
		log.Printf("Terminal tab: sandbox=%v, IsRunning=%v", s.Sandbox != nil, s.Sandbox != nil && s.Sandbox.IsRunning())
	}
	s.renderHTML(w, "tab-terminal", data)
}

func (s *Server) handleTabDeployment(w http.ResponseWriter, r *http.Request) {
	s.renderHTML(w, "tab-deployment", nil)
}
