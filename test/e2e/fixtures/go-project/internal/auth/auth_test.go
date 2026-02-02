package auth

import (
	"testing"
)

func TestAuthenticate(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{"valid credentials", "admin", "secret", false},
		{"empty username", "", "secret", true},
		{"empty password", "admin", "", true},
		{"both empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := Authenticate(tt.username, tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("Authenticate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && user == nil {
				t.Error("Authenticate() returned nil user without error")
			}
			if !tt.wantErr && user.Email != tt.username {
				t.Errorf("Authenticate() email = %v, want %v", user.Email, tt.username)
			}
		})
	}
}

func TestRefreshToken(t *testing.T) {
	tests := []struct {
		name         string
		refreshToken string
		wantErr      bool
	}{
		{"valid token", "old-token", false},
		{"empty token", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := RefreshToken(tt.refreshToken)
			if (err != nil) != tt.wantErr {
				t.Errorf("RefreshToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && token == nil {
				t.Error("RefreshToken() returned nil token without error")
			}
		})
	}
}
