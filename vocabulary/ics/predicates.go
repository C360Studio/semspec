package ics

import "github.com/c360studio/semstreams/vocabulary"

// ICS 206-01 Source Classification predicates.
//
// These predicates enable source classification per ICD 206 "Sourcing
// Requirements for Disseminated Analytic Products" and ICS 206-01
// citation standards.
const (
	// Source type per ICD 206: PAI, CAI, OSINT, or classified.
	PredicateSourceType = "source.ics.type"

	// Security classification level: unclassified, cui, confidential, secret, top_secret.
	PredicateClassification = "source.ics.classification"

	// How information was produced: human, ai_model, external, composite.
	PredicateOrigin = "source.ics.origin"

	// Confidence score (0-100).
	PredicateConfidence = "source.ics.confidence"

	// IC reliability rating (A-F).
	PredicateReliability = "source.ics.reliability"

	// Classification rationale text.
	PredicateJustification = "source.ics.justification"
)

// Validation provenance predicates track who validated source classifications
// and when.
const (
	// Validating agent entity ID.
	PredicateValidator = "source.ics.validator"

	// RFC3339 timestamp of validation.
	PredicateValidatedAt = "source.ics.validated_at"
)

// Citation tracking predicates per ICS 206-01 citation requirements.
const (
	// Source entity ID reference.
	CitationReference = "source.citation.reference"

	// Formatted citation text per ICS 206-01 format.
	CitationText = "source.citation.text"

	// Original URL for PAI/OSINT sources.
	CitationURL = "source.citation.url"

	// Access date for PAI sources (RFC3339 timestamp).
	CitationAccessedAt = "source.citation.accessed"

	// Archived copy reference (e.g., archive.org URL or internal archive ID).
	CitationArchived = "source.citation.archived"

	// Flag indicating this is an authoritative source.
	CitationAuthority = "source.citation.authority"
)

func init() {
	// Register source classification predicates
	vocabulary.Register(PredicateSourceType,
		vocabulary.WithDescription("Information source category per ICD 206"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ICNamespace+"sourceType"))

	vocabulary.Register(PredicateClassification,
		vocabulary.WithDescription("Security classification level"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ICNamespace+"classification"))

	vocabulary.Register(PredicateOrigin,
		vocabulary.WithDescription("How information was produced"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ICNamespace+"origin"))

	vocabulary.Register(PredicateConfidence,
		vocabulary.WithDescription("Confidence score"),
		vocabulary.WithDataType("int"),
		vocabulary.WithRange("0-100"))

	vocabulary.Register(PredicateReliability,
		vocabulary.WithDescription("IC reliability rating (A-F)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ICNamespace+"reliability"))

	vocabulary.Register(PredicateJustification,
		vocabulary.WithDescription("Classification rationale text"),
		vocabulary.WithDataType("string"))

	// Register validation provenance predicates
	vocabulary.Register(PredicateValidator,
		vocabulary.WithDescription("Agent who validated the classification"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(PredicateValidatedAt,
		vocabulary.WithDescription("When validation occurred (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(ProvEndedAtTime))

	// Register citation tracking predicates
	vocabulary.Register(CitationReference,
		vocabulary.WithDescription("Source entity reference"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvHadPrimarySource))

	vocabulary.Register(CitationText,
		vocabulary.WithDescription("Formatted citation per ICS 206-01"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(CitationURL,
		vocabulary.WithDescription("Original URL for PAI/OSINT sources"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(CitationAccessedAt,
		vocabulary.WithDescription("Access date for PAI sources"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(ProvGeneratedAtTime))

	vocabulary.Register(CitationArchived,
		vocabulary.WithDescription("Archived copy reference"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(CitationAuthority,
		vocabulary.WithDescription("Authoritative source flag"),
		vocabulary.WithDataType("bool"))
}
