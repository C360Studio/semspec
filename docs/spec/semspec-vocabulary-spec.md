# Semspec Vocabulary Specification

**Version**: 1.0.0  
**Status**: Draft  
**Last Updated**: 2025-01-28

---

## Table of Contents

1. [Overview](#1-overview)
2. [Ontology Architecture](#2-ontology-architecture)
3. [BFO Alignment](#3-bfo-alignment)
4. [CCO Alignment](#4-cco-alignment)
5. [PROV-O Integration](#5-prov-o-integration)
6. [Domain Vocabularies](#6-domain-vocabularies)
7. [Predicate Reference](#7-predicate-reference)
8. [IRI Registry](#8-iri-registry)
9. [Entity ID Patterns](#9-entity-id-patterns)
10. [RDF Export](#10-rdf-export)
11. [Validation & Compliance](#11-validation--compliance)
12. [Implementation](#12-implementation)

---

## 1. Overview

### 1.1 Purpose

This specification defines the vocabulary used by Semspec for representing software development artifacts, agent activities, and their relationships in a knowledge graph. The vocabulary is designed for:

- **Internal efficiency**: Clean dotted notation for NATS wildcard queries
- **External interoperability**: Full alignment with BFO, CCO, and PROV-O standards
- **Government compliance**: Suitable for DoD/IC ontology requirements

### 1.2 Design Philosophy

Following SemStreams' "Pragmatic Semantic Web" approach (ADR-001):

| Principle | Implementation |
|-----------|----------------|
| Internal simplicity | Three-part dotted predicates: `domain.category.property` |
| External interoperability | IRI mappings to BFO, CCO, PROV-O, Dublin Core |
| Standards at boundaries | RDF/OWL export with full ontology alignment |
| No runtime complexity | IRI mappings are documentation + export, not runtime overhead |

### 1.3 Standards Compliance

| Standard | Version | Usage |
|----------|---------|-------|
| BFO (Basic Formal Ontology) | ISO/IEC 21838-2:2021 | Upper ontology classification |
| CCO (Common Core Ontologies) | 2.0 | Mid-level domain patterns |
| PROV-O | W3C Recommendation | Provenance relationships |
| Dublin Core | DCMI Terms | Basic metadata |
| SKOS | W3C Recommendation | Concept relationships |
| RDF 1.1 | W3C Recommendation | Export format |
| OWL 2 | W3C Recommendation | Ontology language |

---

## 2. Ontology Architecture

### 2.1 Layered Stack

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  LAYER 4: SEMSPEC DOMAIN VOCABULARY                                         │
│  ─────────────────────────────────────                                      │
│  semspec.proposal.*, semspec.spec.*, semspec.task.*                        │
│  code.artifact.*, agent.loop.*, agent.activity.*                           │
│  constitution.rule.*                                                        │
│                                                                              │
│  Purpose: Domain-specific predicates for software development              │
├─────────────────────────────────────────────────────────────────────────────┤
│  LAYER 3: STANDARD VOCABULARIES                                             │
│  ───────────────────────────────                                            │
│  prov.* (PROV-O)     - Provenance tracking                                 │
│  dc.* (Dublin Core)  - Basic metadata                                      │
│  skos.* (SKOS)       - Concept relationships                               │
│                                                                              │
│  Purpose: Widely-adopted predicates for common patterns                    │
├─────────────────────────────────────────────────────────────────────────────┤
│  LAYER 2: CCO (Common Core Ontologies)                                      │
│  ─────────────────────────────────────                                      │
│  Information Entity Ontology (IEO)                                         │
│  Agent Ontology (AgentO)                                                   │
│  Event Ontology (EventO)                                                   │
│  Artifact Ontology (ArtO)                                                  │
│  Action Ontology (ActO)                                                    │
│                                                                              │
│  Purpose: Mid-level classes for government/defense interoperability        │
├─────────────────────────────────────────────────────────────────────────────┤
│  LAYER 1: BFO (Basic Formal Ontology)                                       │
│  ─────────────────────────────────────                                      │
│  Continuant: Independent, Dependent, Generically Dependent                 │
│  Occurrent: Process, Process Boundary, Temporal Region                     │
│                                                                              │
│  Purpose: ISO-standard upper ontology foundation                           │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Internal vs External Representation

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│  INTERNAL (SemStreams/NATS)              EXTERNAL (RDF/OWL Export)          │
│  ─────────────────────────               ────────────────────────           │
│                                                                              │
│  Entity ID:                              IRI:                               │
│  semspec.dev.proposal.core.auth-refresh  https://semspec.dev/entity/        │
│                                            proposal/core/auth-refresh       │
│                                                                              │
│  Predicate:                              Predicate IRI:                     │
│  prov.derivation.source                  http://www.w3.org/ns/prov#         │
│                                            wasDerivedFrom                   │
│                                                                              │
│  Type assertion:                         Type IRIs:                         │
│  entity.type.class = "proposal"          rdf:type bfo:GDC                   │
│                                          rdf:type cco:ICE                   │
│                                          rdf:type semspec:Proposal          │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. BFO Alignment

### 3.1 BFO Overview

BFO (Basic Formal Ontology) is an ISO standard (ISO/IEC 21838-2:2021) upper ontology used extensively in government, defense, and biomedical domains.

### 3.2 BFO Class Hierarchy (Relevant Subset)

```
bfo:Entity
├── bfo:Continuant (BFO_0000002) - Entities that persist through time
│   ├── bfo:IndependentContinuant (BFO_0000004)
│   │   ├── bfo:MaterialEntity (BFO_0000040) - Physical objects
│   │   └── bfo:ImmaterialEntity (BFO_0000141) - Boundaries, sites
│   │
│   ├── bfo:SpecificallyDependentContinuant (BFO_0000020)
│   │   ├── bfo:Quality (BFO_0000019) - Measurable properties
│   │   └── bfo:RealizableEntity (BFO_0000017)
│   │       ├── bfo:Role (BFO_0000023) - Context-dependent function
│   │       └── bfo:Disposition (BFO_0000016) - Capability
│   │
│   └── bfo:GenericallyDependentContinuant (BFO_0000031) ◄── INFORMATION
│       └── (pattern that can be copied: documents, code, specs)
│
└── bfo:Occurrent (BFO_0000003) - Entities that happen in time
    ├── bfo:Process (BFO_0000015) ◄── ACTIVITIES
    │   └── (events, actions, computations)
    ├── bfo:ProcessBoundary (BFO_0000035) - Instants
    └── bfo:TemporalRegion (BFO_0000008)
```

### 3.3 Semspec → BFO Mapping

| Semspec Entity Type | BFO Class | BFO ID | Rationale |
|---------------------|-----------|--------|-----------|
| Proposal | GenericallyDependentContinuant | BFO_0000031 | Information content that can be copied/versioned |
| Spec | GenericallyDependentContinuant | BFO_0000031 | Prescriptive information content |
| Task | GenericallyDependentContinuant | BFO_0000031 | Planned action specification |
| Code File | GenericallyDependentContinuant | BFO_0000031 | Information concretized in file |
| Function/Struct | GenericallyDependentContinuant | BFO_0000031 | Information pattern within file |
| Loop | Process | BFO_0000015 | Computational process with temporal extent |
| Tool Call | Process | BFO_0000015 | Action with start/end times |
| Model Call | Process | BFO_0000015 | Communication/computation process |
| User | IndependentContinuant | BFO_0000004 | Person (bearer of roles) |
| AI Model | IndependentContinuant | BFO_0000004 | Software system (bearer of roles) |
| Role (planner/implementer) | Role | BFO_0000023 | Realized in processes |
| Token Count | Quality | BFO_0000019 | Measurable property of process |
| Duration | TemporalRegion | BFO_0000008 | Time extent |

### 3.4 BFO Relations Used

| BFO Relation | ID | Usage in Semspec |
|--------------|-----|------------------|
| `participates_in` | BFO_0000056 | Agent participates in Loop |
| `has_participant` | BFO_0000057 | Loop has participant Agent |
| `realizes` | BFO_0000055 | Process realizes Role |
| `bearer_of` | BFO_0000053 | Agent bearer of Role |
| `has_part` | BFO_0000051 | File has part Function |
| `part_of` | BFO_0000050 | Function part of File |
| `preceded_by` | BFO_0000062 | ToolCall preceded by ModelCall |
| `precedes` | BFO_0000063 | ModelCall precedes ToolCall |

### 3.5 BFO IRI Constants

```go
// vocabulary/bfo/bfo.go
package bfo

const (
    Namespace = "http://purl.obolibrary.org/obo/"
    
    // Top-level
    Entity                         = Namespace + "BFO_0000001"
    Continuant                     = Namespace + "BFO_0000002"
    Occurrent                      = Namespace + "BFO_0000003"
    
    // Continuants
    IndependentContinuant          = Namespace + "BFO_0000004"
    SpecificallyDependentContinuant = Namespace + "BFO_0000020"
    GenericallyDependentContinuant = Namespace + "BFO_0000031"
    MaterialEntity                 = Namespace + "BFO_0000040"
    ImmaterialEntity               = Namespace + "BFO_0000141"
    Quality                        = Namespace + "BFO_0000019"
    RealizableEntity               = Namespace + "BFO_0000017"
    Role                           = Namespace + "BFO_0000023"
    Disposition                    = Namespace + "BFO_0000016"
    
    // Occurrents
    Process                        = Namespace + "BFO_0000015"
    ProcessBoundary                = Namespace + "BFO_0000035"
    TemporalRegion                 = Namespace + "BFO_0000008"
    
    // Relations
    ParticipatesIn                 = Namespace + "BFO_0000056"
    HasParticipant                 = Namespace + "BFO_0000057"
    Realizes                       = Namespace + "BFO_0000055"
    BearerOf                       = Namespace + "BFO_0000053"
    HasPart                        = Namespace + "BFO_0000051"
    PartOf                         = Namespace + "BFO_0000050"
    PrecededBy                     = Namespace + "BFO_0000062"
    Precedes                       = Namespace + "BFO_0000063"
    OccursIn                       = Namespace + "BFO_0000066"
    ExistsAt                       = Namespace + "BFO_0000108"
)
```

---

## 4. CCO Alignment

### 4.1 CCO Overview

The Common Core Ontologies (CCO) are a suite of mid-level ontologies built on BFO, widely used in DoD, IC, and government applications. They provide domain-neutral patterns for common concepts.

### 4.2 Relevant CCO Modules

| Module | Abbreviation | Domain | Semspec Usage |
|--------|--------------|--------|---------------|
| Information Entity Ontology | IEO | Information content | Specs, proposals, code |
| Agent Ontology | AgentO | Agents and roles | Users, AI models |
| Event Ontology | EventO | Events and processes | Loops, tool calls |
| Artifact Ontology | ArtO | Designed objects | Software artifacts |
| Action Ontology | ActO | Intentional actions | User commands, approvals |
| Quality Ontology | QualO | Measurable properties | Metrics |
| Units of Measure Ontology | UoMO | Measurements | Durations, counts |

### 4.3 CCO Information Entity Hierarchy

```
cco:InformationContentEntity (subclass of bfo:GDC)
├── cco:DescriptiveInformationContentEntity
│   ├── cco:DesignativeICE (names, identifiers)
│   └── cco:RepresentationalICE (descriptions)
│
├── cco:DirectiveInformationContentEntity ◄── SPECS, TASKS
│   ├── cco:PlanSpecification
│   ├── cco:Requirement
│   └── cco:Standard
│
├── cco:InformationBearingArtifact
│   └── cco:Document
│
└── cco:SoftwareCode ◄── CODE FILES
    └── cco:SoftwareModule
```

### 4.4 CCO Agent Hierarchy

```
cco:Agent (subclass of bfo:IndependentContinuant)
├── cco:Person ◄── USERS
│
├── cco:Organization
│
└── cco:SoftwareAgent ◄── AI MODELS, SEMSPEC
    └── cco:IntelligentSoftwareAgent
```

### 4.5 CCO Action/Event Hierarchy

```
cco:Act (subclass of bfo:Process)
├── cco:ActOfCommunication ◄── MODEL CALLS
│   ├── cco:ActOfInforming
│   └── cco:ActOfRequesting
│
├── cco:ActOfArtifactProcessing ◄── LOOPS
│   ├── cco:ActOfArtifactCreation
│   ├── cco:ActOfArtifactModification
│   └── cco:ActOfArtifactAssessment
│
├── cco:ActOfApproval ◄── APPROVALS
│
├── cco:ActOfDecisionMaking
│
└── cco:ActOfMeasuring ◄── TOOL CALLS (some)
```

### 4.6 Semspec → CCO Mapping

| Semspec Entity | CCO Class | CCO IRI Fragment | Rationale |
|----------------|-----------|------------------|-----------|
| Proposal | InformationContentEntity | ICE | General information content |
| Spec | DirectiveInformationContentEntity | DirectiveICE | Prescriptive content |
| Task | PlanSpecification | PlanSpecification | Planned action |
| Code File | SoftwareCode | SoftwareCode | Source code artifact |
| Function | SoftwareModule | SoftwareModule | Code unit |
| Documentation | Document | Document | Information bearing artifact |
| User | Person | Person | Human agent |
| AI Model | IntelligentSoftwareAgent | IntelligentSoftwareAgent | Autonomous software |
| Loop | ActOfArtifactProcessing | ActOfArtifactProcessing | Processing action |
| Tool Call (file ops) | ActOfArtifactModification | ActOfArtifactModification | Modifying artifacts |
| Tool Call (queries) | ActOfMeasuring | ActOfMeasuring | Gathering information |
| Model Call | ActOfCommunication | ActOfCommunication | Information exchange |
| Approval | ActOfApproval | ActOfApproval | Approval action |
| Rejection | ActOfDecisionMaking | ActOfDecisionMaking | Decision action |
| Agent Role | AgentRole | AgentRole | Role in process |

### 4.7 CCO Relations Used

| CCO Relation | Usage |
|--------------|-------|
| `cco:has_input` | Process has input Entity |
| `cco:has_output` | Process has output Entity |
| `cco:has_agent` | Process has agent |
| `cco:agent_in` | Agent participates in Process |
| `cco:is_about` | ICE is about some entity |
| `cco:prescribes` | DirectiveICE prescribes action |
| `cco:designated_by` | Entity has identifier |

### 4.8 CCO IRI Constants

```go
// vocabulary/cco/cco.go
package cco

const (
    Namespace = "http://www.ontologyrepository.com/CommonCoreOntologies/"
    
    // Information Entities (IEO)
    InformationContentEntity           = Namespace + "InformationContentEntity"
    DescriptiveICE                     = Namespace + "DescriptiveInformationContentEntity"
    DirectiveICE                       = Namespace + "DirectiveInformationContentEntity"
    DesignativeICE                     = Namespace + "DesignativeInformationContentEntity"
    RepresentationalICE                = Namespace + "RepresentationalInformationContentEntity"
    PlanSpecification                  = Namespace + "PlanSpecification"
    Requirement                        = Namespace + "Requirement"
    Standard                           = Namespace + "Standard"
    Document                           = Namespace + "Document"
    SoftwareCode                       = Namespace + "SoftwareCode"
    SoftwareModule                     = Namespace + "SoftwareModule"
    
    // Agents (AgentO)
    Agent                              = Namespace + "Agent"
    Person                             = Namespace + "Person"
    Organization                       = Namespace + "Organization"
    SoftwareAgent                      = Namespace + "SoftwareAgent"
    IntelligentSoftwareAgent           = Namespace + "IntelligentSoftwareAgent"
    AgentRole                          = Namespace + "AgentRole"
    
    // Actions/Events (ActO/EventO)
    Act                                = Namespace + "Act"
    ActOfCommunication                 = Namespace + "ActOfCommunication"
    ActOfInforming                     = Namespace + "ActOfInforming"
    ActOfRequesting                    = Namespace + "ActOfRequesting"
    ActOfArtifactProcessing            = Namespace + "ActOfArtifactProcessing"
    ActOfArtifactCreation              = Namespace + "ActOfArtifactCreation"
    ActOfArtifactModification          = Namespace + "ActOfArtifactModification"
    ActOfArtifactAssessment            = Namespace + "ActOfArtifactAssessment"
    ActOfApproval                      = Namespace + "ActOfApproval"
    ActOfDecisionMaking                = Namespace + "ActOfDecisionMaking"
    ActOfMeasuring                     = Namespace + "ActOfMeasuring"
    
    // Relations
    HasInput                           = Namespace + "has_input"
    HasOutput                          = Namespace + "has_output"
    HasAgent                           = Namespace + "has_agent"
    AgentIn                            = Namespace + "agent_in"
    IsAbout                            = Namespace + "is_about"
    Prescribes                         = Namespace + "prescribes"
    DesignatedBy                       = Namespace + "designated_by"
)
```

---

## 5. PROV-O Integration

### 5.1 PROV-O Overview

PROV-O (W3C Provenance Ontology) provides a standard model for provenance - tracking who created what, when, and how. PROV-O has documented alignment with BFO.

### 5.2 PROV-O Core Model

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│  PROV-O CORE                                                                │
│                                                                              │
│                    prov:wasGeneratedBy                                      │
│  prov:Entity ◄─────────────────────────── prov:Activity                    │
│       │                                        │                            │
│       │ prov:wasAttributedTo                   │ prov:wasAssociatedWith    │
│       │                                        │                            │
│       ▼                                        ▼                            │
│  prov:Agent ◄──────────────────────────────────                            │
│                  prov:actedOnBehalfOf                                       │
│                                                                              │
│  KEY RELATIONS:                                                             │
│  • prov:wasGeneratedBy - Entity created by Activity                        │
│  • prov:wasDerivedFrom - Entity derived from Entity                        │
│  • prov:wasAttributedTo - Entity attributed to Agent                       │
│  • prov:used - Activity used Entity                                        │
│  • prov:wasAssociatedWith - Activity associated with Agent                 │
│  • prov:actedOnBehalfOf - Agent delegation                                 │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.3 PROV-O ↔ BFO Alignment

| PROV-O | BFO Equivalent | Notes |
|--------|----------------|-------|
| `prov:Entity` | `bfo:Continuant` | Things that exist |
| `prov:Activity` | `bfo:Process` | Things that happen |
| `prov:Agent` | `bfo:IndependentContinuant` + `bfo:Role` | Bearer of agent role |
| `prov:wasGeneratedBy` | (process output) | `cco:has_output` inverse |
| `prov:used` | (process input) | `cco:has_input` |
| `prov:wasAssociatedWith` | `bfo:has_participant` | Process participation |

### 5.4 PROV-O in Semspec

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│  SEMSPEC PROVENANCE CHAIN                                                   │
│                                                                              │
│  User ──────────────────────────────────────────────────────┐               │
│    │ prov:actedOnBehalfOf                                   │               │
│    ▼                                                        │               │
│  Semspec (Agent) ◄──────────────────────────────────────────┤               │
│    │                                                        │               │
│    │ prov:wasAssociatedWith                                 │               │
│    ▼                                                        │               │
│  Loop (Activity) ───────────────────────────────────────────┤               │
│    │                                                        │               │
│    │ prov:used              prov:wasGeneratedBy            │               │
│    ▼                        │                               │               │
│  Task (Entity) ◄────────────┼───────────────────────────────┤               │
│    │                        │                               │               │
│    │ prov:wasDerivedFrom    │                               │               │
│    ▼                        │                               │               │
│  Spec (Entity) ◄────────────┼───────────────────────────────┤               │
│    │                        │                               │               │
│    │ prov:wasDerivedFrom    │                               │               │
│    ▼                        ▼                               │               │
│  Proposal (Entity) ◄── Code File (Entity)                   │               │
│                             │                               │               │
│                             │ prov:wasAttributedTo          │               │
│                             ▼                               │               │
│                         AI Model (Agent) ◄──────────────────┘               │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.5 PROV-O IRI Constants

```go
// vocabulary/prov/prov.go
package prov

const (
    Namespace = "http://www.w3.org/ns/prov#"
    
    // Classes
    Entity                = Namespace + "Entity"
    Activity              = Namespace + "Activity"
    Agent                 = Namespace + "Agent"
    SoftwareAgent         = Namespace + "SoftwareAgent"
    Person                = Namespace + "Person"
    Organization          = Namespace + "Organization"
    Collection            = Namespace + "Collection"
    Bundle                = Namespace + "Bundle"
    Plan                  = Namespace + "Plan"
    
    // Starting Point Relations
    WasGeneratedBy        = Namespace + "wasGeneratedBy"
    WasDerivedFrom        = Namespace + "wasDerivedFrom"
    WasAttributedTo       = Namespace + "wasAttributedTo"
    Used                  = Namespace + "used"
    WasAssociatedWith     = Namespace + "wasAssociatedWith"
    ActedOnBehalfOf       = Namespace + "actedOnBehalfOf"
    
    // Expanded Relations
    WasInfluencedBy       = Namespace + "wasInfluencedBy"
    WasInformedBy         = Namespace + "wasInformedBy"
    WasStartedBy          = Namespace + "wasStartedBy"
    WasEndedBy            = Namespace + "wasEndedBy"
    WasInvalidatedBy      = Namespace + "wasInvalidatedBy"
    WasRevisionOf         = Namespace + "wasRevisionOf"
    WasQuotedFrom         = Namespace + "wasQuotedFrom"
    HadPrimarySource      = Namespace + "hadPrimarySource"
    HadMember             = Namespace + "hadMember"
    
    // Qualified Relations
    QualifiedGeneration   = Namespace + "qualifiedGeneration"
    QualifiedDerivation   = Namespace + "qualifiedDerivation"
    QualifiedAttribution  = Namespace + "qualifiedAttribution"
    QualifiedUsage        = Namespace + "qualifiedUsage"
    QualifiedAssociation  = Namespace + "qualifiedAssociation"
    QualifiedDelegation   = Namespace + "qualifiedDelegation"
    
    // Time Properties
    GeneratedAtTime       = Namespace + "generatedAtTime"
    StartedAtTime         = Namespace + "startedAtTime"
    EndedAtTime           = Namespace + "endedAtTime"
    InvalidatedAtTime     = Namespace + "invalidatedAtTime"
    
    // Other Properties
    Value                 = Namespace + "value"
    AtLocation            = Namespace + "atLocation"
)
```

---

## 6. Domain Vocabularies

### 6.1 Internal Predicate Format

All predicates use three-part dotted notation: `domain.category.property`

Benefits:
- NATS wildcard queries: `semspec.proposal.*` finds all proposal predicates
- Self-documenting: `agent.loop.status` is immediately understandable
- No collisions: `code.file.path` vs `config.file.path` are distinct

### 6.2 Development Artifacts

#### 6.2.1 Proposals

```go
// Proposal predicates
const (
    ProposalStatus      = "semspec.proposal.status"      // exploring|drafted|approved|implementing|complete
    ProposalPriority    = "semspec.proposal.priority"    // high|medium|low
    ProposalRationale   = "semspec.proposal.rationale"   // why this proposal exists
    ProposalScope       = "semspec.proposal.scope"       // affected areas
    ProposalSpec        = "semspec.proposal.spec"        // → spec entity
    ProposalAuthor      = "semspec.proposal.author"      // → user entity
    ProposalReviewer    = "semspec.proposal.reviewer"    // → user entity
)

// IRI Mappings
var ProposalMappings = map[string]string{
    ProposalStatus:   cco.InformationContentEntity,  // type
    ProposalAuthor:   prov.WasAttributedTo,          // relation
    ProposalSpec:     prov.WasDerivedFrom,           // relation (inverse)
}
```

#### 6.2.2 Specifications

```go
// Spec predicates
const (
    SpecStatus          = "semspec.spec.status"          // draft|in_review|approved|implemented
    SpecVersion         = "semspec.spec.version"         // semver
    SpecProposal        = "semspec.spec.proposal"        // → proposal entity
    SpecTasks           = "semspec.spec.tasks"           // → task entities
    SpecAffects         = "semspec.spec.affects"         // → code entities
    SpecAuthor          = "semspec.spec.author"          // → user/agent entity
    SpecApprovedBy      = "semspec.spec.approved_by"     // → user entity
    SpecApprovedAt      = "semspec.spec.approved_at"     // timestamp
)

// IRI Mappings
var SpecMappings = map[string]string{
    SpecStatus:     cco.DirectiveICE,         // type
    SpecProposal:   prov.WasDerivedFrom,      // relation
    SpecAuthor:     prov.WasAttributedTo,     // relation
    SpecAffects:    cco.IsAbout,              // relation
}
```

#### 6.2.3 Tasks

```go
// Task predicates
const (
    TaskStatus          = "semspec.task.status"          // pending|in_progress|complete|failed
    TaskType            = "semspec.task.type"            // implement|test|document|review
    TaskSpec            = "semspec.task.spec"            // → spec entity
    TaskLoop            = "semspec.task.loop"            // → loop entity
    TaskAssignee        = "semspec.task.assignee"        // → agent entity (role)
    TaskPredecessor     = "semspec.task.predecessor"     // → other task (ordering)
    TaskSuccessor       = "semspec.task.successor"       // → other task (ordering)
    TaskEstimate        = "semspec.task.estimate"        // complexity estimate
    TaskActualEffort    = "semspec.task.actual_effort"   // actual time/iterations
)

// IRI Mappings
var TaskMappings = map[string]string{
    TaskStatus:      cco.PlanSpecification,   // type
    TaskSpec:        prov.WasDerivedFrom,     // relation
    TaskLoop:        prov.WasGeneratedBy,     // relation (inverse)
    TaskPredecessor: bfo.PrecededBy,          // relation
    TaskSuccessor:   bfo.Precedes,            // relation
}
```

### 6.3 Code Artifacts

```go
// Code artifact predicates
const (
    // Identity
    CodePath            = "code.artifact.path"           // file path
    CodeHash            = "code.artifact.hash"           // content hash
    CodeLanguage        = "code.artifact.language"       // go, typescript, etc.
    CodePackage         = "code.artifact.package"        // package name
    
    // Classification
    CodeType            = "code.artifact.type"           // file|function|struct|interface|const|var
    CodeVisibility      = "code.artifact.visibility"     // public|private|internal
    
    // Structure relationships
    CodeContains        = "code.structure.contains"      // file → functions
    CodeBelongsTo       = "code.structure.belongs"       // function → file
    
    // Dependency relationships
    CodeImports         = "code.dependency.imports"      // → other code entity
    CodeExports         = "code.dependency.exports"      // exported symbols
    
    // Semantic relationships
    CodeImplements      = "code.relationship.implements" // → interface entity
    CodeExtends         = "code.relationship.extends"    // → struct entity
    CodeCalls           = "code.relationship.calls"      // → function entity
    CodeReferences      = "code.relationship.references" // → any code entity
    
    // Metrics
    CodeLines           = "code.metric.lines"            // line count
    CodeComplexity      = "code.metric.complexity"       // cyclomatic complexity
)

// IRI Mappings
var CodeMappings = map[string]string{
    CodePath:       cco.SoftwareCode,         // type
    CodeContains:   bfo.HasPart,              // relation
    CodeBelongsTo:  bfo.PartOf,               // relation
    CodeImports:    cco.HasInput,             // relation (conceptually)
}
```

### 6.4 Agent Activities

#### 6.4.1 Loops

```go
// Loop predicates
const (
    // Identity & State
    LoopStatus          = "agent.loop.status"            // executing|paused|awaiting|complete|failed|cancelled
    LoopRole            = "agent.loop.role"              // planner|implementer|reviewer|general
    LoopModel           = "agent.loop.model"             // model identifier
    
    // Iteration tracking
    LoopIterations      = "agent.loop.iterations"        // current iteration
    LoopMaxIterations   = "agent.loop.max_iterations"    // max allowed
    
    // Relationships
    LoopTask            = "agent.loop.task"              // → task entity
    LoopUser            = "agent.loop.user"              // → user who initiated
    LoopAgent           = "agent.loop.agent"             // → AI model agent
    
    // Content
    LoopPrompt          = "agent.loop.prompt"            // initial prompt
    LoopContext         = "agent.loop.context"           // context provided
    
    // Timing
    LoopStartedAt       = "agent.loop.started_at"        // timestamp
    LoopEndedAt         = "agent.loop.ended_at"          // timestamp
    LoopDuration        = "agent.loop.duration"          // milliseconds
)

// IRI Mappings
var LoopMappings = map[string]string{
    LoopStatus:    cco.ActOfArtifactProcessing,  // type
    LoopUser:      prov.WasAssociatedWith,       // relation
    LoopAgent:     prov.WasAssociatedWith,       // relation
    LoopTask:      prov.Used,                    // relation
    LoopStartedAt: prov.StartedAtTime,           // property
    LoopEndedAt:   prov.EndedAtTime,             // property
}
```

#### 6.4.2 Activities (Tool Calls, Model Calls)

```go
// Activity predicates (individual steps within a loop)
const (
    // Classification
    ActivityType        = "agent.activity.type"          // model_call|tool_call
    ActivityTool        = "agent.activity.tool"          // tool name
    ActivityModel       = "agent.activity.model"         // model name
    
    // Relationships
    ActivityLoop        = "agent.activity.loop"          // → parent loop
    ActivityPrecedes    = "agent.activity.precedes"      // → next activity
    ActivityFollows     = "agent.activity.follows"       // → previous activity
    
    // Inputs/Outputs
    ActivityInput       = "agent.activity.input"         // → input entity
    ActivityOutput      = "agent.activity.output"        // → output entity
    ActivityArgs        = "agent.activity.args"          // tool arguments (JSON)
    ActivityResult      = "agent.activity.result"        // result summary
    
    // Metrics
    ActivityDuration    = "agent.activity.duration"      // milliseconds
    ActivityTokensIn    = "agent.activity.tokens_in"     // input tokens
    ActivityTokensOut   = "agent.activity.tokens_out"    // output tokens
    ActivitySuccess     = "agent.activity.success"       // bool
    ActivityError       = "agent.activity.error"         // error message if failed
    
    // Timing
    ActivityStartedAt   = "agent.activity.started_at"    // timestamp
    ActivityEndedAt     = "agent.activity.ended_at"      // timestamp
)

// IRI Mappings (varies by activity type)
var ActivityMappings = map[string]map[string]string{
    "model_call": {
        ActivityType:      cco.ActOfCommunication,
        ActivityLoop:      bfo.PartOf,
        ActivityStartedAt: prov.StartedAtTime,
    },
    "tool_call": {
        ActivityType:      cco.ActOfArtifactModification, // or ActOfMeasuring
        ActivityLoop:      bfo.PartOf,
        ActivityInput:     prov.Used,
        ActivityOutput:    prov.WasGeneratedBy, // inverse
    },
}
```

#### 6.4.3 Results

```go
// Result predicates
const (
    ResultOutcome       = "agent.result.outcome"         // success|failure|partial
    ResultLoop          = "agent.result.loop"            // → loop entity
    ResultSummary       = "agent.result.summary"         // human-readable summary
    ResultArtifacts     = "agent.result.artifacts"       // → created entities
    ResultDiff          = "agent.result.diff"            // unified diff (if applicable)
    
    // Approval tracking
    ResultApproved      = "agent.result.approved"        // bool
    ResultApprovedBy    = "agent.result.approved_by"     // → user entity
    ResultApprovedAt    = "agent.result.approved_at"     // timestamp
    ResultRejectedBy    = "agent.result.rejected_by"     // → user entity
    ResultRejectedAt    = "agent.result.rejected_at"     // timestamp
    ResultRejectionReason = "agent.result.rejection_reason" // reason text
)

// IRI Mappings
var ResultMappings = map[string]string{
    ResultOutcome:    cco.InformationContentEntity,  // type
    ResultLoop:       prov.WasGeneratedBy,           // relation
    ResultArtifacts:  prov.WasGeneratedBy,           // relation (inverse on artifacts)
    ResultApprovedBy: prov.WasAttributedTo,          // relation
}
```

### 6.5 Constitution

```go
// Constitution predicates
const (
    ConstitutionProject  = "constitution.project.name"    // project identifier
    ConstitutionVersion  = "constitution.version.number"  // version
    ConstitutionSection  = "constitution.section.name"    // code_quality|testing|security|architecture
    ConstitutionRule     = "constitution.rule.text"       // rule text
    ConstitutionRuleID   = "constitution.rule.id"         // rule identifier
    ConstitutionEnforced = "constitution.rule.enforced"   // bool - is this rule enforced?
    ConstitutionPriority = "constitution.rule.priority"   // enforcement priority
)

// IRI Mappings
var ConstitutionMappings = map[string]string{
    ConstitutionSection: cco.DirectiveICE,     // type
    ConstitutionRule:    cco.Requirement,      // type
}
```

### 6.6 Standard Metadata

```go
// Dublin Core aligned predicates
const (
    DcTitle             = "dc.terms.title"               // human-readable title
    DcDescription       = "dc.terms.description"         // description
    DcCreator           = "dc.terms.creator"             // creator
    DcCreated           = "dc.terms.created"             // creation timestamp
    DcModified          = "dc.terms.modified"            // modification timestamp
    DcType              = "dc.terms.type"                // type classification
    DcIdentifier        = "dc.terms.identifier"          // external identifier
    DcSource            = "dc.terms.source"              // source reference
    DcFormat            = "dc.terms.format"              // MIME type
    DcLanguage          = "dc.terms.language"            // language code
)

// SKOS aligned predicates
const (
    SkosPrefLabel       = "skos.label.preferred"         // preferred label
    SkosAltLabel        = "skos.label.alternate"         // alternate labels
    SkosBroader         = "skos.semantic.broader"        // parent concept
    SkosNarrower        = "skos.semantic.narrower"       // child concepts
    SkosRelated         = "skos.semantic.related"        // related concepts
    SkosNote            = "skos.documentation.note"      // documentation
    SkosDefinition      = "skos.documentation.definition"// formal definition
)

// PROV-O aligned predicates (dotted notation)
const (
    ProvGeneratedBy     = "prov.generation.activity"     // → activity
    ProvAttributedTo    = "prov.attribution.agent"       // → agent
    ProvDerivedFrom     = "prov.derivation.source"       // → source entity
    ProvUsed            = "prov.usage.entity"            // → input entity
    ProvAssociatedWith  = "prov.association.agent"       // → agent
    ProvActedOnBehalfOf = "prov.delegation.principal"    // → principal agent
    ProvStartedAt       = "prov.time.started"            // timestamp
    ProvEndedAt         = "prov.time.ended"              // timestamp
    ProvGeneratedAt     = "prov.time.generated"          // timestamp
)
```

---

## 7. Predicate Reference

### 7.1 Complete Predicate Table

| Predicate | Domain | Data Type | IRI Mapping | Description |
|-----------|--------|-----------|-------------|-------------|
| `semspec.proposal.status` | proposal | enum | - | Proposal lifecycle state |
| `semspec.proposal.priority` | proposal | enum | - | Priority level |
| `semspec.proposal.spec` | proposal | entity_id | `prov:wasDerivedFrom` (inv) | Related spec |
| `semspec.spec.status` | spec | enum | - | Spec lifecycle state |
| `semspec.spec.version` | spec | semver | - | Version number |
| `semspec.spec.proposal` | spec | entity_id | `prov:wasDerivedFrom` | Source proposal |
| `semspec.spec.affects` | spec | entity_id | `cco:is_about` | Affected code |
| `semspec.task.status` | task | enum | - | Task state |
| `semspec.task.spec` | task | entity_id | `prov:wasDerivedFrom` | Parent spec |
| `semspec.task.loop` | task | entity_id | `prov:wasGeneratedBy` (inv) | Executing loop |
| `semspec.task.predecessor` | task | entity_id | `bfo:preceded_by` | Prior task |
| `code.artifact.path` | code | string | - | File path |
| `code.artifact.type` | code | enum | - | Code element type |
| `code.structure.contains` | code | entity_id | `bfo:has_part` | Contains elements |
| `code.dependency.imports` | code | entity_id | - | Import dependencies |
| `agent.loop.status` | loop | enum | - | Loop state |
| `agent.loop.role` | loop | enum | - | Agent role |
| `agent.loop.task` | loop | entity_id | `prov:used` | Input task |
| `agent.loop.user` | loop | entity_id | `prov:wasAssociatedWith` | Initiating user |
| `agent.activity.type` | activity | enum | - | Activity classification |
| `agent.activity.loop` | activity | entity_id | `bfo:part_of` | Parent loop |
| `agent.activity.duration` | activity | integer | - | Duration in ms |
| `agent.result.outcome` | result | enum | - | Result status |
| `agent.result.approved_by` | result | entity_id | `prov:wasAttributedTo` | Approver |
| `dc.terms.title` | metadata | string | `dc:title` | Title |
| `dc.terms.created` | metadata | datetime | `dc:created` | Creation time |
| `prov.derivation.source` | provenance | entity_id | `prov:wasDerivedFrom` | Source entity |
| `prov.attribution.agent` | provenance | entity_id | `prov:wasAttributedTo` | Attributed agent |
| `prov.generation.activity` | provenance | entity_id | `prov:wasGeneratedBy` | Generating activity |

### 7.2 Enumeration Values

#### Status Enumerations

| Predicate | Valid Values |
|-----------|-------------|
| `semspec.proposal.status` | `exploring`, `drafted`, `approved`, `implementing`, `complete`, `rejected`, `abandoned` |
| `semspec.spec.status` | `draft`, `in_review`, `approved`, `implemented`, `superseded` |
| `semspec.task.status` | `pending`, `in_progress`, `complete`, `failed`, `blocked`, `cancelled` |
| `agent.loop.status` | `executing`, `paused`, `awaiting_approval`, `complete`, `failed`, `cancelled` |
| `agent.result.outcome` | `success`, `failure`, `partial` |

#### Type Enumerations

| Predicate | Valid Values |
|-----------|-------------|
| `semspec.task.type` | `implement`, `test`, `document`, `review`, `refactor` |
| `code.artifact.type` | `file`, `package`, `function`, `method`, `struct`, `interface`, `const`, `var`, `type` |
| `agent.loop.role` | `planner`, `implementer`, `reviewer`, `general` |
| `agent.activity.type` | `model_call`, `tool_call` |

#### Priority Enumerations

| Predicate | Valid Values |
|-----------|-------------|
| `semspec.proposal.priority` | `critical`, `high`, `medium`, `low` |
| `constitution.rule.priority` | `must`, `should`, `may` |

---

## 8. IRI Registry

### 8.1 Namespace Prefixes

```turtle
@prefix rdf:     <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix rdfs:    <http://www.w3.org/2000/01/rdf-schema#> .
@prefix owl:     <http://www.w3.org/2002/07/owl#> .
@prefix xsd:     <http://www.w3.org/2001/XMLSchema#> .
@prefix dc:      <http://purl.org/dc/terms/> .
@prefix skos:    <http://www.w3.org/2004/02/skos/core#> .
@prefix prov:    <http://www.w3.org/ns/prov#> .
@prefix bfo:     <http://purl.obolibrary.org/obo/> .
@prefix cco:     <http://www.ontologyrepository.com/CommonCoreOntologies/> .
@prefix semspec: <https://semspec.dev/ontology/> .
```

### 8.2 Semspec Domain IRI Definitions

```go
// vocabulary/semspec/iris.go
package semspec

const (
    Namespace = "https://semspec.dev/ontology/"
    
    // Classes
    Proposal           = Namespace + "Proposal"
    Specification      = Namespace + "Specification"
    Task               = Namespace + "Task"
    CodeArtifact       = Namespace + "CodeArtifact"
    Loop               = Namespace + "Loop"
    Activity           = Namespace + "Activity"
    ModelCall          = Namespace + "ModelCall"
    ToolCall           = Namespace + "ToolCall"
    Result             = Namespace + "Result"
    Constitution       = Namespace + "Constitution"
    ConstitutionRule   = Namespace + "ConstitutionRule"
    
    // Object Properties
    HasSpec            = Namespace + "hasSpec"
    HasTask            = Namespace + "hasTask"
    HasLoop            = Namespace + "hasLoop"
    HasActivity        = Namespace + "hasActivity"
    HasResult          = Namespace + "hasResult"
    AffectsCode        = Namespace + "affectsCode"
    ExecutedBy         = Namespace + "executedBy"
    
    // Data Properties
    Status             = Namespace + "status"
    Priority           = Namespace + "priority"
    Role               = Namespace + "role"
    Iterations         = Namespace + "iterations"
    MaxIterations      = Namespace + "maxIterations"
    TokensIn           = Namespace + "tokensIn"
    TokensOut          = Namespace + "tokensOut"
    Duration           = Namespace + "duration"
)
```

### 8.3 Complete IRI Mapping Table

| Internal Predicate | Standard IRI | Ontology |
|-------------------|--------------|----------|
| `dc.terms.title` | `http://purl.org/dc/terms/title` | Dublin Core |
| `dc.terms.description` | `http://purl.org/dc/terms/description` | Dublin Core |
| `dc.terms.creator` | `http://purl.org/dc/terms/creator` | Dublin Core |
| `dc.terms.created` | `http://purl.org/dc/terms/created` | Dublin Core |
| `dc.terms.modified` | `http://purl.org/dc/terms/modified` | Dublin Core |
| `dc.terms.type` | `http://purl.org/dc/terms/type` | Dublin Core |
| `skos.label.preferred` | `http://www.w3.org/2004/02/skos/core#prefLabel` | SKOS |
| `skos.label.alternate` | `http://www.w3.org/2004/02/skos/core#altLabel` | SKOS |
| `skos.semantic.broader` | `http://www.w3.org/2004/02/skos/core#broader` | SKOS |
| `skos.semantic.narrower` | `http://www.w3.org/2004/02/skos/core#narrower` | SKOS |
| `skos.semantic.related` | `http://www.w3.org/2004/02/skos/core#related` | SKOS |
| `prov.generation.activity` | `http://www.w3.org/ns/prov#wasGeneratedBy` | PROV-O |
| `prov.attribution.agent` | `http://www.w3.org/ns/prov#wasAttributedTo` | PROV-O |
| `prov.derivation.source` | `http://www.w3.org/ns/prov#wasDerivedFrom` | PROV-O |
| `prov.usage.entity` | `http://www.w3.org/ns/prov#used` | PROV-O |
| `prov.association.agent` | `http://www.w3.org/ns/prov#wasAssociatedWith` | PROV-O |
| `prov.time.started` | `http://www.w3.org/ns/prov#startedAtTime` | PROV-O |
| `prov.time.ended` | `http://www.w3.org/ns/prov#endedAtTime` | PROV-O |
| `code.structure.contains` | `http://purl.obolibrary.org/obo/BFO_0000051` | BFO (has_part) |
| `code.structure.belongs` | `http://purl.obolibrary.org/obo/BFO_0000050` | BFO (part_of) |
| `semspec.task.predecessor` | `http://purl.obolibrary.org/obo/BFO_0000062` | BFO (preceded_by) |
| `semspec.task.successor` | `http://purl.obolibrary.org/obo/BFO_0000063` | BFO (precedes) |

---

## 9. Entity ID Patterns

### 9.1 Six-Part Dotted Notation

Following SemStreams convention, all entity IDs use six-part dotted notation:

```
{org}.{platform}.{system}.{domain}.{type}.{instance}
```

### 9.2 Semspec Entity ID Patterns

| Entity Type | Pattern | Example |
|-------------|---------|---------|
| Proposal | `{org}.semspec.project.proposal.{project}.{id}` | `acme.semspec.project.proposal.api.auth-refresh` |
| Spec | `{org}.semspec.project.spec.{project}.{id}` | `acme.semspec.project.spec.api.auth-refresh-v1` |
| Task | `{org}.semspec.project.task.{project}.{id}` | `acme.semspec.project.task.api.impl-refresh-token` |
| Loop | `{org}.semspec.agent.loop.{project}.{id}` | `acme.semspec.agent.loop.api.loop-abc123` |
| Activity | `{org}.semspec.agent.activity.{loop}.{id}` | `acme.semspec.agent.activity.abc123.act-001` |
| Result | `{org}.semspec.agent.result.{loop}.{id}` | `acme.semspec.agent.result.abc123.res-001` |
| Code File | `{org}.semspec.code.file.{project}.{hash}` | `acme.semspec.code.file.api.a1b2c3d4` |
| Code Function | `{org}.semspec.code.function.{project}.{id}` | `acme.semspec.code.function.api.RefreshToken` |
| User | `{org}.semspec.user.person.{domain}.{id}` | `acme.semspec.user.person.dev.coby` |
| AI Model | `{org}.semspec.agent.model.{provider}.{id}` | `acme.semspec.agent.model.ollama.qwen-32b` |
| Constitution | `{org}.semspec.config.constitution.{project}.{version}` | `acme.semspec.config.constitution.api.v1` |

### 9.3 Entity ID to IRI Conversion

```go
// Convert 6-part entity ID to IRI
func EntityIDToIRI(entityID string) string {
    // semspec.dev.proposal.core.auth-refresh
    // → https://semspec.dev/entity/proposal/core/auth-refresh
    
    parts := strings.Split(entityID, ".")
    if len(parts) < 6 {
        return ""
    }
    
    // Extract meaningful parts for IRI
    // Skip org (parts[0]) and "semspec" (parts[1])
    category := parts[3]  // proposal, spec, task, etc.
    project := parts[4]
    instance := parts[5]
    
    return fmt.Sprintf("https://semspec.dev/entity/%s/%s/%s", category, project, instance)
}
```

---

## 10. RDF Export

### 10.1 Export Capability

Semspec provides RDF/OWL export with full BFO/CCO alignment for interoperability with government and enterprise systems.

### 10.2 Export Formats

| Format | MIME Type | Use Case |
|--------|-----------|----------|
| Turtle | `text/turtle` | Human-readable, compact |
| RDF/XML | `application/rdf+xml` | Legacy tool compatibility |
| JSON-LD | `application/ld+json` | Web integration |
| N-Triples | `application/n-triples` | Bulk processing |
| N-Quads | `application/n-quads` | Named graphs |

### 10.3 Export Profiles

#### 10.3.1 Minimal Profile

Standard predicates only (PROV-O, Dublin Core, SKOS):

```turtle
@prefix prov: <http://www.w3.org/ns/prov#> .
@prefix dc: <http://purl.org/dc/terms/> .
@prefix semspec: <https://semspec.dev/entity/> .

semspec:proposal/api/auth-refresh
    a prov:Entity ;
    dc:title "Auth Token Refresh" ;
    dc:created "2025-01-28T10:30:00Z"^^xsd:dateTime ;
    prov:wasAttributedTo semspec:user/dev/coby ;
    prov:wasDerivedFrom semspec:spec/api/auth-refresh-v1 .
```

#### 10.3.2 BFO Profile

Includes BFO type assertions:

```turtle
@prefix bfo: <http://purl.obolibrary.org/obo/> .
@prefix prov: <http://www.w3.org/ns/prov#> .
@prefix dc: <http://purl.org/dc/terms/> .
@prefix semspec: <https://semspec.dev/entity/> .
@prefix semspec-ont: <https://semspec.dev/ontology/> .

semspec:proposal/api/auth-refresh
    a bfo:BFO_0000031 ;          # GenericallyDependentContinuant
    a prov:Entity ;
    a semspec-ont:Proposal ;
    dc:title "Auth Token Refresh" ;
    dc:created "2025-01-28T10:30:00Z"^^xsd:dateTime ;
    prov:wasAttributedTo semspec:user/dev/coby ;
    prov:wasDerivedFrom semspec:spec/api/auth-refresh-v1 .
```

#### 10.3.3 Full CCO Profile

Complete BFO + CCO alignment:

```turtle
@prefix bfo: <http://purl.obolibrary.org/obo/> .
@prefix cco: <http://www.ontologyrepository.com/CommonCoreOntologies/> .
@prefix prov: <http://www.w3.org/ns/prov#> .
@prefix dc: <http://purl.org/dc/terms/> .
@prefix semspec: <https://semspec.dev/entity/> .
@prefix semspec-ont: <https://semspec.dev/ontology/> .

# Proposal entity with full type hierarchy
semspec:proposal/api/auth-refresh
    a bfo:BFO_0000031 ;                      # BFO: GenericallyDependentContinuant
    a cco:InformationContentEntity ;          # CCO: Information content
    a prov:Entity ;                           # PROV-O: Entity
    a semspec-ont:Proposal ;                  # Semspec: Proposal
    dc:title "Auth Token Refresh" ;
    dc:description "Add refresh token capability to auth system" ;
    dc:created "2025-01-28T10:30:00Z"^^xsd:dateTime ;
    prov:wasAttributedTo semspec:user/dev/coby ;
    semspec-ont:status "approved" ;
    semspec-ont:priority "high" .

# Spec derived from proposal
semspec:spec/api/auth-refresh-v1
    a bfo:BFO_0000031 ;                      # BFO: GenericallyDependentContinuant
    a cco:DirectiveInformationContentEntity ; # CCO: Directive (prescriptive)
    a prov:Entity ;
    a semspec-ont:Specification ;
    dc:title "Auth Token Refresh Specification" ;
    prov:wasDerivedFrom semspec:proposal/api/auth-refresh ;
    semspec-ont:status "approved" ;
    semspec-ont:version "1.0.0" ;
    cco:is_about semspec:code/file/api/auth-token-go .

# Task from spec
semspec:task/api/impl-refresh-token
    a bfo:BFO_0000031 ;                      # BFO: GenericallyDependentContinuant
    a cco:PlanSpecification ;                 # CCO: Planned action
    a prov:Entity ;
    a semspec-ont:Task ;
    dc:title "Implement RefreshToken function" ;
    prov:wasDerivedFrom semspec:spec/api/auth-refresh-v1 ;
    bfo:BFO_0000062 semspec:task/api/impl-token-store ;  # preceded_by
    semspec-ont:status "complete" .

# Loop (Activity) that executed the task
semspec:loop/api/loop-abc123
    a bfo:BFO_0000015 ;                      # BFO: Process
    a cco:ActOfArtifactProcessing ;          # CCO: Artifact processing
    a prov:Activity ;
    a semspec-ont:Loop ;
    prov:used semspec:task/api/impl-refresh-token ;
    prov:wasAssociatedWith semspec:agent/model/ollama/qwen-32b ;
    prov:wasAssociatedWith semspec:user/dev/coby ;
    prov:startedAtTime "2025-01-28T10:35:00Z"^^xsd:dateTime ;
    prov:endedAtTime "2025-01-28T10:36:45Z"^^xsd:dateTime ;
    semspec-ont:role "implementer" ;
    semspec-ont:iterations 3 ;
    semspec-ont:maxIterations 20 .

# Code file generated by loop
semspec:code/file/api/auth-refresh-go
    a bfo:BFO_0000031 ;                      # BFO: GenericallyDependentContinuant
    a cco:SoftwareCode ;                      # CCO: Software code
    a prov:Entity ;
    a semspec-ont:CodeArtifact ;
    prov:wasGeneratedBy semspec:loop/api/loop-abc123 ;
    prov:wasAttributedTo semspec:agent/model/ollama/qwen-32b ;
    semspec-ont:path "auth/refresh.go" ;
    semspec-ont:language "go" .

# User (Agent)
semspec:user/dev/coby
    a bfo:BFO_0000004 ;                      # BFO: IndependentContinuant
    a cco:Person ;                            # CCO: Person
    a prov:Agent ;
    a prov:Person ;
    dc:title "Coby" .

# AI Model (Agent)
semspec:agent/model/ollama/qwen-32b
    a bfo:BFO_0000004 ;                      # BFO: IndependentContinuant
    a cco:IntelligentSoftwareAgent ;          # CCO: Intelligent software agent
    a prov:Agent ;
    a prov:SoftwareAgent ;
    dc:title "Qwen 2.5 Coder 32B" ;
    prov:actedOnBehalfOf semspec:user/dev/coby .
```

### 10.4 Export Implementation

```go
// export/rdf.go
package export

import (
    "github.com/c360/semstreams/vocabulary/bfo"
    "github.com/c360/semstreams/vocabulary/cco"
    "github.com/c360/semstreams/vocabulary/prov"
)

type ExportProfile string

const (
    ProfileMinimal ExportProfile = "minimal"  // PROV-O, DC, SKOS only
    ProfileBFO     ExportProfile = "bfo"      // + BFO type assertions
    ProfileCCO     ExportProfile = "cco"      // + CCO type assertions (full)
)

type RDFExporter struct {
    profile ExportProfile
    graph   *rdf.Graph
}

func NewRDFExporter(profile ExportProfile) *RDFExporter {
    return &RDFExporter{
        profile: profile,
        graph:   rdf.NewGraph(),
    }
}

func (e *RDFExporter) AddEntity(entity Entity) {
    iri := EntityIDToIRI(entity.EntityID())
    
    // Add profile-specific type assertions
    switch e.profile {
    case ProfileCCO:
        e.addCCOTypes(iri, entity)
        fallthrough
    case ProfileBFO:
        e.addBFOTypes(iri, entity)
        fallthrough
    case ProfileMinimal:
        e.addMinimalTypes(iri, entity)
    }
    
    // Add triples with IRI-mapped predicates
    for _, triple := range entity.Triples() {
        predIRI := PredicateToIRI(triple.Predicate)
        objValue := e.convertObject(triple.Object)
        e.graph.AddTriple(iri, predIRI, objValue)
    }
}

func (e *RDFExporter) addBFOTypes(iri string, entity Entity) {
    bfoClass := GetBFOClass(entity.Type())
    if bfoClass != "" {
        e.graph.AddTriple(iri, rdf.Type, bfoClass)
    }
}

func (e *RDFExporter) addCCOTypes(iri string, entity Entity) {
    ccoClass := GetCCOClass(entity.Type())
    if ccoClass != "" {
        e.graph.AddTriple(iri, rdf.Type, ccoClass)
    }
}

func (e *RDFExporter) addMinimalTypes(iri string, entity Entity) {
    // Add PROV-O type
    provClass := GetPROVClass(entity.Type())
    if provClass != "" {
        e.graph.AddTriple(iri, rdf.Type, provClass)
    }
    
    // Add Semspec domain type
    semspecClass := GetSemspecClass(entity.Type())
    e.graph.AddTriple(iri, rdf.Type, semspecClass)
}

// Type mapping functions
func GetBFOClass(entityType string) string {
    mapping := map[string]string{
        "proposal":     bfo.GenericallyDependentContinuant,
        "spec":         bfo.GenericallyDependentContinuant,
        "task":         bfo.GenericallyDependentContinuant,
        "code_file":    bfo.GenericallyDependentContinuant,
        "loop":         bfo.Process,
        "activity":     bfo.Process,
        "user":         bfo.IndependentContinuant,
        "model":        bfo.IndependentContinuant,
    }
    return mapping[entityType]
}

func GetCCOClass(entityType string) string {
    mapping := map[string]string{
        "proposal":     cco.InformationContentEntity,
        "spec":         cco.DirectiveICE,
        "task":         cco.PlanSpecification,
        "code_file":    cco.SoftwareCode,
        "loop":         cco.ActOfArtifactProcessing,
        "model_call":   cco.ActOfCommunication,
        "tool_call":    cco.ActOfArtifactModification,
        "approval":     cco.ActOfApproval,
        "user":         cco.Person,
        "model":        cco.IntelligentSoftwareAgent,
    }
    return mapping[entityType]
}

func GetPROVClass(entityType string) string {
    mapping := map[string]string{
        "proposal":  prov.Entity,
        "spec":      prov.Entity,
        "task":      prov.Entity,
        "code_file": prov.Entity,
        "loop":      prov.Activity,
        "activity":  prov.Activity,
        "user":      prov.Person,
        "model":     prov.SoftwareAgent,
    }
    return mapping[entityType]
}

// Predicate to IRI conversion
func PredicateToIRI(predicate string) string {
    // Standard vocabulary predicates
    standardMappings := map[string]string{
        "dc.terms.title":         "http://purl.org/dc/terms/title",
        "dc.terms.description":   "http://purl.org/dc/terms/description",
        "dc.terms.creator":       "http://purl.org/dc/terms/creator",
        "dc.terms.created":       "http://purl.org/dc/terms/created",
        "prov.derivation.source": prov.WasDerivedFrom,
        "prov.attribution.agent": prov.WasAttributedTo,
        "prov.generation.activity": prov.WasGeneratedBy,
        "prov.usage.entity":      prov.Used,
        "prov.association.agent": prov.WasAssociatedWith,
        "prov.time.started":      prov.StartedAtTime,
        "prov.time.ended":        prov.EndedAtTime,
        "code.structure.contains": bfo.HasPart,
        "code.structure.belongs":  bfo.PartOf,
        // ... more mappings
    }
    
    if iri, ok := standardMappings[predicate]; ok {
        return iri
    }
    
    // Domain predicates → Semspec namespace
    return "https://semspec.dev/ontology/" + strings.ReplaceAll(predicate, ".", "/")
}

// Export to various formats
func (e *RDFExporter) ToTurtle() string {
    return e.graph.Serialize(rdf.FormatTurtle)
}

func (e *RDFExporter) ToRDFXML() string {
    return e.graph.Serialize(rdf.FormatRDFXML)
}

func (e *RDFExporter) ToJSONLD() string {
    return e.graph.Serialize(rdf.FormatJSONLD)
}

func (e *RDFExporter) ToNTriples() string {
    return e.graph.Serialize(rdf.FormatNTriples)
}
```

### 10.5 Export API Endpoint

```go
// GET /api/export/rdf
// Query params:
//   - format: turtle|rdfxml|jsonld|ntriples (default: turtle)
//   - profile: minimal|bfo|cco (default: cco)
//   - entities: comma-separated entity IDs (optional, all if omitted)
//   - types: comma-separated entity types to include (optional)

func (h *Handler) ExportRDF(w http.ResponseWriter, r *http.Request) {
    format := r.URL.Query().Get("format")
    if format == "" {
        format = "turtle"
    }
    
    profile := ExportProfile(r.URL.Query().Get("profile"))
    if profile == "" {
        profile = ProfileCCO
    }
    
    exporter := NewRDFExporter(profile)
    
    // Get entities based on filters
    entities := h.getEntities(r)
    
    for _, entity := range entities {
        exporter.AddEntity(entity)
    }
    
    // Set content type
    contentTypes := map[string]string{
        "turtle":   "text/turtle",
        "rdfxml":   "application/rdf+xml",
        "jsonld":   "application/ld+json",
        "ntriples": "application/n-triples",
    }
    w.Header().Set("Content-Type", contentTypes[format])
    
    // Write output
    switch format {
    case "turtle":
        w.Write([]byte(exporter.ToTurtle()))
    case "rdfxml":
        w.Write([]byte(exporter.ToRDFXML()))
    case "jsonld":
        w.Write([]byte(exporter.ToJSONLD()))
    case "ntriples":
        w.Write([]byte(exporter.ToNTriples()))
    }
}
```

---

## 11. Validation & Compliance

### 11.1 Ontology Validation

Exported RDF can be validated against:

| Validator | Purpose | Tool |
|-----------|---------|------|
| BFO Conformance | Verify BFO class usage | BFO Validator |
| CCO Conformance | Verify CCO class/property usage | CCO Validator |
| PROV Constraints | Verify PROV-O constraints | PROV-CONSTRAINTS |
| SHACL Shapes | Custom domain validation | Any SHACL processor |

### 11.2 SHACL Shapes for Semspec

```turtle
# shapes/semspec-shapes.ttl
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix semspec: <https://semspec.dev/ontology/> .
@prefix prov: <http://www.w3.org/ns/prov#> .
@prefix dc: <http://purl.org/dc/terms/> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .

# Proposal must have title and status
semspec:ProposalShape
    a sh:NodeShape ;
    sh:targetClass semspec:Proposal ;
    sh:property [
        sh:path dc:title ;
        sh:minCount 1 ;
        sh:maxCount 1 ;
        sh:datatype xsd:string ;
    ] ;
    sh:property [
        sh:path semspec:status ;
        sh:minCount 1 ;
        sh:maxCount 1 ;
        sh:in ("exploring" "drafted" "approved" "implementing" "complete") ;
    ] ;
    sh:property [
        sh:path prov:wasAttributedTo ;
        sh:minCount 1 ;
        sh:class prov:Agent ;
    ] .

# Loop must have associated agent and timing
semspec:LoopShape
    a sh:NodeShape ;
    sh:targetClass semspec:Loop ;
    sh:property [
        sh:path prov:wasAssociatedWith ;
        sh:minCount 1 ;
        sh:class prov:Agent ;
    ] ;
    sh:property [
        sh:path prov:startedAtTime ;
        sh:minCount 1 ;
        sh:datatype xsd:dateTime ;
    ] ;
    sh:property [
        sh:path semspec:role ;
        sh:minCount 1 ;
        sh:in ("planner" "implementer" "reviewer" "general") ;
    ] .

# Code artifacts must have derivation
semspec:GeneratedCodeShape
    a sh:NodeShape ;
    sh:targetClass semspec:CodeArtifact ;
    sh:property [
        sh:path prov:wasGeneratedBy ;
        sh:minCount 1 ;
        sh:class prov:Activity ;
    ] .
```

### 11.3 Compliance Testing

```go
// test/compliance_test.go
package test

import (
    "testing"
    "github.com/c360/semspec/export"
    "github.com/c360/semspec/vocabulary/bfo"
    "github.com/c360/semspec/vocabulary/cco"
)

func TestBFOCompliance(t *testing.T) {
    tests := []struct {
        entityType  string
        expectedBFO string
    }{
        {"proposal", bfo.GenericallyDependentContinuant},
        {"spec", bfo.GenericallyDependentContinuant},
        {"task", bfo.GenericallyDependentContinuant},
        {"loop", bfo.Process},
        {"model_call", bfo.Process},
        {"user", bfo.IndependentContinuant},
    }
    
    for _, tt := range tests {
        t.Run(tt.entityType, func(t *testing.T) {
            result := export.GetBFOClass(tt.entityType)
            if result != tt.expectedBFO {
                t.Errorf("BFO class for %s: got %s, want %s", 
                    tt.entityType, result, tt.expectedBFO)
            }
        })
    }
}

func TestCCOCompliance(t *testing.T) {
    tests := []struct {
        entityType  string
        expectedCCO string
    }{
        {"proposal", cco.InformationContentEntity},
        {"spec", cco.DirectiveICE},
        {"task", cco.PlanSpecification},
        {"code_file", cco.SoftwareCode},
        {"loop", cco.ActOfArtifactProcessing},
        {"model_call", cco.ActOfCommunication},
        {"approval", cco.ActOfApproval},
        {"user", cco.Person},
        {"model", cco.IntelligentSoftwareAgent},
    }
    
    for _, tt := range tests {
        t.Run(tt.entityType, func(t *testing.T) {
            result := export.GetCCOClass(tt.entityType)
            if result != tt.expectedCCO {
                t.Errorf("CCO class for %s: got %s, want %s", 
                    tt.entityType, result, tt.expectedCCO)
            }
        })
    }
}

func TestPredicateIRIMappings(t *testing.T) {
    tests := []struct {
        predicate   string
        expectedIRI string
    }{
        {"dc.terms.title", "http://purl.org/dc/terms/title"},
        {"prov.derivation.source", "http://www.w3.org/ns/prov#wasDerivedFrom"},
        {"code.structure.contains", bfo.HasPart},
        {"semspec.task.predecessor", bfo.PrecededBy},
    }
    
    for _, tt := range tests {
        t.Run(tt.predicate, func(t *testing.T) {
            result := export.PredicateToIRI(tt.predicate)
            if result != tt.expectedIRI {
                t.Errorf("IRI for %s: got %s, want %s", 
                    tt.predicate, result, tt.expectedIRI)
            }
        })
    }
}

func TestRDFExportValid(t *testing.T) {
    // Create test entities
    proposal := NewProposal("auth-refresh", "Add token refresh")
    proposal.SetStatus("approved")
    proposal.SetCreator("user:coby")
    
    // Export
    exporter := export.NewRDFExporter(export.ProfileCCO)
    exporter.AddEntity(proposal)
    
    turtle := exporter.ToTurtle()
    
    // Validate contains required type assertions
    requiredAssertions := []string{
        "a bfo:BFO_0000031",                       // BFO GDC
        "a cco:InformationContentEntity",          // CCO ICE
        "a prov:Entity",                           // PROV Entity
        "prov:wasAttributedTo",                    // Has attribution
    }
    
    for _, assertion := range requiredAssertions {
        if !strings.Contains(turtle, assertion) {
            t.Errorf("Export missing required assertion: %s", assertion)
        }
    }
}

func TestSHACLValidation(t *testing.T) {
    // Export entities
    exporter := export.NewRDFExporter(export.ProfileCCO)
    // ... add entities ...
    
    rdfData := exporter.ToTurtle()
    
    // Load SHACL shapes
    shapes := loadFile("../shapes/semspec-shapes.ttl")
    
    // Validate (using SHACL library)
    report := shacl.Validate(rdfData, shapes)
    
    if !report.Conforms() {
        for _, result := range report.Results() {
            t.Errorf("SHACL violation: %s - %s", 
                result.FocusNode(), result.Message())
        }
    }
}
```

### 11.4 Interoperability Testing

```go
func TestImportToExternalTripleStore(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }
    
    // Export from Semspec
    exporter := export.NewRDFExporter(export.ProfileCCO)
    // ... add test entities ...
    ntriples := exporter.ToNTriples()
    
    // Import to GraphDB (or other triple store)
    client := graphdb.NewClient("http://localhost:7200")
    err := client.ImportNTriples("semspec-test", ntriples)
    if err != nil {
        t.Fatalf("Failed to import to GraphDB: %v", err)
    }
    
    // Query to verify BFO types
    query := `
        PREFIX bfo: <http://purl.obolibrary.org/obo/>
        SELECT ?entity WHERE {
            ?entity a bfo:BFO_0000031 .
        }
    `
    results, err := client.Query("semspec-test", query)
    if err != nil {
        t.Fatalf("Query failed: %v", err)
    }
    
    if len(results) == 0 {
        t.Error("No BFO-typed entities found after import")
    }
}
```

---

## 12. Implementation

### 12.1 Package Structure

```
vocabulary/
├── bfo/
│   ├── bfo.go          # BFO IRI constants
│   └── bfo_test.go
├── cco/
│   ├── cco.go          # CCO IRI constants
│   └── cco_test.go
├── prov/
│   ├── prov.go         # PROV-O IRI constants
│   └── prov_test.go
├── dc/
│   ├── dc.go           # Dublin Core IRI constants
│   └── dc_test.go
├── skos/
│   ├── skos.go         # SKOS IRI constants
│   └── skos_test.go
├── semspec/
│   ├── predicates.go   # Semspec domain predicates
│   ├── iris.go         # Semspec IRI definitions
│   ├── mappings.go     # Predicate → IRI mappings
│   └── semspec_test.go
├── registry.go         # Predicate registration
└── doc.go

export/
├── rdf.go              # RDF export implementation
├── profiles.go         # Export profiles (minimal, BFO, CCO)
├── formats.go          # Format serializers
└── export_test.go

shapes/
├── semspec-shapes.ttl  # SHACL validation shapes
└── README.md
```

### 12.2 Registration Example

```go
// vocabulary/semspec/predicates.go
package semspec

import "github.com/c360/semstreams/vocabulary"

func init() {
    // Register all Semspec predicates with their metadata
    
    // Proposal predicates
    vocabulary.Register(ProposalStatus,
        vocabulary.WithDescription("Lifecycle status of the proposal"),
        vocabulary.WithDataType("string"),
        vocabulary.WithEnum("exploring", "drafted", "approved", "implementing", "complete"),
        vocabulary.WithBFOClass(bfo.GenericallyDependentContinuant),
        vocabulary.WithCCOClass(cco.InformationContentEntity),
    )
    
    vocabulary.Register(ProposalSpec,
        vocabulary.WithDescription("Specification derived from this proposal"),
        vocabulary.WithDataType("entity_id"),
        vocabulary.WithIRI(prov.WasDerivedFrom), // inverse relationship
        vocabulary.WithRelationship(true),
    )
    
    // Loop predicates
    vocabulary.Register(LoopStatus,
        vocabulary.WithDescription("Current execution status of the loop"),
        vocabulary.WithDataType("string"),
        vocabulary.WithEnum("executing", "paused", "awaiting_approval", "complete", "failed", "cancelled"),
        vocabulary.WithBFOClass(bfo.Process),
        vocabulary.WithCCOClass(cco.ActOfArtifactProcessing),
    )
    
    vocabulary.Register(LoopTask,
        vocabulary.WithDescription("Task being executed by this loop"),
        vocabulary.WithDataType("entity_id"),
        vocabulary.WithIRI(prov.Used),
        vocabulary.WithRelationship(true),
    )
    
    // ... register all predicates
}
```

### 12.3 Entity Implementation Example

```go
// entity/proposal.go
package entity

import (
    "time"
    "github.com/c360/semstreams/message"
    vocab "github.com/c360/semspec/vocabulary/semspec"
)

type Proposal struct {
    ID          string
    Title       string
    Description string
    Status      string
    Priority    string
    CreatedBy   string
    CreatedAt   time.Time
    SpecID      string // optional
}

func (p *Proposal) EntityID() string {
    return fmt.Sprintf("acme.semspec.project.proposal.%s.%s", p.Project, p.ID)
}

func (p *Proposal) Type() string {
    return "proposal"
}

func (p *Proposal) Triples() []message.Triple {
    triples := []message.Triple{
        // Standard metadata (Dublin Core)
        {Subject: p.EntityID(), Predicate: vocab.DcTitle, Object: p.Title},
        {Subject: p.EntityID(), Predicate: vocab.DcDescription, Object: p.Description},
        {Subject: p.EntityID(), Predicate: vocab.DcCreated, Object: p.CreatedAt},
        {Subject: p.EntityID(), Predicate: vocab.DcType, Object: "proposal"},
        
        // Provenance (PROV-O)
        {Subject: p.EntityID(), Predicate: vocab.ProvAttributedTo, Object: p.CreatedBy},
        
        // Domain predicates
        {Subject: p.EntityID(), Predicate: vocab.ProposalStatus, Object: p.Status},
        {Subject: p.EntityID(), Predicate: vocab.ProposalPriority, Object: p.Priority},
    }
    
    // Optional relationships
    if p.SpecID != "" {
        triples = append(triples, message.Triple{
            Subject:   p.EntityID(),
            Predicate: vocab.ProposalSpec,
            Object:    p.SpecID,
        })
    }
    
    return triples
}
```

---

## Appendix A: Quick Reference Cards

### A.1 BFO Quick Reference

| BFO Class | ID | Semspec Usage |
|-----------|-----|---------------|
| GenericallyDependentContinuant | BFO_0000031 | Proposals, Specs, Tasks, Code |
| Process | BFO_0000015 | Loops, Activities |
| IndependentContinuant | BFO_0000004 | Users, AI Models |
| Role | BFO_0000023 | Agent roles (planner, implementer) |
| Quality | BFO_0000019 | Metrics (token count, duration) |

### A.2 CCO Quick Reference

| CCO Class | Semspec Usage |
|-----------|---------------|
| InformationContentEntity | Proposals |
| DirectiveInformationContentEntity | Specs |
| PlanSpecification | Tasks |
| SoftwareCode | Code artifacts |
| ActOfArtifactProcessing | Loops |
| ActOfCommunication | Model calls |
| ActOfApproval | Approvals |
| Person | Users |
| IntelligentSoftwareAgent | AI Models |

### A.3 PROV-O Quick Reference

| PROV-O Relation | Usage |
|-----------------|-------|
| wasGeneratedBy | Entity ← Activity (code from loop) |
| wasDerivedFrom | Entity ← Entity (spec from proposal) |
| wasAttributedTo | Entity ← Agent (created by user) |
| used | Activity → Entity (loop used task) |
| wasAssociatedWith | Activity → Agent (loop by model) |
| actedOnBehalfOf | Agent → Agent (model for user) |

---

## Appendix B: Government Compliance Notes

### B.1 DoD/IC Ontology Alignment

This vocabulary specification supports interoperability with:

- **DoD Enterprise Ontology**: BFO/CCO-based ontology program
- **Intelligence Community Ontology**: Also BFO-based
- **NATO C3 Taxonomy**: Can map via CCO
- **OGC Standards**: Spatial predicates can map to OGC vocabularies

### B.2 Certification Considerations

For systems requiring formal ontology certification:

1. **BFO Conformance**: Entities correctly typed per BFO 2020
2. **CCO Conformance**: Domain classes align with CCO 2.0
3. **PROV Conformance**: Provenance chains valid per PROV-CONSTRAINTS
4. **Export Validation**: SHACL shapes enforce data quality

### B.3 Data Exchange

Semspec data can be exchanged with government systems via:

- Standard RDF formats (Turtle, RDF/XML, JSON-LD)
- SPARQL endpoints (when backed by triple store)
- OWL ontology for schema exchange
- SHACL shapes for validation contracts

---

**END OF SPECIFICATION**
