// Package query provides a graph query processor component for querying
// entities and relationships in the knowledge graph.
package query

// Query operation types
type QueryType string

const (
	// QueryEntity retrieves a single entity by ID
	QueryEntity QueryType = "entity"

	// QueryRelated finds entities related to a given entity
	QueryRelated QueryType = "related"

	// QueryDependsOn finds what the given entity depends on
	QueryDependsOn QueryType = "depends_on"

	// QueryDependedBy finds what depends on the given entity
	QueryDependedBy QueryType = "depended_by"

	// QueryImplements finds types that implement an interface
	QueryImplements QueryType = "implements"

	// QueryContains finds entities contained by a given entity
	QueryContains QueryType = "contains"

	// QuerySearch performs a text search across entities
	QuerySearch QueryType = "search"
)

// Relationship types for navigation
type RelationType string

const (
	RelContains   RelationType = "code.structure.contains"
	RelBelongsTo  RelationType = "code.structure.belongs"
	RelImports    RelationType = "code.dependency.imports"
	RelImplements RelationType = "code.relationship.implements"
	RelEmbeds     RelationType = "code.relationship.embeds"
	RelCalls      RelationType = "code.relationship.calls"
	RelReferences RelationType = "code.relationship.references"
)
