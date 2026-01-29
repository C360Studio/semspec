package ics

// ICNamespace is the base IRI for IC-specific predicates that do not have
// standard ontology mappings.
const ICNamespace = "https://ic.gov/ontology/"

// PROV-O IRI constants not yet in semstreams vocabulary.
// These extend the PROV-O coverage for ICS 206-01 compliance.
const (
	// ProvEndedAtTime indicates when an activity ended.
	ProvEndedAtTime = "http://www.w3.org/ns/prov#endedAtTime"

	// ProvGeneratedAtTime indicates when an entity was generated.
	ProvGeneratedAtTime = "http://www.w3.org/ns/prov#generatedAtTime"
)
