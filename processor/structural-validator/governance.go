package structuralvalidator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckAntiMock scans modified test files for mock-heavy patterns.
// It returns a CheckResult that fails if test files define more mock types
// than they test real implementations. The check is advisory (Required: false).
func CheckAntiMock(repoPath string, filesModified []string) CheckResult {
	type violation struct {
		file      string
		mockCount int
		testCount int
	}

	var violations []violation

	for _, f := range filesModified {
		if !strings.HasSuffix(f, "_test.go") {
			continue
		}

		absPath := f
		if !filepath.IsAbs(f) {
			absPath = filepath.Join(repoPath, f)
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			// Skip unreadable files — don't penalise for I/O issues.
			continue
		}

		mockCount, testCount := countMocksAndTests(string(data))
		if mockCount > testCount {
			violations = append(violations, violation{
				file:      f,
				mockCount: mockCount,
				testCount: testCount,
			})
		}
	}

	if len(violations) == 0 {
		return CheckResult{
			Name:     "anti-mock-governance",
			Passed:   true,
			Required: false,
			Command:  "anti-mock-governance (internal)",
			Stdout:   "no mock-heavy test files detected",
		}
	}

	var sb strings.Builder
	sb.WriteString("mock-heavy test files detected (mock types > test functions):\n")
	for _, v := range violations {
		sb.WriteString(fmt.Sprintf("  %s: %d mock types, %d test functions\n", v.file, v.mockCount, v.testCount))
	}

	return CheckResult{
		Name:     "anti-mock-governance",
		Passed:   false,
		Required: false,
		Command:  "anti-mock-governance (internal)",
		Stdout:   sb.String(),
	}
}

// countMocksAndTests counts mock struct definitions and test functions in src.
// mockCount counts lines matching `type Mock<Ident> struct` or `type mock<Ident> struct`.
// testCount counts lines matching `func Test<Ident>`.
func countMocksAndTests(src string) (mockCount, testCount int) {
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if isMockTypeDecl(trimmed) {
			mockCount++
		}
		if isTestFuncDecl(trimmed) {
			testCount++
		}
	}
	return mockCount, testCount
}

// isMockTypeDecl returns true for lines that declare a mock struct type:
// `type Mock<Word> struct` or `type mock<Word> struct`.
func isMockTypeDecl(line string) bool {
	if !strings.HasPrefix(line, "type ") {
		return false
	}
	if !strings.HasSuffix(line, " struct") && !strings.Contains(line, " struct{") && !strings.Contains(line, " struct {") {
		return false
	}
	// Extract the type name token.
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return false
	}
	name := parts[1]
	return strings.HasPrefix(name, "Mock") || strings.HasPrefix(name, "mock")
}

// isTestFuncDecl returns true for lines that declare a test function:
// `func Test<Word>(`.
func isTestFuncDecl(line string) bool {
	if !strings.HasPrefix(line, "func Test") {
		return false
	}
	// Ensure it's followed by an identifier character (not just "func Test_").
	rest := strings.TrimPrefix(line, "func Test")
	if len(rest) == 0 {
		return false
	}
	// Accept any character following "Test" — the naming convention is flexible.
	return true
}
