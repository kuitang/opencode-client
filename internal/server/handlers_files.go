package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"opencode-chat/internal/models"
)

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	filepath := r.URL.Query().Get("path")

	if cookie, err := r.Cookie("session"); err == nil && filepath != "" {
		s.mu.Lock()
		if s.selectedFiles == nil {
			s.selectedFiles = make(map[string]string)
		}
		s.selectedFiles[cookie.Value] = filepath
		s.mu.Unlock()
		log.Printf("handleFileContent: saved selected file %s for session %s", filepath, cookie.Value)
	}

	if filepath == "" {
		s.renderHTML(w, "code-placeholder", nil)
		return
	}

	if s.Sandbox == nil || !s.Sandbox.IsRunning() {
		http.Error(w, "Sandbox not available", http.StatusServiceUnavailable)
		return
	}

	opencodeURL := s.Sandbox.OpencodeURL()
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
		s.renderHTML(w, "code-error", data)
		return
	}

	var fileContent models.FileContent
	if err := json.NewDecoder(resp.Body).Decode(&fileContent); err != nil {
		log.Printf("Failed to decode file content: %v", err)
		http.Error(w, "Failed to decode file content", http.StatusInternalServerError)
		return
	}

	lines := strings.Split(fileContent.Content, "\n")
	data := struct {
		Filepath string
		Lines    []string
	}{
		Filepath: filepath,
		Lines:    lines,
	}

	s.renderHTML(w, "code-content", data)
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	currentPath := r.URL.Query().Get("current")
	optionsOnly := r.URL.Query().Get("options_only") == "true"

	files, err := s.fetchAllFiles()
	if err != nil {
		log.Printf("Failed to fetch files from OpenCode: %v", err)
		files = []models.FileNode{}
	}

	lineCount := s.calculateLineCount()

	data := struct {
		Files       []models.FileNode
		FileCount   int
		LineCount   int
		CurrentPath string
	}{
		Files:       files,
		FileCount:   len(files),
		LineCount:   lineCount,
		CurrentPath: currentPath,
	}

	if optionsOnly {
		s.renderHTML(w, "file-options-with-counts", data)
	} else {
		s.renderHTML(w, "file-dropdown", data)
	}
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleDownload: Starting zip download")

	if s.Sandbox == nil {
		http.Error(w, "Sandbox not available", http.StatusServiceUnavailable)
		return
	}

	if !s.Sandbox.IsRunning() {
		http.Error(w, "Sandbox not running", http.StatusServiceUnavailable)
		return
	}

	zipReader, err := s.Sandbox.DownloadZip()
	if err != nil {
		log.Printf("handleDownload: Failed to create zip: %v", err)
		http.Error(w, "Failed to create zip archive", http.StatusInternalServerError)
		return
	}
	defer zipReader.Close()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=opencode-workspace.zip")
	w.Header().Set("Cache-Control", "no-cache")

	bytesWritten, err := copyIO(w, zipReader)
	if err != nil {
		log.Printf("handleDownload: Failed to stream zip: %v", err)
		return
	}

	log.Printf("handleDownload: Successfully streamed %d bytes", bytesWritten)
}

// fetchAllFiles recursively fetches all files from OpenCode sandbox.
func (s *Server) fetchAllFiles() ([]models.FileNode, error) {
	if s.Sandbox == nil || !s.Sandbox.IsRunning() {
		return nil, fmt.Errorf("sandbox not available")
	}

	opencodeURL := s.Sandbox.OpencodeURL()
	allFiles := []models.FileNode{}

	var fetchDir func(path string) error
	fetchDir = func(path string) error {
		url := fmt.Sprintf("%s/file?path=%s", opencodeURL, path)
		resp, err := http.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		var nodes []models.FileNode
		if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
			return err
		}

		for _, node := range nodes {
			if node.Type == "file" {
				allFiles = append(allFiles, node)
			} else if node.Type == "directory" {
				if err := fetchDir(node.Path); err != nil {
					log.Printf("Error fetching directory %s: %v", node.Path, err)
				}
			}
		}
		return nil
	}

	if err := fetchDir("."); err != nil {
		return nil, err
	}

	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].Path < allFiles[j].Path
	})

	return allFiles, nil
}

