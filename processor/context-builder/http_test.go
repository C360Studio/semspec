package contextbuilder

import (
	"testing"
)

func TestExtractRequestID(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "standard path",
			path: "/context-builder/responses/req-12345",
			want: "req-12345",
		},
		{
			name: "with trailing slash",
			path: "/context-builder/responses/req-12345/",
			want: "req-12345",
		},
		{
			name: "uuid format",
			path: "/context-builder/responses/550e8400-e29b-41d4-a716-446655440000",
			want: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name: "complex prefix",
			path: "/api/v1/context-builder/responses/myreq",
			want: "myreq",
		},
		{
			name: "no responses segment",
			path: "/context-builder/other/12345",
			want: "",
		},
		{
			name: "empty path",
			path: "",
			want: "",
		},
		{
			name: "just responses",
			path: "/responses/",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRequestID(tt.path)
			if got != tt.want {
				t.Errorf("extractRequestID(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
