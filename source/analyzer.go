package source

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/c360studio/semspec/llm"
)

// maxAnalysisChars limits document content for LLM analysis.
// ~4000 chars ≈ ~1000 tokens, staying well within context windows
// while providing enough content for accurate classification.
const maxAnalysisChars = 4000

// Analyzer extracts metadata from documents using LLM.
type Analyzer struct {
	client *llm.Client
}

// NewAnalyzer creates a new document analyzer with the given LLM client.
func NewAnalyzer(client *llm.Client) *Analyzer {
	return &Analyzer{client: client}
}

// Analyze extracts metadata from document content using LLM.
// Uses the "fast" capability for quick analysis.
func (a *Analyzer) Analyze(ctx context.Context, content string) (*AnalysisResult, error) {
	// Truncate content if too long
	analysisContent := truncateForAnalysis(content, maxAnalysisChars)

	temp := 0.3 // Low temperature for consistent extraction
	resp, err := a.client.Complete(ctx, llm.Request{
		Capability: "fast",
		Messages: []llm.Message{
			{Role: "system", Content: analysisSystemPrompt},
			{Role: "user", Content: fmt.Sprintf(analysisUserPrompt, analysisContent)},
		},
		Temperature: &temp,
		MaxTokens:   1024,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM analysis failed: %w", err)
	}

	result, err := parseAnalysisResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse analysis response: %w", err)
	}

	return result, nil
}

// truncateForAnalysis truncates content to a maximum length for LLM analysis.
// Tries to truncate at a paragraph boundary if possible.
func truncateForAnalysis(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}

	// Try to find a paragraph break near the limit
	truncated := content[:maxChars]
	lastPara := strings.LastIndex(truncated, "\n\n")
	if lastPara > maxChars/2 {
		return truncated[:lastPara] + "\n\n[Content truncated for analysis...]"
	}

	return truncated + "\n\n[Content truncated for analysis...]"
}

// parseAnalysisResponse extracts AnalysisResult from LLM response.
func parseAnalysisResponse(content string) (*AnalysisResult, error) {
	// Extract JSON from response (may be wrapped in markdown code block)
	jsonStr := extractJSON(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result AnalysisResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}

	// Validate required fields
	if !isValidCategory(result.Category) {
		return nil, fmt.Errorf("invalid category: %q", result.Category)
	}

	// Normalize severity
	if result.Severity != "" && !isValidSeverity(result.Severity) {
		result.Severity = "info" // Default to info for invalid severity
	}

	return &result, nil
}

// extractJSON extracts JSON from a response that may include markdown formatting.
func extractJSON(content string) string {
	// Try to find JSON in code block first
	codeBlockPattern := regexp.MustCompile("```(?:json)?\\s*\\n?([\\s\\S]*?)\\n?```")
	if matches := codeBlockPattern.FindStringSubmatch(content); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Try to find raw JSON object using json.Decoder for correctness
	// This handles strings containing braces properly
	start := strings.Index(content, "{")
	if start == -1 {
		return ""
	}

	// Use json.Decoder to find the valid JSON boundary
	decoder := json.NewDecoder(strings.NewReader(content[start:]))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err == nil {
		return string(raw)
	}

	return ""
}

// isValidCategory checks if a category string is valid.
func isValidCategory(category string) bool {
	switch category {
	case "sop", "spec", "datasheet", "reference", "api":
		return true
	default:
		return false
	}
}

// isValidSeverity checks if a severity string is valid.
func isValidSeverity(severity string) bool {
	switch severity {
	case "error", "warning", "info":
		return true
	default:
		return false
	}
}
