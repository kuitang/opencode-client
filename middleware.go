package main

import "net/http"

// Middleware represents a standard HTTP middleware following the next pattern.
type Middleware func(http.Handler) http.Handler

// chainMiddleware applies middlewares in the order provided around a handler.
// The first middleware in the slice is the outermost wrapper.
func chainMiddleware(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
