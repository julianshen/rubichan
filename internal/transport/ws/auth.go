package ws

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

// AuthClaims carries the validated identity and permissions of a connection.
type AuthClaims struct {
	Subject string // user identifier
}

// Authenticator validates connection tokens.
type Authenticator interface {
	Validate(token string) (AuthClaims, error)
}

// ErrUnauthorized is returned when a token is missing or invalid.
var ErrUnauthorized = errors.New("ws: unauthorized")

// NoopAuth allows all connections without checking tokens.
type NoopAuth struct{}

// Validate implements Authenticator — always succeeds.
func (NoopAuth) Validate(_ string) (AuthClaims, error) {
	return AuthClaims{Subject: "anonymous"}, nil
}

// StaticTokenAuth validates connections against a pre-shared token.
type StaticTokenAuth struct {
	token string
}

// NewStaticTokenAuth creates a StaticTokenAuth that validates against the given token.
// Returns an error if token is empty (an empty token would match missing tokens).
func NewStaticTokenAuth(token string) (StaticTokenAuth, error) {
	if token == "" {
		return StaticTokenAuth{}, errors.New("ws: token must not be empty")
	}
	return StaticTokenAuth{token: token}, nil
}

// Validate implements Authenticator — checks that token matches.
func (a StaticTokenAuth) Validate(token string) (AuthClaims, error) {
	if subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) != 1 {
		return AuthClaims{}, ErrUnauthorized
	}
	return AuthClaims{Subject: "user"}, nil
}

// TokenFromRequest extracts a bearer token from the request.
// It checks the Authorization header first, then the "token" query parameter.
func TokenFromRequest(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
	}
	return r.URL.Query().Get("token")
}
