// Package ts provides TypeScript and JavaScript AST parsing and code entity extraction.
//
// Limitations of regex-based parsing:
// - Does not handle decorators (@Component, etc.)
// - Does not handle multi-line declarations with complex generics
// - Does not handle nested classes or functions defined inside other constructs
// - Does not handle dynamic code patterns or computed property names
// - May miss edge cases with template literals containing code-like strings
//
// For more accurate parsing, consider using esbuild's AST capabilities more deeply
// or calling out to a Node.js process with a proper TypeScript parser.
package ts

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/c360/semspec/processor/ast"
	"github.com/evanw/esbuild/pkg/api"
)

func init() {
	ast.DefaultRegistry.Register("typescript",
		[]string{".ts", ".tsx", ".mts", ".cts"},
		func(org, project, repoRoot string) ast.FileParser {
			return NewParser(org, project, repoRoot)
		})
	ast.DefaultRegistry.Register("javascript",
		[]string{".js", ".jsx", ".mjs", ".cjs"},
		func(org, project, repoRoot string) ast.FileParser {
			return NewParser(org, project, repoRoot)
		})
}

const (
	// maxMethodsPerClass limits memory growth when parsing malformed files
	maxMethodsPerClass = 500
)

// Parser extracts code entities from TypeScript/JavaScript source files
type Parser struct {
	org      string
	project  string
	repoRoot string
}

// NewParser creates a new TypeScript/JavaScript parser
func NewParser(org, project, repoRoot string) *Parser {
	return &Parser{
		org:      org,
		project:  project,
		repoRoot: repoRoot,
	}
}

// ParseFile parses a single TypeScript/JavaScript file and extracts code entities
func (p *Parser) ParseFile(ctx context.Context, filePath string) (*ast.ParseResult, error) {
	// Check context before starting
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Check context after file read (I/O can be slow)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	hash := ast.ComputeHash(content)

	relPath, err := filepath.Rel(p.repoRoot, filePath)
	if err != nil {
		relPath = filePath
	}

	lang := p.detectLanguage(filePath)

	// Use esbuild to transform and validate the file
	loader := p.getLoader(filePath)
	result := api.Transform(string(content), api.TransformOptions{
		Loader:            loader,
		Target:            api.ESNext,
		Format:            api.FormatESModule,
		MinifyWhitespace:  false,
		MinifyIdentifiers: false,
		MinifySyntax:      false,
	})

	// Check context after esbuild transform
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("parse error: %s", result.Errors[0].Text)
	}

	parseResult := &ast.ParseResult{
		Path:     relPath,
		Hash:     hash,
		Imports:  make([]string, 0),
		Entities: make([]*ast.CodeEntity, 0),
	}

	// Create file entity
	fileEntity := ast.NewCodeEntity(p.org, p.project, ast.TypeFile, filepath.Base(filePath), relPath)
	fileEntity.Hash = hash
	fileEntity.Language = lang
	fileEntity.StartLine = 1
	fileEntity.EndLine = countLines(content)

	parseResult.FileEntity = fileEntity
	parseResult.Entities = append(parseResult.Entities, fileEntity)

	// Extract entities from the source
	entities := p.extractEntities(string(content), relPath, lang, fileEntity.ID)
	for _, entity := range entities {
		parseResult.Entities = append(parseResult.Entities, entity)
		fileEntity.Contains = append(fileEntity.Contains, entity.ID)
	}

	// Extract imports
	imports := p.extractImports(string(content))
	fileEntity.Imports = imports
	parseResult.Imports = imports

	return parseResult, nil
}

// ParseDirectory parses all TypeScript/JavaScript files in a directory
func (p *Parser) ParseDirectory(ctx context.Context, dirPath string) ([]*ast.ParseResult, error) {
	var results []*ast.ParseResult

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			base := filepath.Base(path)
			// Skip common non-source directories
			if base == "node_modules" || base == "dist" || base == ".next" ||
				base == "build" || base == "coverage" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if !p.isTargetFile(path) {
			return nil
		}

		result, err := p.ParseFile(ctx, path)
		if err != nil {
			// Log error but continue with other files
			return nil
		}

		results = append(results, result)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	return results, nil
}

// isTargetFile returns true if the file is a TypeScript/JavaScript file
func (p *Parser) isTargetFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs":
		return true
	}
	return false
}

