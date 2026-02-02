package astindexer

import (
	"testing"

	"github.com/c360studio/semspec/processor/ast"
	// ts package is already imported in component.go via blank import
)

func TestDefaultRegistry_TSJSParsersRegistered(t *testing.T) {
	// The ts package is imported via blank import in component.go,
	// which triggers init() registration of TS/JS parsers.

	// Check TypeScript parser
	if !ast.DefaultRegistry.HasParser("typescript") {
		t.Error("expected 'typescript' parser to be registered in DefaultRegistry")
	}

	tsExts := []string{".ts", ".tsx", ".mts", ".cts"}
	for _, ext := range tsExts {
		name, ok := ast.DefaultRegistry.GetParserName(ext)
		if !ok || name != "typescript" {
			t.Errorf("expected %s extension to map to 'typescript' parser, got %q (ok=%v)", ext, name, ok)
		}
	}

	// Check JavaScript parser
	if !ast.DefaultRegistry.HasParser("javascript") {
		t.Error("expected 'javascript' parser to be registered in DefaultRegistry")
	}

	jsExts := []string{".js", ".jsx", ".mjs", ".cjs"}
	for _, ext := range jsExts {
		name, ok := ast.DefaultRegistry.GetParserName(ext)
		if !ok || name != "javascript" {
			t.Errorf("expected %s extension to map to 'javascript' parser, got %q (ok=%v)", ext, name, ok)
		}
	}
}

func TestDefaultRegistry_AllParsersAvailable(t *testing.T) {
	// Verify all expected parsers are registered
	expectedParsers := []string{"go", "typescript", "javascript"}

	for _, name := range expectedParsers {
		if !ast.DefaultRegistry.HasParser(name) {
			t.Errorf("expected parser %q to be registered", name)
		}
	}

	// Verify we can create each parser
	for _, name := range expectedParsers {
		parser, err := ast.DefaultRegistry.CreateParser(name, "testorg", "testproj", "/test/root")
		if err != nil {
			t.Errorf("CreateParser(%q) failed: %v", name, err)
		}
		if parser == nil {
			t.Errorf("CreateParser(%q) returned nil", name)
		}
	}
}
