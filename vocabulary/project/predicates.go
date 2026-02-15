// Package project provides vocabulary predicates for project entities.
// Projects are containers that group related sources and plans together.
package project

import "github.com/c360studio/semstreams/vocabulary"

// Namespace for project vocabulary.
const Namespace = "https://semspec.dev/project#"

// Project identity predicates define core project attributes.
const (
	// ProjectName is the unique identifier (slug) for the project.
	// Used as the primary lookup key and in file paths.
	ProjectName = "project.identity.name"

	// ProjectTitle is the human-readable display title.
	ProjectTitle = "project.identity.title"

	// ProjectDescription provides additional context about the project.
	ProjectDescription = "project.identity.description"
)

// Project metadata predicates track lifecycle information.
const (
	// ProjectCreatedAt is the RFC3339 timestamp when the project was created.
	ProjectCreatedAt = "project.meta.created_at"

	// ProjectCreatedBy is the user/agent who created the project.
	ProjectCreatedBy = "project.meta.created_by"

	// ProjectUpdatedAt is the RFC3339 timestamp of last modification.
	ProjectUpdatedAt = "project.meta.updated_at"

	// ProjectArchivedAt is the RFC3339 timestamp when the project was archived.
	// Only present if the project has been soft-deleted.
	ProjectArchivedAt = "project.meta.archived_at"
)

// Project relationship predicates define graph connections.
const (
	// ProjectHasSource links a project to its source entities.
	// Points to source entity IDs (documents, repositories, web sources).
	ProjectHasSource = "project.relationship.has_source"

	// ProjectHasPlan links a project to its plan entities.
	// Points to plan entity IDs.
	ProjectHasPlan = "project.relationship.has_plan"
)

// Project status values.
const (
	// StatusActive indicates the project is in active use.
	StatusActive = "active"

	// StatusArchived indicates the project has been soft-deleted.
	StatusArchived = "archived"
)

// ProjectStatus is the current state of the project.
const ProjectStatus = "project.meta.status"

func init() {
	// Register identity predicates
	vocabulary.Register(ProjectName,
		vocabulary.WithDescription("Unique identifier (slug) for the project"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"name"))

	vocabulary.Register(ProjectTitle,
		vocabulary.WithDescription("Human-readable display title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.DcTitle))

	vocabulary.Register(ProjectDescription,
		vocabulary.WithDescription("Additional context about the project"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/description"))

	// Register metadata predicates
	vocabulary.Register(ProjectCreatedAt,
		vocabulary.WithDescription("Creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))

	vocabulary.Register(ProjectCreatedBy,
		vocabulary.WithDescription("User/agent who created the project"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(ProjectUpdatedAt,
		vocabulary.WithDescription("Last modification timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI("http://purl.org/dc/terms/modified"))

	vocabulary.Register(ProjectArchivedAt,
		vocabulary.WithDescription("Archive timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(Namespace+"archivedAt"))

	vocabulary.Register(ProjectStatus,
		vocabulary.WithDescription("Current project state: active or archived"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"status"))

	// Register relationship predicates
	vocabulary.Register(ProjectHasSource,
		vocabulary.WithDescription("Links project to source entities"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"hasSource"))

	vocabulary.Register(ProjectHasPlan,
		vocabulary.WithDescription("Links project to plan entities"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"hasPlan"))
}