// detectLanguage returns the language identifier for the file
func (p *Parser) detectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	}
	return "javascript"
}

// getLoader returns the esbuild loader for the file type
func (p *Parser) getLoader(filePath string) api.Loader {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".ts", ".mts", ".cts":
		return api.LoaderTS
	case ".tsx":
		return api.LoaderTSX
	case ".jsx":
		return api.LoaderJSX
	default:
		return api.LoaderJS
	}
}

// extractEntities extracts all code entities from the source
func (p *Parser) extractEntities(source, filePath, lang, parentID string) []*ast.CodeEntity {
	var entities []*ast.CodeEntity

	// Extract classes
	entities = append(entities, p.extractClasses(source, filePath, lang, parentID)...)

	// Extract interfaces (TypeScript only)
	if lang == "typescript" {
		entities = append(entities, p.extractInterfaces(source, filePath, lang, parentID)...)
		entities = append(entities, p.extractTypeAliases(source, filePath, lang, parentID)...)
		entities = append(entities, p.extractEnums(source, filePath, lang, parentID)...)
	}

	// Extract functions
	entities = append(entities, p.extractFunctions(source, filePath, lang, parentID)...)

	// Extract top-level constants and variables
	entities = append(entities, p.extractVariables(source, filePath, lang, parentID)...)

	return entities
}

// Regular expressions for parsing TypeScript/JavaScript constructs
var (
	// Class: export? (abstract)? class Name (extends Super.Path)? (implements Interface1, Interface2)?
	classRegex = regexp.MustCompile(`(?m)^(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+(\w+)(?:\s+extends\s+([\w.]+))?(?:\s+implements\s+([\w\s,]+))?\s*\{`)

	// Interface: export? interface Name (extends Interface1, Interface2)?
	interfaceRegex = regexp.MustCompile(`(?m)^(?:export\s+)?interface\s+(\w+)(?:\s+extends\s+([\w\s,]+))?\s*\{`)

	// Type alias: export? type Name = ...
	typeAliasRegex = regexp.MustCompile(`(?m)^(?:export\s+)?type\s+(\w+)\s*(?:<[^>]*>)?\s*=`)

	// Enum: export? (const)? enum Name
	enumRegex = regexp.MustCompile(`(?m)^(?:export\s+)?(?:const\s+)?enum\s+(\w+)\s*\{`)

	// Named function: export? (async)? function Name
	funcRegex = regexp.MustCompile(`(?m)^(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+(\w+)\s*(?:<[^>]*>)?\s*\(`)

	// Arrow function const: export? const Name = (async)? (...) => or const Name = async (...) =>
	arrowFuncRegex = regexp.MustCompile(`(?m)^(?:export\s+)?const\s+(\w+)[^=]*=\s*(?:async\s+)?[^;]*=>`)

	// Const declaration - matches all const, we filter arrow functions in extractVariables
	constRegex = regexp.MustCompile(`(?m)^(?:export\s+)?const\s+(\w+)\s*(?::\s*[^=]+)?\s*=`)

	// Let/var declaration
	letVarRegex = regexp.MustCompile(`(?m)^(?:export\s+)?(?:let|var)\s+(\w+)`)

	// Import statements
	importRegex = regexp.MustCompile(`(?m)^import\s+(?:(?:[\w*{}\s,]+)\s+from\s+)?['"]([^'"]+)['"]`)

	// Class method: (public|private|protected)? (static)? (async)? methodName(
	methodRegex = regexp.MustCompile(`(?m)^\s+(?:(?:public|private|protected)\s+)?(?:static\s+)?(?:async\s+)?(\w+)\s*(?:<[^>]*>)?\s*\(`)
)

// safeExtract safely extracts a substring from source using match indices.
// Returns empty string if indices are invalid.
func safeExtract(source string, start, end int) string {
	if start < 0 || end < 0 || start > end || end > len(source) {
		return ""
	}
	return source[start:end]
}

