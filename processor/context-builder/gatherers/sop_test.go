package gatherers

import "testing"

func TestKeywordsMatch(t *testing.T) {
	tests := []struct {
		name string
		kw1  string
		kw2  string
		want bool
	}{
		// Short keywords require exact match
		{
			name: "short exact match",
			kw1:  "go",
			kw2:  "go",
			want: true,
		},
		{
			name: "short no match different words",
			kw1:  "go",
			kw2:  "py",
			want: false,
		},
		{
			name: "short no substring match - prevents go matching mongo",
			kw1:  "go",
			kw2:  "mongo",
			want: false,
		},
		{
			name: "short no substring match - prevents go matching algorithm",
			kw1:  "algorithm",
			kw2:  "go",
			want: false,
		},

		// Longer keywords allow substring matching
		{
			name: "long substring match forward",
			kw1:  "authentication",
			kw2:  "auth",
			want: true,
		},
		{
			name: "long substring match reverse",
			kw1:  "auth",
			kw2:  "authentication",
			want: true,
		},
		{
			name: "long exact match",
			kw1:  "token",
			kw2:  "token",
			want: true,
		},
		{
			name: "long no match",
			kw1:  "database",
			kw2:  "security",
			want: false,
		},
		{
			name: "long substring in middle",
			kw1:  "oauth2-token",
			kw2:  "token",
			want: true,
		},

		// Edge cases
		{
			name: "empty strings",
			kw1:  "",
			kw2:  "",
			want: true,
		},
		{
			name: "one empty string",
			kw1:  "token",
			kw2:  "",
			want: false,
		},
		{
			name: "boundary length - 3 chars requires exact",
			kw1:  "api",
			kw2:  "api",
			want: true,
		},
		{
			name: "boundary length - 3 chars no substring",
			kw1:  "api",
			kw2:  "rapid",
			want: false,
		},
		{
			name: "exactly 4 chars allows substring",
			kw1:  "auth",
			kw2:  "authentication",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keywordsMatch(tt.kw1, tt.kw2)
			if got != tt.want {
				t.Errorf("keywordsMatch(%q, %q) = %v, want %v", tt.kw1, tt.kw2, got, tt.want)
			}
		})
	}
}

func TestExtractStringArray(t *testing.T) {
	tests := []struct {
		name string
		obj  any
		want []string
	}{
		{
			name: "string slice",
			obj:  []string{"auth", "security"},
			want: []string{"auth", "security"},
		},
		{
			name: "any slice with strings",
			obj:  []any{"auth", "security"},
			want: []string{"auth", "security"},
		},
		{
			name: "any slice with mixed types",
			obj:  []any{"auth", 123, "security"},
			want: []string{"auth", "security"},
		},
		{
			name: "single string",
			obj:  "auth",
			want: []string{"auth"},
		},
		{
			name: "empty string",
			obj:  "",
			want: nil,
		},
		{
			name: "nil",
			obj:  nil,
			want: nil,
		},
		{
			name: "unsupported type",
			obj:  123,
			want: nil,
		},
		{
			name: "empty slice",
			obj:  []string{},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStringArray(tt.obj)

			if len(got) != len(tt.want) {
				t.Errorf("extractStringArray() = %v, want %v", got, tt.want)
				return
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractStringArray()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSOPDocument_Fields(t *testing.T) {
	// Verify SOPDocument struct has all expected fields
	sop := SOPDocument{
		ID:             "test.sop.1",
		Title:          "Test SOP",
		Content:        "Test content",
		AppliesTo:      "*.go",
		Type:           "sop",
		Scope:          "code",
		Severity:       "error",
		Domains:        []string{"auth", "security"},
		RelatedDomains: []string{"validation"},
		Keywords:       []string{"token", "authentication"},
		Authority:      true,
		Tokens:         100,
	}

	if sop.ID != "test.sop.1" {
		t.Errorf("SOPDocument.ID = %q, want %q", sop.ID, "test.sop.1")
	}
	if len(sop.Domains) != 2 {
		t.Errorf("SOPDocument.Domains length = %d, want 2", len(sop.Domains))
	}
	if len(sop.RelatedDomains) != 1 {
		t.Errorf("SOPDocument.RelatedDomains length = %d, want 1", len(sop.RelatedDomains))
	}
	if len(sop.Keywords) != 2 {
		t.Errorf("SOPDocument.Keywords length = %d, want 2", len(sop.Keywords))
	}
	if !sop.Authority {
		t.Error("SOPDocument.Authority = false, want true")
	}
}
