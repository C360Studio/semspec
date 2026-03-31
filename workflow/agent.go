package workflow

import "time"

// AgentStatus represents the lifecycle state of a persistent agent.
type AgentStatus string

const (
	// AgentAvailable indicates the agent is ready to accept work.
	AgentAvailable AgentStatus = "available"

	// AgentBusy indicates the agent is currently executing a task.
	AgentBusy AgentStatus = "busy"

	// AgentBenched indicates the agent has been excluded from selection due to
	// accumulated errors (Phase B: count >= 3). Benched agents are not dispatched.
	AgentBenched AgentStatus = "benched"

	// AgentRetired indicates the agent has been permanently removed from the roster.
	AgentRetired AgentStatus = "retired"
)

// AgentPersona carries optional persona configuration for an agent.
// When set, persona data is injected into prompt assembly and used for display
// in UI and logs. Configured via the team roster in semspec.json.
// ADR-030 builds vocabulary and prompt fragments on top of this struct.
type AgentPersona struct {
	DisplayName  string   `json:"display_name,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Backstory    string   `json:"backstory,omitempty"`
	Traits       []string `json:"traits,omitempty"`
	Style        string   `json:"style,omitempty"`
}

// Agent is a persistent agent identity with accumulated history.
// Multiple agents can share the same model — identity is independent of model assignment.
// Agents are stored as graph entities and accumulate review scores and error counts
// across task executions, enabling trend detection and escalation.
type Agent struct {
	// ID is a UUID that serves as the entity instance identifier in the graph.
	ID string

	// Name is the human-readable agent name, e.g. "developer-alpha".
	Name string

	// Role is the agent's functional role, e.g. "developer", "reviewer".
	Role string

	// Model is the current model assignment. Model can change over time while
	// the agent's identity and accumulated history remain stable.
	Model string

	// Status is the current lifecycle state of the agent.
	Status AgentStatus

	// Persona carries optional persona configuration (display name, system prompt,
	// traits). When nil, the agent uses default domain fragment identity.
	// NOTE: Persona is not yet persisted as a graph triple — it is populated from
	// the roster config during seedTeams(). ADR-030 will add graph persistence.
	Persona *AgentPersona

	// ErrorCounts tracks the accumulated number of occurrences per error category.
	// The map is created on first use via IncrementErrorCount.
	ErrorCounts map[ErrorCategory]int

	// ReviewStats holds running averages from peer reviews of this agent's work.
	ReviewStats ReviewStats

	// CreatedAt is when the agent entity was first created.
	CreatedAt time.Time

	// UpdatedAt is when the agent entity was last modified.
	UpdatedAt time.Time
}

// ReviewStats holds running averages from peer reviews.
// Q1, Q2, and Q3 correspond to the three review dimensions:
// correctness, quality, and completeness.
type ReviewStats struct {
	// Q1CorrectnessAvg is the running average score for correctness (0–10).
	Q1CorrectnessAvg float64

	// Q2QualityAvg is the running average score for code quality (0–10).
	Q2QualityAvg float64

	// Q3CompletenessAvg is the running average score for completeness (0–10).
	Q3CompletenessAvg float64

	// OverallAvg is the mean of the three dimension averages.
	OverallAvg float64

	// ReviewCount is the total number of reviews incorporated into the averages.
	ReviewCount int
}

// UpdateStats incorporates a new review into the running averages.
// All three dimension scores are expected as integers in the range 0–10.
// The running average is computed as (oldAvg * oldCount + newScore) / (oldCount + 1),
// which avoids storing the full history while maintaining an accurate running mean.
func (s *ReviewStats) UpdateStats(q1, q2, q3 int) {
	n := float64(s.ReviewCount)
	s.Q1CorrectnessAvg = (s.Q1CorrectnessAvg*n + float64(q1)) / (n + 1)
	s.Q2QualityAvg = (s.Q2QualityAvg*n + float64(q2)) / (n + 1)
	s.Q3CompletenessAvg = (s.Q3CompletenessAvg*n + float64(q3)) / (n + 1)
	s.OverallAvg = (s.Q1CorrectnessAvg + s.Q2QualityAvg + s.Q3CompletenessAvg) / 3
	s.ReviewCount++
}

// IncrementErrorCount increments the accumulated count for the given error category.
// The ErrorCounts map is initialised lazily on first call.
func (a *Agent) IncrementErrorCount(category ErrorCategory) {
	if a.ErrorCounts == nil {
		a.ErrorCounts = make(map[ErrorCategory]int)
	}
	a.ErrorCounts[category]++
}

// DefaultBenchingThreshold is the minimum error count per category that triggers
// agent benching. When any single error category reaches this count, the agent
// should be excluded from future task assignment.
const DefaultBenchingThreshold = 3

// IsBenched returns true if the agent is in the benched state.
func (a *Agent) IsBenched() bool {
	return a.Status == AgentBenched
}

// ShouldBench returns true if any error category count has reached or exceeded
// the given threshold, indicating the agent should be benched.
func (a *Agent) ShouldBench(threshold int) bool {
	for _, count := range a.ErrorCounts {
		if count >= threshold {
			return true
		}
	}
	return false
}

// TotalErrorCount returns the sum of all error category counts.
func (a *Agent) TotalErrorCount() int {
	total := 0
	for _, count := range a.ErrorCounts {
		total += count
	}
	return total
}
