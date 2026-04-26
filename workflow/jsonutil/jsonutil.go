// Package jsonutil extracts and cleans JSON from LLM-generated text.
//
// LLMs frequently wrap JSON in markdown code fences, prefix it with prose
// ("Here is the JSON:"), trail it with explanations, or include line
// comments inside the body. ExtractJSON and ExtractJSONArray strip those
// wrappers and return a parseable string. trimToBalancedJSON handles the
// Go 1.25 json.Decoder breaking change where trailing content makes
// Unmarshal fail.
//
// This package is the LLM-output equivalent of "do what the model
// probably meant" — it is intentionally permissive. Strict callers
// should validate after parsing.
package jsonutil

import (
	"encoding/json"
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
// Go 1.25+ rejects trailing content after top-level JSON value, so the output
// is validated and trimmed to ensure clean unmarshalling.
func ExtractJSON(content string) string {
	raw := extractRawJSON(content)
	if raw == "" {
		return ""
	}
	cleaned := cleanJSON(raw)
	// Go 1.25 rejects trailing content after JSON. Validate and trim if needed.
	if json.Valid([]byte(cleaned)) {
		return cleaned
	}
	// Try to find the balanced JSON object boundary.
	if trimmed := trimToBalancedJSON(cleaned); trimmed != "" {
		return trimmed
	}
	return cleaned
}

// ExtractJSONArray extracts a JSON array from an LLM response string.
func ExtractJSONArray(content string) string {
	var raw string
	// Try markdown code block first
	if matches := jsonArrayBlockPattern.FindStringSubmatch(content); len(matches) > 1 {
		raw = matches[1]
	} else if matches := jsonArrayPattern.FindString(content); matches != "" {
		raw = matches
	}
	if raw == "" {
		return ""
	}
	cleaned := cleanJSON(raw)
	if json.Valid([]byte(cleaned)) {
		return cleaned
	}
	return cleaned
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

// trimToBalancedJSON finds the substring from the first { to its balanced },
// handling nested braces and string escapes. Returns "" if no balanced object found.
func trimToBalancedJSON(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
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
		if inString {
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				candidate := s[start : i+1]
				if json.Valid([]byte(candidate)) {
					return candidate
				}
				return ""
			}
		}
	}
	return ""
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
