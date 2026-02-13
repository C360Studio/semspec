// Package source provides types and parsers for document ingestion.
package source

import (
	"time"

	vocab "github.com/c360studio/semspec/vocabulary/source"
)

// Source represents a knowledge source (document or repository).
type Source struct {
	// ID is the unique identifier for this source.
	ID string `json:"id"`

	// Name is the display name.
	Name string `json:"name"`

	// Type discriminates between document and repository sources.
	Type vocab.SourceTypeValue `json:"type"`

	// Status tracks the processing state.
	Status vocab.SourceStatusType `json:"status"`

	// Project groups related sources for context assembly.
	Project string `json:"project,omitempty"`

	// AddedBy identifies who added this source.
	AddedBy string `json:"added_by,omitempty"`

	// AddedAt is when the source was added.
	AddedAt time.Time `json:"added_at"`

	// Error holds any processing error message.
	Error string `json:"error,omitempty"`
}

// DocumentSource represents an ingested document with LLM-extracted metadata.
type DocumentSource struct {
	Source

	// Filename is the original filename.
	Filename string `json:"filename"`

	// MimeType is the document MIME type.
	MimeType string `json:"mime_type"`

	// FilePath is the path relative to .semspec/sources/docs/.
	FilePath string `json:"file_path"`

	// FileHash is the content hash for staleness detection.
	FileHash string `json:"file_hash,omitempty"`

	// Category classifies the document (sop, spec, datasheet, reference, api).
	Category vocab.DocCategoryType `json:"category"`

	// AppliesTo lists file patterns this document applies to.
	AppliesTo []string `json:"applies_to,omitempty"`

	// Severity indicates violation severity for SOPs.
	Severity vocab.DocSeverityType `json:"severity,omitempty"`

	// Summary is the LLM-extracted summary.
	Summary string `json:"summary,omitempty"`

	// Requirements are extracted key rules.
	Requirements []string `json:"requirements,omitempty"`

	// ChunkCount is the total number of chunks.
	ChunkCount int `json:"chunk_count,omitempty"`
}

// RepositorySource represents an external git repository.
type RepositorySource struct {
	Source

	// URL is the git clone URL.
	URL string `json:"url"`

	// Branch is the branch name to track.
	Branch string `json:"branch"`

	// Languages are the detected programming languages.
	Languages []string `json:"languages,omitempty"`

	// EntityCount is the number of indexed entities.
	EntityCount int `json:"entity_count,omitempty"`

	// LastIndexed is when the repo was last indexed.
	LastIndexed *time.Time `json:"last_indexed,omitempty"`

	// LastCommit is the SHA of the last indexed commit.
	LastCommit string `json:"last_commit,omitempty"`

	// AutoPull indicates whether to auto-pull for updates.
	AutoPull bool `json:"auto_pull,omitempty"`

	// PullInterval is the auto-pull interval.
	PullInterval string `json:"pull_interval,omitempty"`
}

// Chunk represents a section of a document for context assembly.
type Chunk struct {
	// ParentID is the ID of the parent document.
	ParentID string `json:"parent_id"`

	// Index is the chunk sequence number (0-indexed internally, 1-indexed for display).
	Index int `json:"index"`

	// Section is the heading or section name.
	Section string `json:"section,omitempty"`

	// Content is the chunk text.
	Content string `json:"content"`

	// TokenCount is the estimated token count.
	TokenCount int `json:"token_count"`
}

// Document represents a parsed document with its content and metadata.
type Document struct {
	// ID is the document identifier (typically derived from file path).
	ID string `json:"id"`

	// Filename is the original filename.
	Filename string `json:"filename"`

	// Content is the raw document content.
	Content string `json:"content"`

	// Frontmatter contains parsed YAML frontmatter if present.
	Frontmatter map[string]any `json:"frontmatter,omitempty"`

	// Body is the content without frontmatter.
	Body string `json:"body"`
}

// HasFrontmatter returns true if the document has parsed frontmatter.
func (d *Document) HasFrontmatter() bool {
	return len(d.Frontmatter) > 0
}

// FrontmatterAsAnalysis converts frontmatter to AnalysisResult if valid.
// Returns nil if frontmatter doesn't contain analysis fields.
func (d *Document) FrontmatterAsAnalysis() *AnalysisResult {
	if !d.HasFrontmatter() {
		return nil
	}

	result := &AnalysisResult{}

	// Extract category
	if cat, ok := d.Frontmatter["category"].(string); ok {
		result.Category = cat
	}

	// Extract applies_to
	if appliesTo, ok := d.Frontmatter["applies_to"].([]any); ok {
		for _, v := range appliesTo {
			if s, ok := v.(string); ok {
				result.AppliesTo = append(result.AppliesTo, s)
			}
		}
	} else if appliesTo, ok := d.Frontmatter["applies_to"].([]string); ok {
		result.AppliesTo = appliesTo
	}

	// Extract severity
	if sev, ok := d.Frontmatter["severity"].(string); ok {
		result.Severity = sev
	}

	// Extract summary
	if sum, ok := d.Frontmatter["summary"].(string); ok {
		result.Summary = sum
	}

	// Extract requirements
	if reqs, ok := d.Frontmatter["requirements"].([]any); ok {
		for _, v := range reqs {
			if s, ok := v.(string); ok {
				result.Requirements = append(result.Requirements, s)
			}
		}
	} else if reqs, ok := d.Frontmatter["requirements"].([]string); ok {
		result.Requirements = reqs
	}

	// Return nil if no useful fields were extracted
	if result.Category == "" && len(result.AppliesTo) == 0 {
		return nil
	}

	return result
}

// AnalysisResult contains LLM-extracted document metadata.
type AnalysisResult struct {
	// Category classifies the document type.
	Category string `json:"category"`

	// AppliesTo lists file patterns this document applies to.
	AppliesTo []string `json:"applies_to"`

	// Severity indicates violation severity for SOPs.
	Severity string `json:"severity,omitempty"`

	// Summary is a brief description.
	Summary string `json:"summary,omitempty"`

	// Requirements are extracted key rules.
	Requirements []string `json:"requirements,omitempty"`
}

// IsValid checks if the analysis result has required fields.
func (a *AnalysisResult) IsValid() bool {
	return a != nil && a.Category != ""
}

// CategoryType returns the category as a vocabulary enum.
func (a *AnalysisResult) CategoryType() vocab.DocCategoryType {
	switch a.Category {
	case "sop":
		return vocab.DocCategorySOP
	case "spec":
		return vocab.DocCategorySpec
	case "datasheet":
		return vocab.DocCategoryDatasheet
	case "reference":
		return vocab.DocCategoryReference
	case "api":
		return vocab.DocCategoryAPI
	default:
		return vocab.DocCategoryReference
	}
}

// SeverityType returns the severity as a vocabulary enum.
func (a *AnalysisResult) SeverityType() vocab.DocSeverityType {
	switch a.Severity {
	case "error":
		return vocab.DocSeverityError
	case "warning":
		return vocab.DocSeverityWarning
	case "info":
		return vocab.DocSeverityInfo
	default:
		return vocab.DocSeverityInfo
	}
}
