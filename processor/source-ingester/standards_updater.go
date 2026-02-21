package sourceingester

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// StandardsUpdater manages updates to standards.json when SOPs are ingested.
// It reads the existing standards, merges new rules from SOP requirements,
// and writes the result back. This is the critical link between SOP ingestion
// and the context-builder's standards preamble injection.
//
// Thread-safe: multiple ingestions can trigger concurrent updates.
type StandardsUpdater struct {
	standardsPath string
	mu            sync.Mutex
}

// NewStandardsUpdater creates a standards updater for the given .semspec directory.
func NewStandardsUpdater(semspecDir string) *StandardsUpdater {
	return &StandardsUpdater{
		standardsPath: filepath.Join(semspecDir, workflow.StandardsFile),
	}
}

// SOPMetadata is the subset of SOP analysis relevant to standards generation.
type SOPMetadata struct {
	Filename     string
	Category     string
	Severity     string
	AppliesTo    []string
	Requirements []string
}

// UpdateFromSOP reads the current standards.json, adds rules derived from
// the SOP's extracted requirements, deduplicates by rule ID, and writes back.
//
// If standards.json does not exist, it creates one with default version.
// This is idempotent — re-ingesting the same SOP produces the same rules.
func (u *StandardsUpdater) UpdateFromSOP(meta *SOPMetadata) error {
	if meta == nil || meta.Category != "sop" {
		return nil // Not an SOP, nothing to update
	}
	if len(meta.Requirements) == 0 {
		return nil // No requirements to add
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	// Read existing standards
	standards, err := u.readStandards()
	if err != nil {
		// Create new standards if file doesn't exist
		if os.IsNotExist(err) {
			standards = &workflow.Standards{
				Version: "1.0.0",
			}
		} else {
			return fmt.Errorf("read standards: %w", err)
		}
	}

	// Build new rules from SOP requirements
	newRules := sopToRules(meta)

	// Merge: add new rules, replace existing ones with same ID
	standards.Rules = mergeRules(standards.Rules, newRules)

	// Update metadata
	standards.GeneratedAt = time.Now()
	standards.TokenEstimate = estimateRuleTokens(standards.Rules)

	// Write back
	if err := u.writeStandards(standards); err != nil {
		return fmt.Errorf("write standards: %w", err)
	}

	return nil
}

// sopToRules converts SOP metadata into workflow.Rule entries.
// Each requirement becomes a rule with origin tracking back to the SOP file.
func sopToRules(meta *SOPMetadata) []workflow.Rule {
	origin := workflow.RuleOriginSOP(meta.Filename)
	severity := mapSeverity(meta.Severity)

	rules := make([]workflow.Rule, 0, len(meta.Requirements))
	for i, req := range meta.Requirements {
		// Generate stable rule ID from SOP filename + requirement index
		ruleID := fmt.Sprintf("sop-%s-%d", sanitizeForID(meta.Filename), i+1)

		rules = append(rules, workflow.Rule{
			ID:        ruleID,
			Text:      req,
			Severity:  severity,
			Category:  "sop",
			AppliesTo: meta.AppliesTo,
			Origin:    origin,
		})
	}

	return rules
}

// mergeRules combines existing rules with new rules, deduplicating by ID.
// New rules with matching IDs replace existing ones (re-ingestion updates).
func mergeRules(existing, incoming []workflow.Rule) []workflow.Rule {
	// Build map of existing rules by ID
	ruleMap := make(map[string]workflow.Rule, len(existing))
	order := make([]string, 0, len(existing))

	for _, rule := range existing {
		if _, seen := ruleMap[rule.ID]; !seen {
			order = append(order, rule.ID)
		}
		ruleMap[rule.ID] = rule
	}

	// Add/replace with incoming rules
	for _, rule := range incoming {
		if _, seen := ruleMap[rule.ID]; !seen {
			order = append(order, rule.ID)
		}
		ruleMap[rule.ID] = rule
	}

	// Rebuild slice preserving order
	result := make([]workflow.Rule, 0, len(order))
	for _, id := range order {
		result = append(result, ruleMap[id])
	}

	return result
}

// mapSeverity converts SOP severity string to workflow.RuleSeverity.
func mapSeverity(severity string) workflow.RuleSeverity {
	switch strings.ToLower(severity) {
	case "error":
		return workflow.RuleSeverityError
	case "warning":
		return workflow.RuleSeverityWarning
	case "info":
		return workflow.RuleSeverityInfo
	default:
		return workflow.RuleSeverityWarning // Default for SOPs
	}
}

// sanitizeForID produces a safe identifier fragment from a filename.
func sanitizeForID(filename string) string {
	// Remove extension
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	// Replace non-alphanumeric with hyphens
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}
	return strings.ToLower(sb.String())
}

// estimateRuleTokens provides a rough token estimate for a set of rules.
func estimateRuleTokens(rules []workflow.Rule) int {
	total := 0
	for _, rule := range rules {
		// Rough: 4 chars per token for text + overhead for severity/category/ID
		total += (len(rule.Text) + len(rule.ID) + 20) / 4
	}
	return total
}

// readStandards reads and parses the standards.json file.
func (u *StandardsUpdater) readStandards() (*workflow.Standards, error) {
	data, err := os.ReadFile(u.standardsPath)
	if err != nil {
		return nil, err
	}

	var standards workflow.Standards
	if err := json.Unmarshal(data, &standards); err != nil {
		return nil, fmt.Errorf("parse standards JSON: %w", err)
	}

	return &standards, nil
}

// writeStandards writes the standards to disk as formatted JSON.
func (u *StandardsUpdater) writeStandards(standards *workflow.Standards) error {
	data, err := json.MarshalIndent(standards, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal standards: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(u.standardsPath), 0755); err != nil {
		return fmt.Errorf("create standards directory: %w", err)
	}

	return os.WriteFile(u.standardsPath, data, 0644)
}
