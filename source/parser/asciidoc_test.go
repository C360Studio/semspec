package parser

import (
	"strings"
	"testing"
)

func TestAsciiDocParser_MimeType(t *testing.T) {
	p := NewAsciiDocParser()
	if p.MimeType() != "text/asciidoc" {
		t.Errorf("expected text/asciidoc, got %s", p.MimeType())
	}
}

func TestAsciiDocParser_CanParse(t *testing.T) {
	p := NewAsciiDocParser()

	tests := []struct {
		mimeType string
		want     bool
	}{
		{"text/asciidoc", true},
		{"text/x-asciidoc", true},
		{"text/plain", false},
		{"text/markdown", false},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := p.CanParse(tt.mimeType)
			if got != tt.want {
				t.Errorf("CanParse(%s) = %v, want %v", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestAsciiDocParser_ParseBasicDocument(t *testing.T) {
	p := NewAsciiDocParser()

	content := `= Document Title
:author: John Doe
:version: 1.0

== Introduction

This is the introduction.

=== Subsection

More content here.
`

	doc, err := p.Parse("test.adoc", []byte(content))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if doc.ID == "" {
		t.Error("expected document ID to be set")
	}

	if doc.Filename != "test.adoc" {
		t.Errorf("expected filename test.adoc, got %s", doc.Filename)
	}

	// Check frontmatter extraction
	if doc.Frontmatter == nil {
		t.Fatal("expected frontmatter to be extracted")
	}

	if doc.Frontmatter["title"] != "Document Title" {
		t.Errorf("expected title Document Title, got %v", doc.Frontmatter["title"])
	}

	if doc.Frontmatter["author"] != "John Doe" {
		t.Errorf("expected author John Doe, got %v", doc.Frontmatter["author"])
	}
}

func TestAsciiDocParser_ParseSectionTitles(t *testing.T) {
	p := NewAsciiDocParser()

	content := `== Level 2 Section

Some text.

=== Level 3 Section

More text.

==== Level 4 Section

Even more.
`

	doc, err := p.Parse("test.adoc", []byte(content))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Check markdown conversion
	if !strings.Contains(doc.Body, "## Level 2 Section") {
		t.Error("expected ## Level 2 Section heading")
	}

	if !strings.Contains(doc.Body, "### Level 3 Section") {
		t.Error("expected ### Level 3 Section heading")
	}

	if !strings.Contains(doc.Body, "#### Level 4 Section") {
		t.Error("expected #### Level 4 Section heading")
	}
}

func TestAsciiDocParser_ParseCodeBlock(t *testing.T) {
	p := NewAsciiDocParser()

	content := `== Code Example

[source,python]
----
def hello():
    print("Hello, World!")
----

More text.
`

	doc, err := p.Parse("test.adoc", []byte(content))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Check code block conversion
	if !strings.Contains(doc.Body, "```python") {
		t.Error("expected ```python code fence")
	}

	if !strings.Contains(doc.Body, "def hello():") {
		t.Error("expected code content to be preserved")
	}
}

func TestAsciiDocParser_ParseListingBlock(t *testing.T) {
	p := NewAsciiDocParser()

	content := `== Listing

----
Some code here
More code
----
`

	doc, err := p.Parse("test.adoc", []byte(content))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Should have code fences
	if strings.Count(doc.Body, "```") < 2 {
		t.Error("expected code block fences for listing block")
	}
}

func TestAsciiDocParser_ParseAdmonitions(t *testing.T) {
	p := NewAsciiDocParser()

	content := `== Important Notes

NOTE: This is a note.

WARNING: Be careful!

TIP: Here's a helpful tip.
`

	doc, err := p.Parse("test.adoc", []byte(content))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Check admonition conversion
	if !strings.Contains(doc.Body, "**NOTE:**") {
		t.Error("expected NOTE admonition to be converted")
	}

	if !strings.Contains(doc.Body, "**WARNING:**") {
		t.Error("expected WARNING admonition to be converted")
	}

	if !strings.Contains(doc.Body, "**TIP:**") {
		t.Error("expected TIP admonition to be converted")
	}
}

func TestAsciiDocParser_ParseImageMacro(t *testing.T) {
	p := NewAsciiDocParser()

	content := `== Images

image::diagram.png[Architecture Diagram]
`

	doc, err := p.Parse("test.adoc", []byte(content))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Check image conversion to markdown
	if !strings.Contains(doc.Body, "![Architecture Diagram](diagram.png)") {
		t.Error("expected image macro to be converted to markdown image")
	}
}

func TestAsciiDocParser_ParseLiteralBlock(t *testing.T) {
	p := NewAsciiDocParser()

	content := `== Literal Example

....
Literal text here
With no formatting
....
`

	doc, err := p.Parse("test.adoc", []byte(content))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Should have code fences for literal block
	if strings.Count(doc.Body, "```") < 2 {
		t.Error("expected code block fences for literal block")
	}
}

func TestAsciiDocParser_BooleanAttribute(t *testing.T) {
	p := NewAsciiDocParser()

	content := `= Document
:toc:
:sectnums:

== Content
`

	doc, err := p.Parse("test.adoc", []byte(content))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Boolean attributes should be set to true
	if doc.Frontmatter["toc"] != true {
		t.Errorf("expected toc attribute to be true, got %v", doc.Frontmatter["toc"])
	}

	if doc.Frontmatter["sectnums"] != true {
		t.Errorf("expected sectnums attribute to be true, got %v", doc.Frontmatter["sectnums"])
	}
}
