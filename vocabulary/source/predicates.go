package source

import "github.com/c360studio/semstreams/vocabulary"

// Document metadata predicates for ingested documents.
// These predicates track document metadata extracted during ingestion.
const (
	// DocType identifies the source as a document.
	// Values: "document"
	DocType = "source.doc.type"

	// DocCategory classifies the document purpose.
	// Values: sop, spec, datasheet, reference, api
	DocCategory = "source.doc.category"

	// DocAppliesTo specifies file patterns this document applies to.
	// Format: glob patterns like "*.go", "auth/*", "**/*.ts"
	// For SOPs, this determines which files trigger SOP inclusion in reviewer context.
	DocAppliesTo = "source.doc.applies_to"

	// DocSeverity indicates violation severity for SOPs.
	// Values: error (blocks approval), warning (reviewer discretion), info (no enforcement)
	DocSeverity = "source.doc.severity"

	// DocSummary is a short LLM-extracted summary.
	// Used in context assembly when full document doesn't fit in budget.
	DocSummary = "source.doc.summary"

	// DocRequirements are extracted key rules/requirements.
	// Array of strings representing must-check items for reviewers.
	DocRequirements = "source.doc.requirements"

	// DocContent is the chunk text content.
	// Only present on chunk entities, not parent entities.
	DocContent = "source.doc.content"

	// DocSection is the section or heading name.
	// Identifies which part of the document this chunk represents.
	DocSection = "source.doc.section"

	// DocChunkIndex is the chunk sequence number (1-indexed).
	DocChunkIndex = "source.doc.chunk_index"

	// DocChunkCount is the total number of chunks in the parent document.
	DocChunkCount = "source.doc.chunk_count"

	// DocMimeType is the document MIME type.
	// Values: text/markdown, application/pdf, text/plain, etc.
	DocMimeType = "source.doc.mime_type"

	// DocFilePath is the original file path in .semspec/sources/docs/.
	DocFilePath = "source.doc.file_path"

	// DocFileHash is the content hash for staleness detection.
	DocFileHash = "source.doc.file_hash"
)

// Repository source predicates for external code sources.
const (
	// RepoType identifies the source as a repository.
	// Values: "repository"
	RepoType = "source.repo.type"

	// RepoURL is the git clone URL.
	RepoURL = "source.repo.url"

	// RepoBranch is the branch name to track.
	RepoBranch = "source.repo.branch"

	// RepoStatus is the repository indexing status.
	// Values: pending, indexing, ready, error, stale
	RepoStatus = "source.repo.status"

	// RepoLanguages are the programming languages detected.
	// Array of strings like ["go", "typescript", "python"].
	RepoLanguages = "source.repo.languages"

	// RepoEntityCount is the number of entities indexed from this repo.
	RepoEntityCount = "source.repo.entity_count"

	// RepoLastIndexed is the timestamp of last successful indexing (RFC3339).
	RepoLastIndexed = "source.repo.last_indexed"

	// RepoAutoPull indicates whether to auto-pull for updates.
	RepoAutoPull = "source.repo.auto_pull"

	// RepoPullInterval is the auto-pull interval (duration string like "1h").
	RepoPullInterval = "source.repo.pull_interval"

	// RepoLastCommit is the SHA of the last indexed commit.
	RepoLastCommit = "source.repo.last_commit"

	// RepoError is the error message if indexing failed.
	RepoError = "source.repo.error"
)

// Generic source predicates applicable to both documents and repositories.
const (
	// SourceType is the source type discriminator.
	// Values: "repository", "document"
	SourceType = "source.type"

	// SourceName is the display name for the source.
	SourceName = "source.name"

	// SourceStatus is the overall source status.
	// Values: pending, indexing, ready, error, stale
	SourceStatus = "source.status"

	// SourceAddedBy is the user/agent who added this source.
	SourceAddedBy = "source.added_by"

	// SourceAddedAt is the timestamp when the source was added (RFC3339).
	SourceAddedAt = "source.added_at"

	// SourceError is the error message if source processing failed.
	SourceError = "source.error"
)

