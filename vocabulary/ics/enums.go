package ics

// SourceType represents information source categories per ICD 206.
type SourceType string

const (
	// SourceTypePAI is Publicly Available Information - information that has
	// been published or broadcast for public consumption, is available on
	// request to the public, or is accessible to the public online or otherwise.
	SourceTypePAI SourceType = "PAI"

	// SourceTypeCAI is Commercially Available Information - information or data
	// that is made available for purchase by the public at large and is not
	// limited or restricted to certain entities.
	SourceTypeCAI SourceType = "CAI"

	// SourceTypeOSINT is Open Source Intelligence - intelligence produced from
	// publicly available information that is collected, exploited, and
	// disseminated in a timely manner to an appropriate audience for the
	// purpose of addressing a specific intelligence requirement.
	SourceTypeOSINT SourceType = "OSINT"

	// SourceTypeClassified indicates the source material itself is classified
	// and requires appropriate handling per classification level.
	SourceTypeClassified SourceType = "classified"
)

// Classification represents security classification levels.
type Classification string

const (
	// ClassificationUnclassified is information that has been determined not
	// to require protection against unauthorized disclosure.
	ClassificationUnclassified Classification = "unclassified"

	// ClassificationCUI is Controlled Unclassified Information - information
	// that requires safeguarding or dissemination controls pursuant to law,
	// regulation, or government-wide policy but is not classified.
	ClassificationCUI Classification = "cui"

	// ClassificationConfidential is information that reasonably could be
	// expected to cause damage to national security if disclosed without
	// authorization.
	ClassificationConfidential Classification = "confidential"

	// ClassificationSecret is information that reasonably could be expected
	// to cause serious damage to national security if disclosed without
	// authorization.
	ClassificationSecret Classification = "secret"

	// ClassificationTopSecret is information that reasonably could be expected
	// to cause exceptionally grave damage to national security if disclosed
	// without authorization.
	ClassificationTopSecret Classification = "top_secret"
)

// Origin represents how information was produced.
type Origin string

const (
	// OriginHuman indicates information produced directly by a human analyst
	// or author.
	OriginHuman Origin = "human"

	// OriginAIModel indicates information generated or significantly processed
	// by an AI model or automated system.
	OriginAIModel Origin = "ai_model"

	// OriginExternal indicates information retrieved from an external source
	// without significant modification.
	OriginExternal Origin = "external"

	// OriginComposite indicates information synthesized from multiple sources
	// or production methods.
	OriginComposite Origin = "composite"
)

// Reliability represents the IC standard reliability rating (A-F scale)
// for evaluating source reliability.
type Reliability string

const (
	// ReliabilityA indicates a completely reliable source - no doubt of
	// authenticity, trustworthiness, or competency; has a history of complete
	// reliability.
	ReliabilityA Reliability = "A"

	// ReliabilityB indicates a usually reliable source - minor doubt about
	// authenticity, trustworthiness, or competency; has a history of valid
	// information most of the time.
	ReliabilityB Reliability = "B"

	// ReliabilityC indicates a fairly reliable source - doubt of authenticity,
	// trustworthiness, or competency but has provided valid information in
	// the past.
	ReliabilityC Reliability = "C"

	// ReliabilityD indicates a not usually reliable source - significant doubt
	// about authenticity, trustworthiness, or competency but has provided
	// valid information in the past.
	ReliabilityD Reliability = "D"

	// ReliabilityE indicates an unreliable source - lacking in authenticity,
	// trustworthiness, and competency; history of invalid information.
	ReliabilityE Reliability = "E"

	// ReliabilityF indicates the source reliability cannot be judged -
	// no basis exists for evaluating the reliability of the source.
	ReliabilityF Reliability = "F"
)
