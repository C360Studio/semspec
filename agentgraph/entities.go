package agentgraph

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/pkg/types"
)

// Entity ID component constants. These define the fixed hierarchy positions
// used when constructing 6-part graph entity IDs for agentic resources.
//
// Format: {org}.{platform}.{domain}.{system}.{type}.{instance}
//
// Example loop:   semspec.local.agent.loop.loop.<loopID>
// Example task:   semspec.local.agent.loop.task.<taskID>
// Example DAG:    semspec.local.agent.loop.dag.<dagID>
// Example agent:  semspec.local.agent.roster.agent.<agentID>
// Example review: semspec.local.agent.roster.review.<reviewID>
// Example errcat: semspec.local.agent.roster.errcat.<categoryID>
// Example team:   semspec.local.agent.team.team.<teamID>
// Example insight:semspec.local.agent.team.insight.<teamID>-<insightID>
const (
	DomainAgent       = "agent"
	SystemLoop        = "loop"
	SystemRoster      = "roster"
	SystemTeam        = "team"
	TypeLoop          = "loop"
	TypeTask          = "task"
	TypeDAG           = "dag"
	TypeAgent         = "agent"
	TypeReview        = "review"
	TypeErrorCategory = "errcat"
	TypeTeam          = "team"
	TypeInsight       = "insight"

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

// entityID constructs a 6-part graph entity ID from the fixed hierarchy components.
// Format: {prefix}.{domain}.{system}.{typ}.{instance}
func entityID(domain, system, typ, instance string) string {
	return fmt.Sprintf("%s.%s.%s.%s.%s", workflow.EntityPrefix(), domain, system, typ, instance)
}

// typePrefix constructs a 5-part entity type prefix for listing/matching.
// Format: {prefix}.{domain}.{system}.{typ}
func typePrefix(domain, system, typ string) string {
	return fmt.Sprintf("%s.%s.%s.%s", workflow.EntityPrefix(), domain, system, typ)
}

// LoopEntityID returns the full 6-part graph entity ID string for an agent loop.
// Format: {prefix}.agent.loop.loop.{hash}
func LoopEntityID(loopID string) string {
	return entityID(DomainAgent, SystemLoop, TypeLoop, workflow.HashInstanceID(loopID))
}

// LoopTypePrefix returns the 5-part prefix that identifies the loop entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all loop entities.
// Format: {prefix}.agent.loop.loop
func LoopTypePrefix() string {
	return typePrefix(DomainAgent, SystemLoop, TypeLoop)
}

// TaskEntityID returns the full 6-part graph entity ID string for a task.
// Format: {prefix}.agent.loop.task.{hash}
func TaskEntityID(taskID string) string {
	return entityID(DomainAgent, SystemLoop, TypeTask, workflow.HashInstanceID(taskID))
}

// TaskTypePrefix returns the 5-part prefix that identifies the task entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all task entities.
// Format: {prefix}.agent.loop.task
func TaskTypePrefix() string {
	return typePrefix(DomainAgent, SystemLoop, TypeTask)
}

// DAGEntityID returns the full 6-part graph entity ID string for a DAG execution.
// Format: {prefix}.agent.loop.dag.{hash}
func DAGEntityID(dagID string) string {
	return entityID(DomainAgent, SystemLoop, TypeDAG, workflow.HashInstanceID(dagID))
}

// DAGTypePrefix returns the 5-part prefix that identifies the DAG entity type.
// Format: {prefix}.agent.loop.dag
func DAGTypePrefix() string {
	return typePrefix(DomainAgent, SystemLoop, TypeDAG)
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
// Format: {prefix}.agent.roster.agent.{hash}
func AgentEntityID(agentID string) string {
	return entityID(DomainAgent, SystemRoster, TypeAgent, workflow.HashInstanceID(agentID))
}

// AgentTypePrefix returns the 5-part prefix that identifies the agent entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all agent entities.
// Format: {prefix}.agent.roster.agent
func AgentTypePrefix() string {
	return typePrefix(DomainAgent, SystemRoster, TypeAgent)
}

// ReviewEntityID returns the full 6-part graph entity ID string for a review record.
// Format: {prefix}.agent.roster.review.{hash}
func ReviewEntityID(reviewID string) string {
	return entityID(DomainAgent, SystemRoster, TypeReview, workflow.HashInstanceID(reviewID))
}

// ReviewTypePrefix returns the 5-part prefix that identifies the review entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all review entities.
// Format: {prefix}.agent.roster.review
func ReviewTypePrefix() string {
	return typePrefix(DomainAgent, SystemRoster, TypeReview)
}

// ErrorCategoryEntityID returns the full 6-part graph entity ID string for an error category.
// Format: {prefix}.agent.roster.errcat.{hash}
func ErrorCategoryEntityID(categoryID string) string {
	return entityID(DomainAgent, SystemRoster, TypeErrorCategory, workflow.HashInstanceID(categoryID))
}

// ErrorCategoryTypePrefix returns the 5-part prefix that identifies the error category entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all error category entities.
// Format: {prefix}.agent.roster.errcat
func ErrorCategoryTypePrefix() string {
	return typePrefix(DomainAgent, SystemRoster, TypeErrorCategory)
}

// TeamEntityID returns the full 6-part graph entity ID string for a team.
// Format: {prefix}.agent.team.team.{hash}
func TeamEntityID(teamID string) string {
	return entityID(DomainAgent, SystemTeam, TypeTeam, workflow.HashInstanceID(teamID))
}

// TeamTypePrefix returns the 5-part prefix that identifies the team entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all team entities.
// Format: {prefix}.agent.team.team
func TeamTypePrefix() string {
	return typePrefix(DomainAgent, SystemTeam, TypeTeam)
}

// TeamInsightEntityID returns the full 6-part graph entity ID string for a team insight.
// Format: {prefix}.agent.team.insight.{hash}
func TeamInsightEntityID(teamID, insightID string) string {
	return entityID(DomainAgent, SystemTeam, TypeInsight, workflow.HashInstanceID(teamID, insightID))
}

// TeamInsightTypePrefix returns the 5-part prefix that identifies the team insight entity type.
// Use this prefix with EntityManager.ListWithPrefix to enumerate all team insight entities.
// Format: {prefix}.agent.team.insight
func TeamInsightTypePrefix() string {
	return typePrefix(DomainAgent, SystemTeam, TypeInsight)
}
