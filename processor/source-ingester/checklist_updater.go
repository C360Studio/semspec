package sourceingester

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/c360studio/semspec/workflow"
)

// ChecklistUpdater manages updates to checklist.json and project.json when
// stack detection discovers new languages, frameworks, or tooling after init.
//
// This mirrors the StandardsUpdater pattern: re-detection runs on every
// ingestion (cheap filesystem stat calls), and the add-only merge ensures
// user customizations are never overwritten.
//
// Thread-safe: multiple ingestions can trigger concurrent updates.
type ChecklistUpdater struct {
	checklistPath string
	projectPath   string
	repoRoot      string
	mu            sync.Mutex
}

// ChecklistUpdateResult describes what changed after an update attempt.
type ChecklistUpdateResult struct {
	// ChecksAdded is the number of new checks added to checklist.json.
	ChecksAdded int

	// NewCheckNames lists the names of newly added checks.
	NewCheckNames []string

	// LanguagesAdded lists newly detected languages added to project.json.
	LanguagesAdded []string

	// FrameworksAdded lists newly detected frameworks added to project.json.
	FrameworksAdded []string
}

// NewChecklistUpdater creates a checklist updater for the given paths.
func NewChecklistUpdater(semspecDir, repoRoot string) *ChecklistUpdater {
	return &ChecklistUpdater{
		checklistPath: filepath.Join(semspecDir, workflow.ChecklistFile),
		projectPath:   filepath.Join(semspecDir, workflow.ProjectConfigFile),
		repoRoot:      repoRoot,
	}
}

// UpdateFromDetection runs stack detection on the repo root, merges any new
// checks into checklist.json, and updates project.json with new languages.
//
// Returns a result describing what changed. If checklist.json doesn't exist
// (project not yet initialized), this is a no-op.
func (u *ChecklistUpdater) UpdateFromDetection() (*ChecklistUpdateResult, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	result := &ChecklistUpdateResult{}

	// Skip if project not yet initialized (no checklist.json)
	if _, err := os.Stat(u.checklistPath); os.IsNotExist(err) {
		return result, nil
	}

	// Run fresh detection
	detector := workflow.NewFileSystemDetector()
	detection, err := detector.Detect(u.repoRoot)
	if err != nil {
		return nil, fmt.Errorf("detect stack: %w", err)
	}

	// Update checklist.json with any new checks
	checksAdded, newNames, err := u.updateChecklist(detection)
	if err != nil {
		return nil, fmt.Errorf("update checklist: %w", err)
	}
	result.ChecksAdded = checksAdded
	result.NewCheckNames = newNames

	// Update project.json with any new languages/frameworks
	langsAdded, fwsAdded, err := u.updateProjectConfig(detection)
	if err != nil {
		return nil, fmt.Errorf("update project config: %w", err)
	}
	result.LanguagesAdded = langsAdded
	result.FrameworksAdded = fwsAdded

	return result, nil
}

// updateChecklist reads checklist.json, merges new checks, and writes back.
// Returns the number of checks added and their names.
func (u *ChecklistUpdater) updateChecklist(detection *workflow.DetectionResult) (int, []string, error) {
	checklist, err := u.readChecklist()
	if err != nil {
		return 0, nil, fmt.Errorf("read checklist: %w", err)
	}

	merged, changed := mergeChecks(checklist.Checks, detection.ProposedChecklist)
	if !changed {
		return 0, nil, nil
	}

	// Identify new check names
	existingNames := make(map[string]bool, len(checklist.Checks))
	for _, ch := range checklist.Checks {
		existingNames[ch.Name] = true
	}
	var newNames []string
	for _, ch := range merged {
		if !existingNames[ch.Name] {
			newNames = append(newNames, ch.Name)
		}
	}

	checklist.Checks = merged
	if err := u.writeChecklist(checklist); err != nil {
		return 0, nil, fmt.Errorf("write checklist: %w", err)
	}

	return len(newNames), newNames, nil
}