// extractClasses extracts class declarations
func (p *Parser) extractClasses(source, filePath, lang, parentID string) []*ast.CodeEntity {
	var entities []*ast.CodeEntity
	lines := strings.Split(source, "\n")

	matches := classRegex.FindAllStringSubmatchIndex(source, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}

		name := safeExtract(source, match[2], match[3])
		if name == "" {
			continue
		}
		lineNum := countLinesUntil(source, match[0])

		entity := ast.NewCodeEntity(p.org, p.project, ast.TypeClass, name, filePath)
		entity.Language = lang
		entity.ContainedBy = parentID
		entity.StartLine = lineNum
		entity.Visibility = p.determineVisibility(getMatchedLine(source, match[0]))

		// Extract extends
		if match[4] != -1 && match[5] != -1 {
			superClass := safeExtract(source, match[4], match[5])
			if superClass != "" {
				entity.Extends = append(entity.Extends, p.typeNameToEntityID(superClass, filePath))
			}
		}

		// Extract implements
		if len(match) >= 8 && match[6] != -1 && match[7] != -1 {
			implements := safeExtract(source, match[6], match[7])
			for _, iface := range strings.Split(implements, ",") {
				iface = strings.TrimSpace(iface)
				if iface != "" {
					entity.Implements = append(entity.Implements, p.typeNameToEntityID(iface, filePath))
				}
			}
		}

		// Find class end and extract methods
		endLine, methods := p.extractClassBody(lines, lineNum-1, filePath, lang, entity.ID)
		entity.EndLine = endLine
		for _, method := range methods {
			entity.Contains = append(entity.Contains, method.ID)
			entities = append(entities, method)
		}

		entities = append(entities, entity)
	}

	return entities
}

// extractClassBody finds the class end and extracts methods
func (p *Parser) extractClassBody(lines []string, startIdx int, filePath, lang, parentID string) (int, []*ast.CodeEntity) {
	var methods []*ast.CodeEntity
	braceCount := 0
	started := false
	methodLimitReached := false

	for i := startIdx; i < len(lines); i++ {
		line := lines[i]

		// Count braces
		for _, ch := range line {
			if ch == '{' {
				braceCount++
				started = true
			} else if ch == '}' {
				braceCount--
			}
		}

		// Extract methods (simple heuristic) with limit to prevent unbounded memory growth
		if started && braceCount > 0 && !methodLimitReached {
			if matches := methodRegex.FindStringSubmatch(line); matches != nil {
				methodName := matches[1]
				// Skip constructor and special names
				if methodName != "constructor" && !strings.HasPrefix(methodName, "_") {
					method := ast.NewCodeEntity(p.org, p.project, ast.TypeMethod, methodName, filePath)
					method.Language = lang
					method.ContainedBy = parentID
					method.StartLine = i + 1
					method.EndLine = i + 1 // Simplified - would need more complex logic for actual end
					method.Visibility = p.determineMethodVisibility(line)
					methods = append(methods, method)

					if len(methods) >= maxMethodsPerClass {
						methodLimitReached = true
						slog.Warn("Method limit reached for class",
							"file", filePath,
							"limit", maxMethodsPerClass)
					}
				}
			}
		}

		if started && braceCount == 0 {
			return i + 1, methods
		}
	}

	return len(lines), methods
}

// extractInterfaces extracts interface declarations (TypeScript)
func (p *Parser) extractInterfaces(source, filePath, lang, parentID string) []*ast.CodeEntity {
	var entities []*ast.CodeEntity

	matches := interfaceRegex.FindAllStringSubmatchIndex(source, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}

		name := safeExtract(source, match[2], match[3])
		if name == "" {
			continue
		}
		lineNum := countLinesUntil(source, match[0])

		entity := ast.NewCodeEntity(p.org, p.project, ast.TypeInterface, name, filePath)
		entity.Language = lang
		entity.ContainedBy = parentID
		entity.StartLine = lineNum
		entity.EndLine = lineNum // Simplified
		entity.Visibility = p.determineVisibility(getMatchedLine(source, match[0]))

		// Extract extends
		if len(match) >= 6 && match[4] != -1 && match[5] != -1 {
			extends := safeExtract(source, match[4], match[5])
			for _, ext := range strings.Split(extends, ",") {
				ext = strings.TrimSpace(ext)
				if ext != "" {
					entity.Extends = append(entity.Extends, p.typeNameToEntityID(ext, filePath))
				}
			}
		}

		entities = append(entities, entity)
	}

	return entities
}

