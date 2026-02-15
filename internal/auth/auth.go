package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

// AuthSession represents a user's authentication session.
type AuthSession struct {
	Email     string
	CreatedAt time.Time
}

type authContextKey struct{}

// AuthContext holds authentication data for a request lifecycle.
type AuthContext struct {
	Session         *AuthSession
	IsAuthenticated bool
}

// GetAuthContext extracts authentication context from the request.
func GetAuthContext(r *http.Request) AuthContext {
	if ctx, ok := r.Context().Value(authContextKey{}).(AuthContext); ok {
		return ctx
	}
	return AuthContext{}
}

// SetAuthContext returns a new context with the given AuthContext attached.
func SetAuthContext(ctx context.Context, ac AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, ac)
}

// GenerateSessionID creates a secure random session ID.
func GenerateSessionID() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}