// updateProjectConfig reads project.json, adds new languages/frameworks, writes back.
// Returns lists of added languages and frameworks.
func (u *ChecklistUpdater) updateProjectConfig(detection *workflow.DetectionResult) ([]string, []string, error) {
	// Skip if project.json doesn't exist
	if _, err := os.Stat(u.projectPath); os.IsNotExist(err) {
		return nil, nil, nil
	}

	config, err := u.readProjectConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("read project config: %w", err)
	}

	// Add-only merge for languages
	existingLangs := make(map[string]bool, len(config.Languages))
	for _, lang := range config.Languages {
		existingLangs[lang.Name] = true
	}

	var langsAdded []string
	for _, detected := range detection.Languages {
		if !existingLangs[detected.Name] {
			config.Languages = append(config.Languages, workflow.LanguageInfo{
				Name:    detected.Name,
				Version: detected.Version,
				Primary: false, // Never override existing primary
			})
			langsAdded = append(langsAdded, detected.Name)
		}
	}

	// Add-only merge for frameworks
	existingFWs := make(map[string]bool, len(config.Frameworks))
	for _, fw := range config.Frameworks {
		existingFWs[fw.Name] = true
	}

	var fwsAdded []string
	for _, detected := range detection.Frameworks {
		if !existingFWs[detected.Name] {
			config.Frameworks = append(config.Frameworks, workflow.FrameworkInfo{
				Name:     detected.Name,
				Language: detected.Language,
			})
			fwsAdded = append(fwsAdded, detected.Name)
		}
	}

	// Only write if something changed
	if len(langsAdded) == 0 && len(fwsAdded) == 0 {
		return nil, nil, nil
	}

	if err := u.writeProjectConfig(config); err != nil {
		return nil, nil, fmt.Errorf("write project config: %w", err)
	}

	return langsAdded, fwsAdded, nil
}

// mergeChecks combines existing checks with proposed new checks.
// Existing checks are never modified or removed — only new checks (by Name)
// are appended. This preserves user customizations to commands, timeouts, etc.
func mergeChecks(existing, proposed []workflow.Check) ([]workflow.Check, bool) {
	existingNames := make(map[string]bool, len(existing))
	for _, ch := range existing {
		existingNames[ch.Name] = true
	}

	// Collect new checks
	var newChecks []workflow.Check
	for _, ch := range proposed {
		if !existingNames[ch.Name] {
			// Normalise defaults for new checks
			if ch.WorkingDir == "" {
				ch.WorkingDir = "."
			}
			if ch.Timeout == "" {
				ch.Timeout = "120s"
			}
			if ch.Trigger == nil {
				ch.Trigger = []string{}
			}
			newChecks = append(newChecks, ch)
		}
	}

	if len(newChecks) == 0 {
		return existing, false
	}

	// Append new checks after existing ones
	merged := make([]workflow.Check, len(existing), len(existing)+len(newChecks))
	copy(merged, existing)
	merged = append(merged, newChecks...)
	return merged, true
}

// readChecklist reads and parses checklist.json.
func (u *ChecklistUpdater) readChecklist() (*workflow.Checklist, error) {
	data, err := os.ReadFile(u.checklistPath)
	if err != nil {
		return nil, err
	}
	var checklist workflow.Checklist
	if err := json.Unmarshal(data, &checklist); err != nil {
		return nil, fmt.Errorf("parse checklist JSON: %w", err)
	}
	return &checklist, nil
}

// writeChecklist writes checklist.json to disk.
func (u *ChecklistUpdater) writeChecklist(checklist *workflow.Checklist) error {
	data, err := json.MarshalIndent(checklist, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checklist: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(u.checklistPath), 0755); err != nil {
		return fmt.Errorf("create checklist directory: %w", err)
	}
	return os.WriteFile(u.checklistPath, data, 0644)
}

// readProjectConfig reads and parses project.json.
func (u *ChecklistUpdater) readProjectConfig() (*workflow.ProjectConfig, error) {
	data, err := os.ReadFile(u.projectPath)
	if err != nil {
		return nil, err
	}
	var config workflow.ProjectConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse project config JSON: %w", err)
	}
	return &config, nil
}

// writeProjectConfig writes project.json to disk.
func (u *ChecklistUpdater) writeProjectConfig(config *workflow.ProjectConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}
	return os.WriteFile(u.projectPath, data, 0644)
}
