package source

// DocCategoryType represents document classification categories.
type DocCategoryType string

const (
	// DocCategorySOP is a Standard Operating Procedure document.
	// SOPs define validation rules and review requirements.
	DocCategorySOP DocCategoryType = "sop"

	// DocCategorySpec is a technical specification document.
	// Specs define system behavior and requirements.
	DocCategorySpec DocCategoryType = "spec"

	// DocCategoryDatasheet is a data format documentation.
	// Datasheets describe data structures and formats.
	DocCategoryDatasheet DocCategoryType = "datasheet"

	// DocCategoryReference is general reference material.
	// Reference docs provide background and context.
	DocCategoryReference DocCategoryType = "reference"

	// DocCategoryAPI is API documentation.
	// API docs describe endpoints, schemas, and usage.
	DocCategoryAPI DocCategoryType = "api"
)

// DocSeverityType represents SOP violation severity levels.
type DocSeverityType string

const (
	// DocSeverityError indicates a violation that blocks approval.
	// Reviewer cannot approve if this SOP is violated.
	DocSeverityError DocSeverityType = "error"

	// DocSeverityWarning indicates a violation that should be addressed.
	// Reviewer has discretion on whether to approve.
	DocSeverityWarning DocSeverityType = "warning"

	// DocSeverityInfo indicates informational guidance.
	// No enforcement; purely advisory.
	DocSeverityInfo DocSeverityType = "info"
)

// SourceStatusType represents the status of a source.
type SourceStatusType string

const (
	// SourceStatusPending indicates the source is queued for processing.
	SourceStatusPending SourceStatusType = "pending"

	// SourceStatusIndexing indicates the source is currently being processed.
	SourceStatusIndexing SourceStatusType = "indexing"

	// SourceStatusReady indicates the source is fully processed and available.
	SourceStatusReady SourceStatusType = "ready"

	// SourceStatusError indicates processing failed.
	SourceStatusError SourceStatusType = "error"

	// SourceStatusStale indicates the source may be out of date.
	// For repos: new commits available. For docs: file modified.
	SourceStatusStale SourceStatusType = "stale"
)

// SourceTypeValue represents the type discriminator for sources.
type SourceTypeValue string

const (
	// SourceTypeRepository indicates an external git repository.
	SourceTypeRepository SourceTypeValue = "repository"

	// SourceTypeDocument indicates an ingested document.
	SourceTypeDocument SourceTypeValue = "document"

	// SourceTypeWeb indicates a web URL source.
	SourceTypeWeb SourceTypeValue = "web"
)
