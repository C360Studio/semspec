package agentgraph

import (
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/pkg/types"
)

// Entity ID component constants. These define the fixed hierarchy positions
// used when constructing 6-part graph entity IDs for agentic resources.
//
// Format: org.platform.domain.system.type.instance
// Example loop:  semspec.local.agentic.orchestrator.loop.<loopID>
// Example task:  semspec.local.agentic.orchestrator.task.<taskID>
// Example DAG:   semspec.local.agentic.orchestrator.dag.<dagID>
const (
	OrgDefault         = "semspec"
	PlatformDefault    = "local"
	DomainAgentic      = "agentic"
	SystemOrchestrator = "orchestrator"
	TypeLoop           = "loop"
	TypeTask           = "task"
	TypeDAG            = "dag"
	TypeAgent          = "agent"
	TypeReview         = "review"
	TypeErrorCategory  = "error-category"

	// SourceSemspec is the source identifier stamped on triples created by Semspec.
	// It enables provenance filtering when querying the graph.
	SourceSemspec = "semspec"
)

// ValidateInstanceID checks that an instance ID is valid for use in a 6-part entity ID.
// It must be non-empty and must not contain dots (which would break the 6-part format).
func ValidateInstanceID(id string) error {
	if id == "" {
		return fmt.Errorf("agentgraph: instance ID must not be empty")
	}
	if strings.Contains(id, ".") {
		return fmt.Errorf("agentgraph: instance ID %q must not contain dots", id)
	}
	return nil
}

// LoopEntityID returns the full 6-part graph entity ID string for an agent loop.
// Format: semspec.local.agentic.orchestrator.loop.<loopID>
// Panics if loopID is empty or contains dots.
func LoopEntityID(loopID string) string {
	if err := ValidateInstanceID(loopID); err != nil {
		panic(err)
	}
	return LoopEntityIDParsed(loopID).String()
}

// TaskEntityID returns the full 6-part graph entity ID string for a task.
// Format: semspec.local.agentic.orchestrator.task.<taskID>
// Panics if taskID is empty or contains dots.
func TaskEntityID(taskID string) string {
	if err := ValidateInstanceID(taskID); err != nil {
		panic(err)
	}
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeTask,
		Instance: taskID,
	}.String()
}

// DAGEntityID returns the full 6-part graph entity ID string for a DAG execution.
// Format: semspec.local.agentic.orchestrator.dag.<dagID>
// Panics if dagID is empty or contains dots.
func DAGEntityID(dagID string) string {
	if err := ValidateInstanceID(dagID); err != nil {
		panic(err)
	}
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeDAG,
		Instance: dagID,
	}.String()
}

// LoopEntityIDParsed returns a structured EntityID for an agent loop.
// Prefer LoopEntityID when only the string form is needed.
func LoopEntityIDParsed(loopID string) types.EntityID {
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeLoop,
		Instance: loopID,
	}
}

// LoopTypePrefix returns the 5-part prefix that identifies the loop entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all loop entities.
// Format: semspec.local.agentic.orchestrator.loop
func LoopTypePrefix() string {
	return LoopEntityIDParsed("_").TypePrefix()
}

// TaskTypePrefix returns the 5-part prefix that identifies the task entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all task entities.
// Format: semspec.local.agentic.orchestrator.task
func TaskTypePrefix() string {
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeTask,
		Instance: "_",
	}.TypePrefix()
}

// DAGTypePrefix returns the 5-part prefix that identifies the DAG entity type.
// Format: semspec.local.agentic.orchestrator.dag
func DAGTypePrefix() string {
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeDAG,
		Instance: "_",
	}.TypePrefix()
}

// ParseEntityID extracts the instance component from a 6-part entity ID string.
// Returns the instance string and ok=true if the ID is valid; otherwise returns
// an empty string and ok=false.
func ParseEntityID(entityID string) (instance string, ok bool) {
	parsed, err := types.ParseEntityID(entityID)
	if err != nil {
		return "", false
	}
	return parsed.Instance, true
}

// AgentEntityID returns the full 6-part graph entity ID string for a persistent agent.
// Format: semspec.local.agentic.orchestrator.agent.<agentID>
// Panics if agentID is empty or contains dots.
func AgentEntityID(agentID string) string {
	if err := ValidateInstanceID(agentID); err != nil {
		panic(err)
	}
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeAgent,
		Instance: agentID,
	}.String()
}

// AgentTypePrefix returns the 5-part prefix that identifies the agent entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all agent entities.
// Format: semspec.local.agentic.orchestrator.agent
func AgentTypePrefix() string {
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeAgent,
		Instance: "_",
	}.TypePrefix()
}

// ReviewEntityID returns the full 6-part graph entity ID string for a review record.
// Format: semspec.local.agentic.orchestrator.review.<reviewID>
// Panics if reviewID is empty or contains dots.
func ReviewEntityID(reviewID string) string {
	if err := ValidateInstanceID(reviewID); err != nil {
		panic(err)
	}
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeReview,
		Instance: reviewID,
	}.String()
}

// ReviewTypePrefix returns the 5-part prefix that identifies the review entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all review entities.
// Format: semspec.local.agentic.orchestrator.review
func ReviewTypePrefix() string {
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeReview,
		Instance: "_",
	}.TypePrefix()
}

// ErrorCategoryEntityID returns the full 6-part graph entity ID string for an error category.
// Format: semspec.local.agentic.orchestrator.error-category.<categoryID>
// Panics if categoryID is empty or contains dots.
func ErrorCategoryEntityID(categoryID string) string {
	if err := ValidateInstanceID(categoryID); err != nil {
		panic(err)
	}
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeErrorCategory,
		Instance: categoryID,
	}.String()
}

// ErrorCategoryTypePrefix returns the 5-part prefix that identifies the error category entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all error category entities.
// Format: semspec.local.agentic.orchestrator.error-category
func ErrorCategoryTypePrefix() string {
	return types.EntityID{
		Org:      OrgDefault,
		Platform: PlatformDefault,
		Domain:   DomainAgentic,
		System:   SystemOrchestrator,
		Type:     TypeErrorCategory,
		Instance: "_",
	}.TypePrefix()
}
