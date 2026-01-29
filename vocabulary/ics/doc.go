// Package ics provides ICS 206-01 Source Classification vocabulary predicates.
//
// This vocabulary implements the Intelligence Community Standard (ICS) 206-01
// "Citation and Reference for Publicly Available Information, Commercially
// Available Information, and Open Source Intelligence" per ICD 206 "Sourcing
// Requirements for Disseminated Analytic Products."
//
// # Semstreams Integration
//
// This package follows semstreams vocabulary patterns:
//   - Predicates use three-level dotted notation (domain.category.property)
//   - Predicates are registered in init() using vocabulary.Register()
//   - IRI mappings use vocabulary.WithIRI() for RDF export compatibility
//   - Metadata includes description, data type, and range where applicable
//
// # Source Types (ICD 206)
//
// The package defines source types per ICD 206:
//   - PAI: Publicly Available Information
//   - CAI: Commercially Available Information
//   - OSINT: Open Source Intelligence
//   - classified: Classified source material
//
// # Classification Levels
//
// Standard IC classification levels are supported:
//   - unclassified
//   - cui (Controlled Unclassified Information)
//   - confidential
//   - secret
//   - top_secret
//
// # Reliability Ratings
//
// IC standard reliability ratings (A-F):
//   - A: Completely reliable
//   - B: Usually reliable
//   - C: Fairly reliable
//   - D: Not usually reliable
//   - E: Unreliable
//   - F: Cannot be judged
//
// # Usage
//
// Import the package to register predicates, then use predicate constants:
//
//	import (
//	    "github.com/c360/semspec/vocabulary/ics"
//	    "github.com/c360/semstreams/message"
//	)
//
//	func (p *Processor) buildTriples(entityID string, data Data) []message.Triple {
//	    return []message.Triple{
//	        {Subject: entityID, Predicate: ics.PredicateSourceType, Object: string(ics.SourceTypePAI)},
//	        {Subject: entityID, Predicate: ics.PredicateOrigin, Object: string(ics.OriginExternal)},
//	        {Subject: entityID, Predicate: ics.PredicateConfidence, Object: 85},
//	        {Subject: entityID, Predicate: ics.CitationURL, Object: data.SourceURL},
//	        {Subject: entityID, Predicate: ics.CitationAccessedAt, Object: time.Now().Format(time.RFC3339)},
//	    }
//	}
//
// # IRI Mappings
//
// The package registers IRI mappings to PROV-O for RDF export compatibility:
//   - PredicateValidator → prov:wasAttributedTo
//   - PredicateValidatedAt → prov:endedAtTime
//   - CitationReference → prov:hadPrimarySource
//   - CitationAccessedAt → prov:generatedAtTime
//
// IC-specific predicates use the IC namespace: https://ic.gov/ontology/
package ics
