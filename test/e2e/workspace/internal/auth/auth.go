package auth

import (
	"errors"
	"time"
)

// User represents an authenticated user.
type User struct {
	ID        string
	Email     string
	CreatedAt time.Time
}

// Token represents an auth token.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// ErrInvalidCredentials is returned for bad login.
var ErrInvalidCredentials = errors.New("invalid credentials")

// Authenticate validates credentials and returns a user.
func Authenticate(username, password string) (*User, error) {
	// Simplified auth logic
	if username == "" || password == "" {
		return nil, ErrInvalidCredentials
	}
	return &User{
		ID:        "user-1",
		Email:     username,
		CreatedAt: time.Now(),
	}, nil
}

// RefreshToken generates a new token pair.
func RefreshToken(refreshToken string) (*Token, error) {
	if refreshToken == "" {
		return nil, errors.New("refresh token required")
	}
	return &Token{
		AccessToken:  "new-access-token",
		RefreshToken: "new-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
	}, nil
}
