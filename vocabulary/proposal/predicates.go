package proposal

import "github.com/c360studio/semstreams/vocabulary"

// Namespace for proposal predicates.
const Namespace = "https://semspec.dev/vocabulary/proposal#"

// PROV-O IRI constants for temporal predicates.
const (
	// ProvGeneratedAtTime indicates when an entity was generated.
	ProvGeneratedAtTime = "http://www.w3.org/ns/prov#generatedAtTime"
)

// Core proposal predicates.
const (
	// PredicateTitle is the proposal title.
	PredicateTitle = "semspec.proposal.title"

	// PredicateDescription is the proposal description/summary.
	PredicateDescription = "semspec.proposal.description"

	// PredicateStatus is the workflow status.
	// Values: created, drafted, reviewed, approved, implementing, complete, archived, rejected
	PredicateStatus = "semspec.proposal.status"

	// PredicateSlug is the URL-safe identifier for the proposal.
	PredicateSlug = "semspec.proposal.slug"

	// PredicateAuthor is the user who created the proposal.
	PredicateAuthor = "semspec.proposal.author"

	// PredicateCreatedAt is the RFC3339 timestamp when the proposal was created.
	PredicateCreatedAt = "semspec.proposal.created_at"

	// PredicateUpdatedAt is the RFC3339 timestamp when the proposal was last updated.
	PredicateUpdatedAt = "semspec.proposal.updated_at"
)

// File tracking predicates.
const (
	// PredicateHasProposal indicates whether proposal.md exists.
	PredicateHasProposal = "semspec.proposal.has_proposal"

	// PredicateHasDesign indicates whether design.md exists.
	PredicateHasDesign = "semspec.proposal.has_design"

	// PredicateHasSpec indicates whether spec.md exists.
	PredicateHasSpec = "semspec.proposal.has_spec"

	// PredicateHasTasks indicates whether tasks.md exists.
	PredicateHasTasks = "semspec.proposal.has_tasks"
)

// Relationship predicates.
const (
	// PredicateHasSpec links a proposal to its specification entity.
	PredicateSpec = "semspec.proposal.spec"

	// PredicateHasTask links a proposal to task entities.
	PredicateTask = "semspec.proposal.task"
)

// GitHub integration predicates.
const (
	// PredicateGitHubEpic is the GitHub epic issue number.
	PredicateGitHubEpic = "semspec.proposal.github.epic"

	// PredicateGitHubRepo is the GitHub repository (owner/repo format).
	PredicateGitHubRepo = "semspec.proposal.github.repo"
)

func init() {
	// Register core predicates
	vocabulary.Register(PredicateTitle,
		vocabulary.WithDescription("Proposal title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"title"))

	vocabulary.Register(PredicateDescription,
		vocabulary.WithDescription("Proposal description or summary"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"description"))

	vocabulary.Register(PredicateStatus,
		vocabulary.WithDescription("Workflow status"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"status"))

	vocabulary.Register(PredicateSlug,
		vocabulary.WithDescription("URL-safe identifier"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"slug"))

	vocabulary.Register(PredicateAuthor,
		vocabulary.WithDescription("Creator of the proposal"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(PredicateCreatedAt,
		vocabulary.WithDescription("Creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(ProvGeneratedAtTime))

	vocabulary.Register(PredicateUpdatedAt,
		vocabulary.WithDescription("Last update timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"))

	// Register file tracking predicates
	vocabulary.Register(PredicateHasProposal,
		vocabulary.WithDescription("Whether proposal.md exists"),
		vocabulary.WithDataType("bool"))

	vocabulary.Register(PredicateHasDesign,
		vocabulary.WithDescription("Whether design.md exists"),
		vocabulary.WithDataType("bool"))

	vocabulary.Register(PredicateHasSpec,
		vocabulary.WithDescription("Whether spec.md exists"),
		vocabulary.WithDataType("bool"))

	vocabulary.Register(PredicateHasTasks,
		vocabulary.WithDescription("Whether tasks.md exists"),
		vocabulary.WithDataType("bool"))

	// Register relationship predicates
	vocabulary.Register(PredicateSpec,
		vocabulary.WithDescription("Link to specification entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"hasSpec"))

	vocabulary.Register(PredicateTask,
		vocabulary.WithDescription("Link to task entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"hasTask"))

	// Register GitHub predicates
	vocabulary.Register(PredicateGitHubEpic,
		vocabulary.WithDescription("GitHub epic issue number"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(PredicateGitHubRepo,
		vocabulary.WithDescription("GitHub repository (owner/repo)"),
		vocabulary.WithDataType("string"))
}