// extractTypeAliases extracts type alias declarations (TypeScript)
func (p *Parser) extractTypeAliases(source, filePath, lang, parentID string) []*ast.CodeEntity {
	var entities []*ast.CodeEntity

	matches := typeAliasRegex.FindAllStringSubmatchIndex(source, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}

		name := safeExtract(source, match[2], match[3])
		if name == "" {
			continue
		}
		lineNum := countLinesUntil(source, match[0])

		entity := ast.NewCodeEntity(p.org, p.project, ast.TypeType, name, filePath)
		entity.Language = lang
		entity.ContainedBy = parentID
		entity.StartLine = lineNum
		entity.EndLine = lineNum
		entity.Visibility = p.determineVisibility(getMatchedLine(source, match[0]))

		entities = append(entities, entity)
	}

	return entities
}

// extractEnums extracts enum declarations (TypeScript)
func (p *Parser) extractEnums(source, filePath, lang, parentID string) []*ast.CodeEntity {
	var entities []*ast.CodeEntity

	matches := enumRegex.FindAllStringSubmatchIndex(source, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}

		name := safeExtract(source, match[2], match[3])
		if name == "" {
			continue
		}
		lineNum := countLinesUntil(source, match[0])

		entity := ast.NewCodeEntity(p.org, p.project, ast.TypeEnum, name, filePath)
		entity.Language = lang
		entity.ContainedBy = parentID
		entity.StartLine = lineNum
		entity.EndLine = lineNum
		entity.Visibility = p.determineVisibility(getMatchedLine(source, match[0]))

		entities = append(entities, entity)
	}

	return entities
}

// extractFunctions extracts function declarations
func (p *Parser) extractFunctions(source, filePath, lang, parentID string) []*ast.CodeEntity {
	var entities []*ast.CodeEntity
	seen := make(map[string]bool)

	// Named functions
	matches := funcRegex.FindAllStringSubmatchIndex(source, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}

		name := safeExtract(source, match[2], match[3])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		lineNum := countLinesUntil(source, match[0])

		entity := ast.NewCodeEntity(p.org, p.project, ast.TypeFunction, name, filePath)
		entity.Language = lang
		entity.ContainedBy = parentID
		entity.StartLine = lineNum
		entity.EndLine = lineNum
		entity.Visibility = p.determineVisibility(getMatchedLine(source, match[0]))

		entities = append(entities, entity)
	}

	// Arrow functions assigned to const
	arrowMatches := arrowFuncRegex.FindAllStringSubmatchIndex(source, -1)
	for _, match := range arrowMatches {
		if len(match) < 4 {
			continue
		}

		name := safeExtract(source, match[2], match[3])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		lineNum := countLinesUntil(source, match[0])

		entity := ast.NewCodeEntity(p.org, p.project, ast.TypeFunction, name, filePath)
		entity.Language = lang
		entity.ContainedBy = parentID
		entity.StartLine = lineNum
		entity.EndLine = lineNum
		entity.Visibility = p.determineVisibility(getMatchedLine(source, match[0]))

		entities = append(entities, entity)
	}

	return entities
}

// extractVariables extracts const/let/var declarations
func (p *Parser) extractVariables(source, filePath, lang, parentID string) []*ast.CodeEntity {
	var entities []*ast.CodeEntity
	seen := make(map[string]bool)

	// Mark function names as seen to avoid duplicates
	funcMatches := funcRegex.FindAllStringSubmatch(source, -1)
	for _, match := range funcMatches {
		if len(match) >= 2 && match[1] != "" {
			seen[match[1]] = true
		}
	}
	arrowMatches := arrowFuncRegex.FindAllStringSubmatch(source, -1)
	for _, match := range arrowMatches {
		if len(match) >= 2 && match[1] != "" {
			seen[match[1]] = true
		}
	}

	// Const declarations (non-function)
	constMatches := constRegex.FindAllStringSubmatchIndex(source, -1)
	for _, match := range constMatches {
		if len(match) < 4 {
			continue
		}

		name := safeExtract(source, match[2], match[3])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		lineNum := countLinesUntil(source, match[0])

		entity := ast.NewCodeEntity(p.org, p.project, ast.TypeConst, name, filePath)
		entity.Language = lang
		entity.ContainedBy = parentID
		entity.StartLine = lineNum
		entity.EndLine = lineNum
		entity.Visibility = p.determineVisibility(getMatchedLine(source, match[0]))

		entities = append(entities, entity)
	}

	// Let/var declarations
	letVarMatches := letVarRegex.FindAllStringSubmatchIndex(source, -1)
	for _, match := range letVarMatches {
		if len(match) < 4 {
			continue
		}

		name := safeExtract(source, match[2], match[3])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		lineNum := countLinesUntil(source, match[0])

		entity := ast.NewCodeEntity(p.org, p.project, ast.TypeVar, name, filePath)
		entity.Language = lang
		entity.ContainedBy = parentID
		entity.StartLine = lineNum
		entity.EndLine = lineNum
		entity.Visibility = p.determineVisibility(getMatchedLine(source, match[0]))

		entities = append(entities, entity)
	}

	return entities
}