// sendCodeTabUpdates sends combined file stats and dropdown updates via SSE.
func (s *Server) sendCodeTabUpdates(w http.ResponseWriter, flusher http.Flusher, currentPath string) {
	files, err := s.fetchAllFiles()
	if err != nil {
		log.Printf("Failed to fetch files for code tab update: %v", err)
		return
	}

	lineCount := s.calculateLineCount()

	data := struct {
		Files       []models.FileNode
		FileCount   int
		LineCount   int
		CurrentPath string
	}{
		Files:       files,
		FileCount:   len(files),
		LineCount:   lineCount,
		CurrentPath: currentPath,
	}

	html, err := s.renderHTMLToString("code-updates-oob", data)
	if err != nil {
		log.Printf("Failed to render code updates OOB: %v", err)
		return
	}

	fmt.Fprintf(w, "event: code-updates\n")
	lines := strings.Split(strings.TrimSpace(html), "\n")
	for _, line := range lines {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprintf(w, "\n")
	flusher.Flush()
	log.Printf("Sent code tab update: %d files, %d lines", data.FileCount, data.LineCount)
}

// handleKillPreviewPort handles killing a process listening on a specific port.
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

	command := fmt.Sprintf("kill $(lsof -t -i:%s) 2>/dev/null || true", port)
	_, err := s.executeShellCommand(sessionID, command)
	if err != nil {
		log.Printf("handleKillPreviewPort: Failed to execute kill command: %v", err)
	}

	// Small delay to ensure process is killed
	// (using time.Sleep is intentional here for subprocess cleanup)
	sleepForProcessKill()

	s.handleTabPreview(w, r)
}

// ports.go functionality

// detectOpenPorts uses lsof to find open listening ports in the sandbox.
func (s *Server) detectOpenPorts() []int {
	if s.Sandbox == nil || !s.Sandbox.IsRunning() {
		return []int{}
	}

	s.mu.RLock()
	sessionID := s.workspaceSession
	s.mu.RUnlock()

	if sessionID == "" {
		log.Printf("detectOpenPorts: no workspace session available")
		return []int{}
	}

	return s.findUserPorts(sessionID)
}

// findUserPorts finds all listening ports excluding system services.
func (s *Server) findUserPorts(sessionID string) []int {
	command := `lsof -i -sTCP:LISTEN -P -n | grep -o ':[0-9]*' | sed 's/://' | sort -u`

	outputText, err := s.executeShellCommand(sessionID, command)
	if err != nil {
		log.Printf("findUserPorts: failed to execute command: %v", err)
		return []int{}
	}
	log.Printf("findUserPorts: raw lsof output: %q", outputText)

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

		if port == 8080 || port == 8081 || port == 7681 || port < 1024 {
			continue
		}

		ports = append(ports, port)
	}

	log.Printf("findUserPorts: found user ports %v", ports)
	return ports
}

// calculateLineCount runs wc -l command via OpenCode shell to get total line count.
func (s *Server) calculateLineCount() int {
	if s.Sandbox == nil || !s.Sandbox.IsRunning() {
		return 0
	}

	s.mu.RLock()
	sessionID := s.workspaceSession
	s.mu.RUnlock()

	if sessionID == "" {
		log.Printf("calculateLineCount: no workspace session available")
		return 0
	}

	command := "find . -type f -exec wc -l {} + 2>/dev/null | tail -1 | awk '{print $1}'"
	output, err := s.executeShellCommand(sessionID, command)
	if err != nil {
		log.Printf("Failed to run shell command: %v", err)
		return 0
	}

	output = strings.TrimSpace(output)
	var lineCount int
	fmt.Sscanf(output, "%d", &lineCount)
	return lineCount
}
