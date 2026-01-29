package constitution

import (
	"strings"
	"testing"
)

func TestNewConstitution(t *testing.T) {
	c := NewConstitution("acme", "myproject", "v1")

	if c.Project != "myproject" {
		t.Errorf("Project = %q, want %q", c.Project, "myproject")
	}
	if c.Version != "v1" {
		t.Errorf("Version = %q, want %q", c.Version, "v1")
	}
	if !strings.HasPrefix(c.ID, "acme.semspec.config.constitution.myproject.v1") {
		t.Errorf("ID = %q, want prefix 'acme.semspec.config.constitution.myproject.v1'", c.ID)
	}
	if c.Sections == nil {
		t.Error("Sections is nil")
	}
}

func TestConstitution_AddRule(t *testing.T) {
	c := NewConstitution("acme", "test", "v1")

	c.AddRule(SectionCodeQuality, Rule{
		ID:       "cq-1",
		Text:     "All functions must have doc comments",
		Priority: PriorityMust,
		Enforced: true,
	})

	rules := c.GetRules(SectionCodeQuality)
	if len(rules) != 1 {
		t.Fatalf("GetRules returned %d rules, want 1", len(rules))
	}

	if rules[0].ID != "cq-1" {
		t.Errorf("Rule ID = %q, want %q", rules[0].ID, "cq-1")
	}
	if rules[0].Priority != PriorityMust {
		t.Errorf("Rule Priority = %q, want %q", rules[0].Priority, PriorityMust)
	}
}

func TestConstitution_AllRules(t *testing.T) {
	c := NewConstitution("acme", "test", "v1")

	c.AddRule(SectionCodeQuality, Rule{ID: "cq-1", Text: "Rule 1"})
	c.AddRule(SectionTesting, Rule{ID: "t-1", Text: "Rule 2"})
	c.AddRule(SectionSecurity, Rule{ID: "s-1", Text: "Rule 3"})

	rules := c.AllRules()
	if len(rules) != 3 {
		t.Errorf("AllRules returned %d rules, want 3", len(rules))
	}
}

func TestConstitution_EnforcedRules(t *testing.T) {
	c := NewConstitution("acme", "test", "v1")

	c.AddRule(SectionCodeQuality, Rule{ID: "cq-1", Text: "Enforced", Enforced: true})
	c.AddRule(SectionCodeQuality, Rule{ID: "cq-2", Text: "Not enforced", Enforced: false})
	c.AddRule(SectionTesting, Rule{ID: "t-1", Text: "Also enforced", Enforced: true})

	rules := c.EnforcedRules()
	if len(rules) != 2 {
		t.Errorf("EnforcedRules returned %d rules, want 2", len(rules))
	}
}

func TestConstitution_Triples(t *testing.T) {
	c := NewConstitution("acme", "test", "v1")
	c.AddRule(SectionCodeQuality, Rule{
		ID:       "cq-1",
		Text:     "Test rule",
		Priority: PriorityShould,
		Enforced: true,
	})

	triples := c.Triples()

	// Should have constitution identity triples + rule triples
	if len(triples) < 5 {
		t.Errorf("Triples returned %d triples, want at least 5", len(triples))
	}

	// Check for project predicate
	hasProject := false
	for _, triple := range triples {
		if triple.Predicate == Project && triple.Object == "test" {
			hasProject = true
			break
		}
	}
	if !hasProject {
		t.Error("Missing project predicate in triples")
	}
}

func TestCheckResult(t *testing.T) {
	result := NewCheckResult()

	if !result.Passed {
		t.Error("New CheckResult should have Passed = true")
	}

	result.AddViolation(Violation{
		Rule:    Rule{ID: "r-1"},
		Section: SectionSecurity,
		Message: "Violation found",
	})

	if result.Passed {
		t.Error("CheckResult should have Passed = false after AddViolation")
	}
	if len(result.Violations) != 1 {
		t.Errorf("Violations count = %d, want 1", len(result.Violations))
	}

	result.AddWarning(Violation{
		Rule:    Rule{ID: "r-2"},
		Section: SectionCodeQuality,
		Message: "Warning found",
	})

	if len(result.Warnings) != 1 {
		t.Errorf("Warnings count = %d, want 1", len(result.Warnings))
	}
}
