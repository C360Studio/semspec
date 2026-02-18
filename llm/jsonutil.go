package llm

import (
	"regexp"
	"strings"
)

// Pre-compiled regex patterns for JSON extraction from LLM responses.
var (
	// jsonBlockPattern matches JSON inside markdown code blocks: ```json { ... } ```
	jsonBlockPattern = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\{.*\\})\\s*```")
	// jsonObjectPattern matches any JSON object (greedy fallback).
	jsonObjectPattern = regexp.MustCompile(`(?s)\{[\s\S]*\}`)
	// jsonArrayBlockPattern matches JSON arrays inside markdown code blocks.
	jsonArrayBlockPattern = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\[.*\\])\\s*```")
	// jsonArrayPattern matches any JSON array (greedy fallback).
	jsonArrayPattern = regexp.MustCompile(`(?s)\[[\s\S]*\]`)
	// trailingCommaPattern matches trailing commas before ] or }.
	trailingCommaPattern = regexp.MustCompile(`,\s*([}\]])`)
)

// ExtractJSON extracts a JSON object from an LLM response string.
// It handles markdown code blocks, JavaScript-style comments, and trailing commas.
func ExtractJSON(content string) string {
	raw := extractRawJSON(content)
	if raw == "" {
		return ""
	}
	return cleanJSON(raw)
}

// ExtractJSONArray extracts a JSON array from an LLM response string.
func ExtractJSONArray(content string) string {
	// Try markdown code block first
	if matches := jsonArrayBlockPattern.FindStringSubmatch(content); len(matches) > 1 {
		return cleanJSON(matches[1])
	}
	// Fallback to raw array
	if matches := jsonArrayPattern.FindString(content); matches != "" {
		return cleanJSON(matches)
	}
	return ""
}

// extractRawJSON extracts raw JSON content before cleaning.
func extractRawJSON(content string) string {
	// Try markdown code block first
	if matches := jsonBlockPattern.FindStringSubmatch(content); len(matches) > 1 {
		return matches[1]
	}
	// Fallback to raw JSON object
	if matches := jsonObjectPattern.FindString(content); matches != "" {
		return matches
	}
	return ""
}

// cleanJSON removes JavaScript-style comments and trailing commas from JSON.
// LLMs commonly produce these invalid JSON artifacts.
func cleanJSON(raw string) string {
	// Remove // comments that are NOT inside JSON string values.
	// Strategy: process line by line, only strip comments outside of strings.
	lines := strings.Split(raw, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned = append(cleaned, stripLineComment(line))
	}
	result := strings.Join(cleaned, "\n")

	// Remove trailing commas before } or ]
	result = trailingCommaPattern.ReplaceAllString(result, "$1")

	return result
}

// stripLineComment removes a // comment from a JSON line, respecting string values.
// For example:
//
//	"path/to/file.js",          // This is a comment  → "path/to/file.js",
//	"url": "http://example.com" // comment             → "url": "http://example.com"
//	"url": "http://example.com"                        → "url": "http://example.com" (no change)
func stripLineComment(line string) string {
	// Fast path: no // at all
	if !strings.Contains(line, "//") {
		return line
	}

	// Walk the line character by character, tracking whether we're inside a string.
	inString := false
	escaped := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if !inString && ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			// Found a comment outside a string — strip from here
			trimmed := strings.TrimRight(line[:i], " \t")
			return trimmed
		}
	}
	return line
}
