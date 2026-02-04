// Package semspec provides domain vocabulary predicates for the Semspec system.
//
// This vocabulary implements predicates for software development artifacts,
// agent activities, and their relationships. The vocabulary is designed for:
//   - Internal efficiency: Clean dotted notation for NATS wildcard queries
//   - External interoperability: Full alignment with BFO, CCO, and PROV-O standards
//   - Government compliance: Suitable for DoD/IC ontology requirements
//
// # Semstreams Integration
//
// This package follows semstreams vocabulary patterns:
//   - Predicates use three-level dotted notation (domain.category.property)
//   - Predicates are registered in init() using vocabulary.Register()
//   - IRI mappings use vocabulary.WithIRI() for RDF export compatibility
//   - Metadata includes description, data type, and range where applicable
//
// # Domain Vocabularies
//
// The package consolidates predicates from multiple domains:
//   - Proposal: Development proposal lifecycle (semspec.proposal.*)
//   - Spec: Specification artifacts (semspec.spec.*)
//   - Task: Work item tracking (semspec.task.*)
//   - Loop: Agent execution loops (agent.loop.*)
//   - Activity: Individual agent actions (agent.activity.*)
//   - Result: Execution results (agent.result.*)
//   - Code: Source code artifacts (code.artifact.*, code.structure.*, code.dependency.*)
//   - Constitution: Project rules (constitution.*)
//
// # Ontology Alignment
//
// Entity types map to standard ontology classes:
//
//	Entity Type → BFO Class                        → CCO Class
//	Proposal    → GenericallyDependentContinuant   → InformationContentEntity
//	Spec        → GenericallyDependentContinuant   → DirectiveInformationContentEntity
//	Task        → GenericallyDependentContinuant   → PlanSpecification
//	CodeFile    → GenericallyDependentContinuant   → SoftwareCode
//	Loop        → Process                          → ActOfArtifactProcessing
//	ModelCall   → Process                          → ActOfCommunication
//	User        → IndependentContinuant            → Person
//	AIModel     → IndependentContinuant            → IntelligentSoftwareAgent
//
// # Usage
//
// Import the package to register predicates, then use predicate constants:
//
//	import (
//	    "github.com/c360studio/semspec/vocabulary/semspec"
//	    "github.com/c360studio/semstreams/message"
//	)
//
//	func (p *Processor) buildTriples(entityID string) []message.Triple {
//	    return []message.Triple{
//	        {Subject: entityID, Predicate: semspec.ProposalStatus, Object: string(semspec.StatusApproved)},
//	        {Subject: entityID, Predicate: semspec.ProposalPriority, Object: string(semspec.PriorityHigh)},
//	        {Subject: entityID, Predicate: semspec.ProposalAuthor, Object: userEntityID},
//	    }
//	}
//
// # RDF Export
//
// The mappings.go file provides BFO/CCO/IRI mappings for RDF export:
//
//	import "github.com/c360studio/semspec/vocabulary/semspec"
//
//	// Get BFO class for entity type
//	bfoClass := semspec.BFOClassMap["proposal"]  // → bfo.GenericallyDependentContinuant
//
//	// Get CCO class for entity type
//	ccoClass := semspec.CCOClassMap["proposal"]  // → cco.InformationContentEntity
//
//	// Get standard IRI for predicate
//	iri := semspec.PredicateIRIMap[semspec.ProposalAuthor]  // → prov:wasAttributedTo
package semspec
