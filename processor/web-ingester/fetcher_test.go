package webingester

import (
	"testing"

	"github.com/c360studio/semspec/source/weburl"
)

// TestValidateURL tests URL validation via the shared weburl package.
// The actual validation logic is tested in source/weburl/weburl_test.go.
// This test ensures the fetcher correctly integrates with the shared package.
func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid https URL",
			url:     "https://go.dev/doc/effective_go",
			wantErr: false,
		},
		{
			name:    "http URL rejected",
			url:     "http://example.com",
			wantErr: true,
		},
		{
			name:    "localhost rejected",
			url:     "https://localhost:8080",
			wantErr: true,
		},
		{
			name:    "private IP rejected",
			url:     "https://192.168.1.1/path",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := weburl.ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}