func init() {
	// Register document metadata predicates
	vocabulary.Register(DocType,
		vocabulary.WithDescription("Source type identifier (document)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"docType"))

	vocabulary.Register(DocCategory,
		vocabulary.WithDescription("Document classification: sop, spec, datasheet, reference, api"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(DcType))

	vocabulary.Register(DocAppliesTo,
		vocabulary.WithDescription("File patterns this document applies to (glob patterns)"),
		vocabulary.WithDataType("array"),
		vocabulary.WithIRI(Namespace+"appliesTo"))

	vocabulary.Register(DocSeverity,
		vocabulary.WithDescription("Violation severity: error, warning, info"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"severity"))

	vocabulary.Register(DocSummary,
		vocabulary.WithDescription("Short extracted summary for context assembly"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(DcAbstract))

	vocabulary.Register(DocRequirements,
		vocabulary.WithDescription("Extracted key requirements for review validation"),
		vocabulary.WithDataType("array"),
		vocabulary.WithIRI(Namespace+"requirements"))

	vocabulary.Register(DocContent,
		vocabulary.WithDescription("Chunk text content"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"content"))

	vocabulary.Register(DocSection,
		vocabulary.WithDescription("Section or heading name for chunk"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"section"))

	vocabulary.Register(DocChunkIndex,
		vocabulary.WithDescription("Chunk sequence number (1-indexed)"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"chunkIndex"))

	vocabulary.Register(DocChunkCount,
		vocabulary.WithDescription("Total chunks in parent document"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"chunkCount"))

	vocabulary.Register(DocMimeType,
		vocabulary.WithDescription("Document MIME type"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(DcFormat))

	vocabulary.Register(DocFilePath,
		vocabulary.WithDescription("Original file path in sources directory"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"filePath"))

	vocabulary.Register(DocFileHash,
		vocabulary.WithDescription("Content hash for staleness detection"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"fileHash"))

	// Register repository source predicates
	vocabulary.Register(RepoType,
		vocabulary.WithDescription("Source type identifier (repository)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"repoType"))

	vocabulary.Register(RepoURL,
		vocabulary.WithDescription("Git clone URL"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"repoURL"))

	vocabulary.Register(RepoBranch,
		vocabulary.WithDescription("Branch name to track"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"branch"))

	vocabulary.Register(RepoStatus,
		vocabulary.WithDescription("Repository indexing status"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"repoStatus"))

	vocabulary.Register(RepoLanguages,
		vocabulary.WithDescription("Programming languages detected"),
		vocabulary.WithDataType("array"),
		vocabulary.WithIRI(Namespace+"languages"))

	vocabulary.Register(RepoEntityCount,
		vocabulary.WithDescription("Number of entities indexed"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"entityCount"))

	vocabulary.Register(RepoLastIndexed,
		vocabulary.WithDescription("Last successful indexing timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(Namespace+"lastIndexed"))

	vocabulary.Register(RepoAutoPull,
		vocabulary.WithDescription("Whether to auto-pull for updates"),
		vocabulary.WithDataType("bool"),
		vocabulary.WithIRI(Namespace+"autoPull"))

	vocabulary.Register(RepoPullInterval,
		vocabulary.WithDescription("Auto-pull interval (duration string)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"pullInterval"))

	vocabulary.Register(RepoLastCommit,
		vocabulary.WithDescription("SHA of last indexed commit"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"lastCommit"))

	vocabulary.Register(RepoError,
		vocabulary.WithDescription("Error message if indexing failed"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"repoError"))

	// Register generic source predicates
	vocabulary.Register(SourceType,
		vocabulary.WithDescription("Source type discriminator: repository or document"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"sourceType"))

	vocabulary.Register(SourceName,
		vocabulary.WithDescription("Display name for the source"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.DcTitle))

	vocabulary.Register(SourceStatus,
		vocabulary.WithDescription("Overall source status"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"status"))

	vocabulary.Register(SourceAddedBy,
		vocabulary.WithDescription("User/agent who added this source"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(SourceAddedAt,
		vocabulary.WithDescription("Timestamp when source was added (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))

	vocabulary.Register(SourceError,
		vocabulary.WithDescription("Error message if source processing failed"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"error"))
}
