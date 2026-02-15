package middleware

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net"
	"net/http"
)

// Middleware represents a standard HTTP middleware following the next pattern.
type Middleware func(http.Handler) http.Handler

// ChainMiddleware applies middlewares in the order provided around a handler.
// The first middleware in the slice is the outermost wrapper.
func ChainMiddleware(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// LoggingResponseWriter wraps http.ResponseWriter to log all responses.
type LoggingResponseWriter struct {
	http.ResponseWriter
	StatusCode int
	Body       *bytes.Buffer
}

// NewLoggingResponseWriter creates a new LoggingResponseWriter.
func NewLoggingResponseWriter(w http.ResponseWriter) *LoggingResponseWriter {
	return &LoggingResponseWriter{
		ResponseWriter: w,
		StatusCode:     200,
		Body:           &bytes.Buffer{},
	}
}

func (lw *LoggingResponseWriter) WriteHeader(code int) {
	lw.StatusCode = code
	lw.ResponseWriter.WriteHeader(code)
}

func (lw *LoggingResponseWriter) Write(data []byte) (int, error) {
	lw.Body.Write(data)
	return lw.ResponseWriter.Write(data)
}

// Hijack implements http.Hijacker so WebSocket upgrades work through the logging wrapper.
func (lw *LoggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := lw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// Flush implements http.Flusher for streaming responses.
func (lw *LoggingResponseWriter) Flush() {
	if f, ok := lw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (lw *LoggingResponseWriter) LogResponse(method, path string) {
	bodyStr := lw.Body.String()
	log.Printf("WIRE_OUT %s %s [%d]: %s", method, path, lw.StatusCode, bodyStr)
}

// LoggingMiddleware is an http.Handler middleware that logs requests and responses.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Special handling for SSE endpoint - don't buffer the entire stream
		if r.URL.Path == "/events" {
			log.Printf("WIRE_OUT SSE connection started: %s %s", r.Method, r.URL.Path)
			next.ServeHTTP(w, r)
			log.Printf("WIRE_OUT SSE connection ended: %s %s", r.Method, r.URL.Path)
			return
		}

		lw := NewLoggingResponseWriter(w)
		next.ServeHTTP(lw, r)
		lw.LogResponse(r.Method, r.URL.Path)
	})
}