// extractImports extracts import statements
func (p *Parser) extractImports(source string) []string {
	var imports []string
	seen := make(map[string]bool)

	matches := importRegex.FindAllStringSubmatch(source, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			importPath := match[1]
			if !seen[importPath] {
				seen[importPath] = true
				imports = append(imports, importPath)
			}
		}
	}

	return imports
}

// determineVisibility determines if an identifier is exported
// matchedLine should be the full line where the declaration starts
func (p *Parser) determineVisibility(matchedLine string) ast.Visibility {
	// Check if line starts with export
	trimmed := strings.TrimSpace(matchedLine)
	if strings.HasPrefix(trimmed, "export ") || strings.HasPrefix(trimmed, "export\t") {
		return ast.VisibilityPublic
	}
	return ast.VisibilityPrivate
}

// determineMethodVisibility determines method visibility from modifiers
func (p *Parser) determineMethodVisibility(line string) ast.Visibility {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "private") || strings.HasPrefix(line, "#") {
		return ast.VisibilityPrivate
	}
	if strings.HasPrefix(line, "protected") {
		return ast.VisibilityPrivate // treat protected as private for graph purposes
	}
	return ast.VisibilityPublic
}

// typeNameToEntityID converts a type name to an entity ID reference
func (p *Parser) typeNameToEntityID(typeName, filePath string) string {
	if typeName == "" {
		return ""
	}

	// Strip any generic type parameters
	if idx := strings.Index(typeName, "<"); idx > 0 {
		typeName = typeName[:idx]
	}

	// Check for built-in types
	if isTSBuiltinType(typeName) {
		return fmt.Sprintf("builtin:%s", typeName)
	}

	// Local type: create entity ID within current project
	instance := buildInstanceID(filePath, typeName)
	return fmt.Sprintf("%s.semspec.code.type.%s.%s", p.org, p.project, instance)
}

// buildInstanceID creates a unique instance identifier from path and name.
// Note: This is intentionally duplicated from ast/entities.go to avoid
// import cycles and because the TS version has simpler semantics
// (always includes name, no entity type consideration).
func buildInstanceID(path, name string) string {
	sanitized := strings.ReplaceAll(path, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, ".", "-")
	sanitized = strings.TrimPrefix(sanitized, "-")

	if name != "" {
		return fmt.Sprintf("%s-%s", sanitized, name)
	}
	return sanitized
}

// isTSBuiltinType returns true if the type is a TypeScript/JavaScript built-in type
func isTSBuiltinType(name string) bool {
	switch name {
	case "string", "number", "boolean", "object", "any", "unknown",
		"void", "null", "undefined", "never", "symbol", "bigint",
		"String", "Number", "Boolean", "Object", "Array", "Function",
		"Promise", "Map", "Set", "Date", "RegExp", "Error":
		return true
	}
	return false
}

// countLines counts the total number of lines in content
func countLines(content []byte) int {
	count := 1
	for _, b := range content {
		if b == '\n' {
			count++
		}
	}
	return count
}

// countLinesUntil counts lines from start to the given position
func countLinesUntil(source string, pos int) int {
	lineNum := 1
	for i := 0; i < pos && i < len(source); i++ {
		if source[i] == '\n' {
			lineNum++
		}
	}
	return lineNum
}

// getMatchedLine returns the full line containing the match position
func getMatchedLine(source string, matchStart int) string {
	// Find start of line
	lineStart := matchStart
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}

	// Find end of line
	lineEnd := matchStart
	for lineEnd < len(source) && source[lineEnd] != '\n' {
		lineEnd++
	}

	return source[lineStart:lineEnd]
}

// IsTargetFile returns true if the file is a TypeScript/JavaScript file
func IsTargetFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs":
		return true
	}
	return false
}

