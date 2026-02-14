package repoingester

import (
	"os"
	"path/filepath"
	"strings"
)

// LanguageInfo contains metadata about a programming language.
type LanguageInfo struct {
	Name       string
	Extensions []string
	Color      string // For UI display
}

// KnownLanguages maps language identifiers to their info.
var KnownLanguages = map[string]LanguageInfo{
	"go": {
		Name:       "Go",
		Extensions: []string{".go"},
		Color:      "#00ADD8",
	},
	"typescript": {
		Name:       "TypeScript",
		Extensions: []string{".ts", ".tsx", ".mts", ".cts"},
		Color:      "#3178C6",
	},
	"javascript": {
		Name:       "JavaScript",
		Extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
		Color:      "#F7DF1E",
	},
	"python": {
		Name:       "Python",
		Extensions: []string{".py", ".pyi", ".pyw"},
		Color:      "#3776AB",
	},
	"rust": {
		Name:       "Rust",
		Extensions: []string{".rs"},
		Color:      "#DEA584",
	},
	"java": {
		Name:       "Java",
		Extensions: []string{".java"},
		Color:      "#B07219",
	},
	"kotlin": {
		Name:       "Kotlin",
		Extensions: []string{".kt", ".kts"},
		Color:      "#A97BFF",
	},
	"swift": {
		Name:       "Swift",
		Extensions: []string{".swift"},
		Color:      "#F05138",
	},
	"csharp": {
		Name:       "C#",
		Extensions: []string{".cs", ".csx"},
		Color:      "#512BD4",
	},
	"cpp": {
		Name:       "C++",
		Extensions: []string{".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx", ".h"},
		Color:      "#F34B7D",
	},
	"c": {
		Name:       "C",
		Extensions: []string{".c", ".h"},
		Color:      "#555555",
	},
	"ruby": {
		Name:       "Ruby",
		Extensions: []string{".rb", ".rake", ".gemspec"},
		Color:      "#CC342D",
	},
	"php": {
		Name:       "PHP",
		Extensions: []string{".php", ".phtml"},
		Color:      "#777BB4",
	},
	"svelte": {
		Name:       "Svelte",
		Extensions: []string{".svelte"},
		Color:      "#FF3E00",
	},
	"vue": {
		Name:       "Vue",
		Extensions: []string{".vue"},
		Color:      "#41B883",
	},
	"html": {
		Name:       "HTML",
		Extensions: []string{".html", ".htm"},
		Color:      "#E34C26",
	},
	"css": {
		Name:       "CSS",
		Extensions: []string{".css", ".scss", ".sass", ".less"},
		Color:      "#563D7C",
	},
	"sql": {
		Name:       "SQL",
		Extensions: []string{".sql"},
		Color:      "#E38C00",
	},
	"shell": {
		Name:       "Shell",
		Extensions: []string{".sh", ".bash", ".zsh"},
		Color:      "#89E051",
	},
	"yaml": {
		Name:       "YAML",
		Extensions: []string{".yaml", ".yml"},
		Color:      "#CB171E",
	},
	"json": {
		Name:       "JSON",
		Extensions: []string{".json", ".jsonc"},
		Color:      "#292929",
	},
	"markdown": {
		Name:       "Markdown",
		Extensions: []string{".md", ".mdx"},
		Color:      "#083FA1",
	},
}

// buildExtensionMap creates a map from extension to language key.
func buildExtensionMap() map[string]string {
	m := make(map[string]string)
	for lang, info := range KnownLanguages {
		for _, ext := range info.Extensions {
			m[ext] = lang
		}
	}
	return m
}

var extensionToLanguage = buildExtensionMap()

// DetectLanguages scans a directory and returns detected programming languages.
// It returns a slice of language identifiers (e.g., ["go", "typescript"]).
func DetectLanguages(repoPath string) ([]string, error) {
	languageCounts := make(map[string]int)

	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip directories
		if info.IsDir() {
			base := filepath.Base(path)
			// Skip hidden directories and common non-source directories
			if strings.HasPrefix(base, ".") ||
				base == "node_modules" ||
				base == "vendor" ||
				base == "dist" ||
				base == "build" ||
				base == "__pycache__" ||
				base == "target" {
				return filepath.SkipDir
			}
			return nil
		}

		// Get extension
		ext := strings.ToLower(filepath.Ext(path))
		if lang, ok := extensionToLanguage[ext]; ok {
			languageCounts[lang]++
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Extract languages with at least some files
	var languages []string
	for lang, count := range languageCounts {
		if count >= 1 {
			languages = append(languages, lang)
		}
	}

	return languages, nil
}

// GetSupportedASTLanguages returns the languages that can be parsed by the AST indexer.
func GetSupportedASTLanguages() []string {
	return []string{"go", "typescript", "javascript"}
}

// FilterASTLanguages filters a list of languages to only those supported by AST indexer.
func FilterASTLanguages(languages []string) []string {
	supported := make(map[string]bool)
	for _, lang := range GetSupportedASTLanguages() {
		supported[lang] = true
	}

	var result []string
	for _, lang := range languages {
		if supported[lang] {
			result = append(result, lang)
		}
	}
	return result
}
