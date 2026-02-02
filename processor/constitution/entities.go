package constitution

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/message"
)

// Constitution represents a project's constitution with all its rules
type Constitution struct {
	// ID is the entity identifier
	// Format: {org}.semspec.config.constitution.{project}.{version}
	ID string

	// Project is the project this constitution applies to
	Project string

	// Version is the constitution version
	Version string

	// Sections contains rules organized by category
	Sections map[SectionName][]Rule

	// Timestamps
	CreatedAt  time.Time
	ModifiedAt time.Time
}

// Rule represents a single constitution rule
type Rule struct {
	// ID is the rule identifier within the section
	ID string

	// Text is the rule description
	Text string

	// Priority indicates enforcement level
	Priority RulePriorityValue

	// Enforced indicates if the rule is actively enforced
	Enforced bool
}

// NewConstitution creates a new constitution entity
func NewConstitution(org, project, version string) *Constitution {
	now := time.Now()
	return &Constitution{
		ID:         fmt.Sprintf("%s.semspec.config.constitution.%s.%s", org, project, version),
		Project:    project,
		Version:    version,
		Sections:   make(map[SectionName][]Rule),
		CreatedAt:  now,
		ModifiedAt: now,
	}
}

// AddRule adds a rule to the constitution under the specified section
func (c *Constitution) AddRule(section SectionName, rule Rule) {
	if c.Sections == nil {
		c.Sections = make(map[SectionName][]Rule)
	}
	c.Sections[section] = append(c.Sections[section], rule)
	c.ModifiedAt = time.Now()
}

// GetRules returns all rules for a given section
func (c *Constitution) GetRules(section SectionName) []Rule {
	return c.Sections[section]
}

// AllRules returns all rules across all sections
func (c *Constitution) AllRules() []Rule {
	var rules []Rule
	for _, sectionRules := range c.Sections {
		rules = append(rules, sectionRules...)
	}
	return rules
}

// EnforcedRules returns only rules that are actively enforced
func (c *Constitution) EnforcedRules() []Rule {
	var rules []Rule
	for _, sectionRules := range c.Sections {
		for _, rule := range sectionRules {
			if rule.Enforced {
				rules = append(rules, rule)
			}
		}
	}
	return rules
}

// Triples converts the Constitution to a slice of message.Triple for graph storage
func (c *Constitution) Triples() []message.Triple {
	triples := make([]message.Triple, 0, 20+len(c.AllRules())*4)

	// Constitution identity
	triples = append(triples,
		message.Triple{Subject: c.ID, Predicate: DcTitle, Object: fmt.Sprintf("Constitution: %s", c.Project)},
		message.Triple{Subject: c.ID, Predicate: Project, Object: c.Project},
		message.Triple{Subject: c.ID, Predicate: Version, Object: c.Version},
		message.Triple{Subject: c.ID, Predicate: DcCreated, Object: c.CreatedAt.Format(time.RFC3339)},
		message.Triple{Subject: c.ID, Predicate: DcModified, Object: c.ModifiedAt.Format(time.RFC3339)},
	)

	// Rules per section
	for sectionName, rules := range c.Sections {
		for i, rule := range rules {
			ruleID := rule.ID
			if ruleID == "" {
				ruleID = fmt.Sprintf("%s-%d", sectionName, i+1)
			}
			ruleSubject := fmt.Sprintf("%s.rule.%s", c.ID, ruleID)

			triples = append(triples,
				message.Triple{Subject: ruleSubject, Predicate: Section, Object: string(sectionName)},
				message.Triple{Subject: ruleSubject, Predicate: RuleID, Object: ruleID},
				message.Triple{Subject: ruleSubject, Predicate: RuleText, Object: rule.Text},
				message.Triple{Subject: ruleSubject, Predicate: RulePriority, Object: string(rule.Priority)},
				message.Triple{Subject: ruleSubject, Predicate: RuleEnforced, Object: rule.Enforced},
			)
		}
	}

	return triples
}

// CheckResult represents the result of checking content against the constitution
type CheckResult struct {
	// Passed indicates if all enforced rules passed
	Passed bool

	// Violations contains any rule violations found
	Violations []Violation

	// Warnings contains non-enforced rule issues
	Warnings []Violation

	// CheckedAt is when the check was performed
	CheckedAt time.Time
}

// Violation represents a constitution rule violation
type Violation struct {
	// Rule is the rule that was violated
	Rule Rule

	// Section is the section the rule belongs to
	Section SectionName

	// Message describes the violation
	Message string

	// Location is where the violation was found (optional)
	Location string
}

// NewCheckResult creates a new check result
func NewCheckResult() *CheckResult {
	return &CheckResult{
		Passed:    true,
		CheckedAt: time.Now(),
	}
}

// AddViolation adds a violation to the result
func (r *CheckResult) AddViolation(v Violation) {
	r.Passed = false
	r.Violations = append(r.Violations, v)
}

// AddWarning adds a warning to the result
func (r *CheckResult) AddWarning(v Violation) {
	r.Warnings = append(r.Warnings, v)
}
