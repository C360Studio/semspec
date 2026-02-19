package constitution

import (
	"encoding/json"
	"net/http"
	"strings"
)

// RegisterHTTPHandlers registers HTTP handlers for the constitution component.
// The prefix should include the trailing slash (e.g., "/api/constitution/").
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	mux.HandleFunc(prefix, c.handleGetConstitution)
	mux.HandleFunc(prefix+"rules", c.handleGetRules)
	mux.HandleFunc(prefix+"rules/", c.handleGetRulesBySection)
	mux.HandleFunc(prefix+"check", c.handleCheck)
	mux.HandleFunc(prefix+"reload", c.handleReload)
}

// Response is the JSON response for GET /
type Response struct {
	ID         string                 `json:"id"`
	Project    string                 `json:"project"`
	Version    string                 `json:"version"`
	Sections   map[SectionName][]Rule `json:"sections"`
	RuleCount  int                    `json:"rule_count"`
	CreatedAt  string                 `json:"created_at"`
	ModifiedAt string                 `json:"modified_at"`
}

// RulesResponse is the JSON response for GET /rules
type RulesResponse struct {
	Rules []RuleWithSection `json:"rules"`
	Count int               `json:"count"`
}

// RuleWithSection includes section info with the rule
type RuleWithSection struct {
	Section  SectionName `json:"section"`
	ID       string      `json:"id"`
	Text     string      `json:"text"`
	Priority string      `json:"priority"`
	Enforced bool        `json:"enforced"`
}

// SectionRulesResponse is the JSON response for GET /rules/{section}
type SectionRulesResponse struct {
	Section SectionName `json:"section"`
	Rules   []Rule      `json:"rules"`
	Count   int         `json:"count"`
}

// HTTPCheckRequest is the JSON request body for POST /check
type HTTPCheckRequest struct {
	Content string            `json:"content"`
	Context map[string]string `json:"context,omitempty"`
}

// HTTPCheckResponse is the JSON response for POST /check
type HTTPCheckResponse struct {
	Passed     bool        `json:"passed"`
	Violations []Violation `json:"violations,omitempty"`
	Warnings   []Violation `json:"warnings,omitempty"`
	CheckedAt  string      `json:"checked_at"`
}

// ReloadResponse is the JSON response for POST /reload
type ReloadResponse struct {
	Success   bool   `json:"success"`
	RuleCount int    `json:"rule_count"`
	Message   string `json:"message,omitempty"`
}

// handleGetConstitution handles GET / - returns the current constitution
func (c *Component) handleGetConstitution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c.mu.RLock()
	constitution := c.constitution
	c.mu.RUnlock()

	if constitution == nil {
		writeJSON(w, http.StatusOK, Response{})
		return
	}

	resp := Response{
		ID:         constitution.ID,
		Project:    constitution.Project,
		Version:    constitution.Version,
		Sections:   constitution.Sections,
		RuleCount:  len(constitution.AllRules()),
		CreatedAt:  constitution.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		ModifiedAt: constitution.ModifiedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleGetRules handles GET /rules - returns all rules across all sections
func (c *Component) handleGetRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c.mu.RLock()
	constitution := c.constitution
	c.mu.RUnlock()

	var rules []RuleWithSection
	if constitution != nil {
		for section, sectionRules := range constitution.Sections {
			for _, rule := range sectionRules {
				rules = append(rules, RuleWithSection{
					Section:  section,
					ID:       rule.ID,
					Text:     rule.Text,
					Priority: string(rule.Priority),
					Enforced: rule.Enforced,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, RulesResponse{
		Rules: rules,
		Count: len(rules),
	})
}

// handleGetRulesBySection handles GET /rules/{section}
func (c *Component) handleGetRulesBySection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract section from path (e.g., "/api/constitution/rules/testing" -> "testing")
	path := r.URL.Path
	idx := strings.LastIndex(path, "/rules/")
	if idx == -1 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sectionStr := path[idx+len("/rules/"):]
	if sectionStr == "" {
		http.Error(w, "Section name required", http.StatusBadRequest)
		return
	}

	section := SectionName(sectionStr)

	// Validate section name
	switch section {
	case SectionCodeQuality, SectionTesting, SectionSecurity, SectionArchitecture:
		// valid
	default:
		http.Error(w, "Invalid section: must be one of code_quality, testing, security, architecture", http.StatusBadRequest)
		return
	}

	c.mu.RLock()
	constitution := c.constitution
	c.mu.RUnlock()

	var rules []Rule
	if constitution != nil {
		rules = constitution.GetRules(section)
	}
	if rules == nil {
		rules = []Rule{}
	}

	writeJSON(w, http.StatusOK, SectionRulesResponse{
		Section: section,
		Rules:   rules,
		Count:   len(rules),
	})
}

// handleCheck handles POST /check - check content against the constitution
func (c *Component) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req HTTPCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON request body", http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		http.Error(w, "content field is required", http.StatusBadRequest)
		return
	}

	result := c.Check(req.Content, req.Context)

	resp := HTTPCheckResponse{
		Passed:     result.Passed,
		Violations: result.Violations,
		Warnings:   result.Warnings,
		CheckedAt:  result.CheckedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleReload handles POST /reload - reload constitution from file
func (c *Component) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if c.config.FilePath == "" {
		writeJSON(w, http.StatusOK, ReloadResponse{
			Success: false,
			Message: "No constitution file path configured",
		})
		return
	}

	if err := c.loadConstitutionFromFile(c.config.FilePath); err != nil {
		c.logger.Error("Failed to reload constitution", "error", err)
		writeJSON(w, http.StatusInternalServerError, ReloadResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	c.mu.RLock()
	ruleCount := len(c.constitution.AllRules())
	c.mu.RUnlock()

	writeJSON(w, http.StatusOK, ReloadResponse{
		Success:   true,
		RuleCount: ruleCount,
		Message:   "Constitution reloaded successfully",
	})
}

// writeJSON writes a JSON response with the given status code
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// Log but can't do much at this point
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}
