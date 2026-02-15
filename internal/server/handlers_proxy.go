package server

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// createProxyDirector creates a director function for reverse proxy.
func (s *Server) createProxyDirector(target *url.URL, pathPrefix string, originalReq *http.Request) func(*http.Request) {
	return func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host

		originalPath := originalReq.URL.Path
		if pathPrefix != "" && strings.HasPrefix(originalPath, pathPrefix) {
			req.URL.Path = strings.TrimPrefix(originalPath, pathPrefix)
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
		} else {
			req.URL.Path = originalPath
		}

		req.URL.RawQuery = originalReq.URL.RawQuery

		if originalReq.Header.Get("Upgrade") == "websocket" {
			req.Header.Set("Origin", fmt.Sprintf("http://%s", target.Host))
		} else if pathPrefix == "/terminal" {
			req.Header.Set("Origin", fmt.Sprintf("http://%s", target.Host))
		}

		if upgrade := originalReq.Header.Get("Upgrade"); upgrade != "" {
			req.Header.Set("Upgrade", upgrade)
		}
		if connection := originalReq.Header.Get("Connection"); connection != "" {
			req.Header.Set("Connection", connection)
		}
		for key, values := range originalReq.Header {
			if strings.HasPrefix(key, "Sec-Websocket-") {
				for _, value := range values {
					req.Header.Add(key, value)
				}
			}
		}
	}
}

func (s *Server) handleTerminalProxy(w http.ResponseWriter, r *http.Request) {
	if s.Sandbox == nil || !s.Sandbox.IsRunning() {
		http.Error(w, "Terminal not available", http.StatusServiceUnavailable)
		return
	}

	gottyURL := s.Sandbox.GottyURL()
	if gottyURL == "" {
		http.Error(w, "Terminal not supported for this sandbox type", http.StatusNotImplemented)
		return
	}

	target, err := url.Parse(gottyURL)
	if err != nil {
		log.Printf("Error parsing gotty URL: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = s.createProxyDirector(target, "/terminal", r)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Terminal proxy error for %s: %v", r.URL.Path, err)
		http.Error(w, fmt.Sprintf("Terminal proxy error: %v", err), http.StatusBadGateway)
	}

	proxy.ServeHTTP(w, r)
}

func (s *Server) handlePreviewProxy(w http.ResponseWriter, r *http.Request) {
	if s.Sandbox == nil || !s.Sandbox.IsRunning() {
		http.Error(w, "Sandbox not available", http.StatusServiceUnavailable)
		return
	}

	ports := s.detectOpenPorts()
	if len(ports) == 0 {
		s.renderHTML(w, "preview-no-app", nil)
		return
	}

	port := ports[0]

	containerIP := s.Sandbox.ContainerIP()
	if containerIP == "" {
		containerIP = "localhost"
	}

	targetURL := fmt.Sprintf("http://%s:%d", containerIP, port)
	log.Printf("Preview proxy: forwarding to %s", targetURL)

	target, err := url.Parse(targetURL)
	if err != nil {
		log.Printf("Error parsing preview URL: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = s.createProxyDirector(target, "/preview", r)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Preview proxy error: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		data := struct {
			Port int
		}{
			Port: port,
		}
		s.renderHTML(w, "preview-connection-error", data)
	}

	proxy.ServeHTTP(w, r)
}
