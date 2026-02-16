// Package constitution provides vocabulary predicates for constitution entities.
package constitution

// Vocabulary predicates for constitution entities.
// Uses three-part dotted notation: domain.category.property
const (
	// Identity predicates
	Project = "constitution.project.name" // project identifier
	Version = "constitution.version.number"

	// Section predicates
	Section = "constitution.section.name" // code_quality|testing|security|architecture

	// Rule predicates
	RuleID       = "constitution.rule.id"       // unique rule identifier
	RuleText     = "constitution.rule.text"     // rule text/description
	RuleEnforced = "constitution.rule.enforced" // bool - is this rule enforced?
	RulePriority = "constitution.rule.priority" // must|should|may

	// Standard metadata (Dublin Core aligned)
	DcTitle    = "dc.terms.title"
	DcCreated  = "dc.terms.created"
	DcModified = "dc.terms.modified"
)

// RulePriorityValue represents the enforcement priority of a rule
type RulePriorityValue string

const (
	PriorityMust   RulePriorityValue = "must"   // Required - violations block work
	PriorityShould RulePriorityValue = "should" // Recommended - violations warn
	PriorityMay    RulePriorityValue = "may"    // Optional - informational
)

// SectionName represents a constitution section category
type SectionName string

const (
	SectionCodeQuality  SectionName = "code_quality"
	SectionTesting      SectionName = "testing"
	SectionSecurity     SectionName = "security"
	SectionArchitecture SectionName = "architecture"
)
