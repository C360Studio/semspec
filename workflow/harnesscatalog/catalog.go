// Package harnesscatalog loads system-owned test harness profile catalogs.
package harnesscatalog

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/c360studio/semspec/workflow"
)

//go:embed catalog/*.yaml
var builtInCatalogFS embed.FS

const (
	// TierRequired profiles are hard-gated by structural validation when
	// selected: modified test files must contain the profile evidence anchors.
	TierRequired = "required"
	// TierCompatibility profiles are visible to the architect/developer but do
	// not hard-fail validation unless promoted to a required catalog entry.
	TierCompatibility = "compatibility"
	// TierHeavy profiles need expensive or peripheral infrastructure and are not
	// required by default.
	TierHeavy = "heavy"
)

const (
	// OrchestrationServices profiles declare an invariant integration stack that
	// semspec renders into qa.yml services from images/ports/env/readiness for
	// the operator's CI to bring up.
	OrchestrationServices = "services"
	// OrchestrationTestcontainers profiles need per-test or dynamic stack
	// orchestration the dev agent expresses with testcontainers-go (or an
	// equivalent in-test fixture). The qa renderer emits no services for these.
	OrchestrationTestcontainers = "testcontainers"
	// OrchestrationPureFixture profiles need no container — captured frames,
	// in-process peers, or pure-unit fixtures. The qa renderer emits no
	// services for these.
	OrchestrationPureFixture = "pure-fixture"
)

// Catalog is the merged set of built-in and workspace harness profiles.
type Catalog struct {
	Profiles map[string]Profile
}

// Profile describes one system-owned test harness option.
type Profile struct {
	ID                 string              `yaml:"id" json:"id"`
	Domain             string              `yaml:"domain" json:"domain"`
	Tier               string              `yaml:"tier" json:"tier"`
	Summary            string              `yaml:"summary" json:"summary"`
	Proves             []string            `yaml:"proves" json:"proves"`
	Covers             map[string][]string `yaml:"covers" json:"covers"`
	RunnerSupport      []string            `yaml:"runner_support" json:"runner_support"`
	Cost               string              `yaml:"cost" json:"cost"`
	Constraints        []string            `yaml:"constraints" json:"constraints"`
	RequiredAssertions []string            `yaml:"required_assertions" json:"required_assertions"`
	EvidenceAnchors    []string            `yaml:"evidence_anchors" json:"evidence_anchors"`
	Images             []ImageRef          `yaml:"images" json:"images,omitempty"`
	Ports              []PortRef           `yaml:"ports" json:"ports,omitempty"`
	Env                map[string]string   `yaml:"env" json:"env,omitempty"`
	Readiness          []string            `yaml:"readiness" json:"readiness,omitempty"`
	TestGuidance       []string            `yaml:"test_guidance" json:"test_guidance,omitempty"`
	// Orchestration declares how the operator's CI (via the emitted qa.yml)
	// brings up this profile's integration stack. Empty defaults to
	// OrchestrationServices when Images is non-empty and OrchestrationPureFixture
	// otherwise; callers should read EffectiveOrchestration() rather than this
	// raw field.
	Orchestration string `yaml:"orchestration" json:"orchestration,omitempty"`
}

// EffectiveOrchestration returns Orchestration if set, otherwise the inferred
// default: services when the profile declares images, pure-fixture otherwise.
func (p Profile) EffectiveOrchestration() string {
	if o := strings.TrimSpace(p.Orchestration); o != "" {
		return o
	}
	if len(p.Images) > 0 {
		return OrchestrationServices
	}
	return OrchestrationPureFixture
}

// ImageRef is a container image a profile expects tests to start.
type ImageRef struct {
	Name    string `yaml:"name" json:"name"`
	Purpose string `yaml:"purpose" json:"purpose,omitempty"`
}

// PortRef is a container port or protocol endpoint a profile exposes.
type PortRef struct {
	Name          string `yaml:"name" json:"name"`
	ContainerPort int    `yaml:"container_port" json:"container_port"`
	Protocol      string `yaml:"protocol" json:"protocol"`
	Purpose       string `yaml:"purpose" json:"purpose,omitempty"`
}

type catalogFile struct {
	Profiles []Profile `yaml:"profiles"`
}

// LoadBuiltIn returns the embedded catalog without workspace overrides.
func LoadBuiltIn() (*Catalog, error) {
	c := &Catalog{Profiles: map[string]Profile{}}
	if err := loadEmbedded(c); err != nil {
		return nil, err
	}
	return c, nil
}

// Load returns built-in profiles merged with workspace overrides from
// .semspec/harness-catalog/*.yaml. If repoRoot is empty, SEMSPEC_REPO_PATH then
// the current working directory are used. A workspace profile with the same ID
// as a built-in replaces the built-in; duplicate IDs within built-ins or within
// workspace override files are rejected.
func Load(repoRoot string) (*Catalog, error) {
	c, err := LoadBuiltIn()
	if err != nil {
		return nil, err
	}
	root := resolveRepoRoot(repoRoot)
	if root == "" {
		return c, nil
	}
	if err := loadWorkspaceOverrides(c, root); err != nil {
		return nil, err
	}
	return c, nil
}

// ResolveSelections validates and resolves profile selections against the
// catalog. Duplicate selected profile IDs are rejected to keep downstream
// evidence diagnostics unambiguous.
func (c *Catalog) ResolveSelections(selections []workflow.HarnessProfileSelection) ([]ResolvedSelection, error) {
	if c == nil {
		return nil, errors.New("harness catalog is nil")
	}
	seen := map[string]struct{}{}
	out := make([]ResolvedSelection, 0, len(selections))
	for i, s := range selections {
		id := strings.TrimSpace(s.ProfileID)
		if id == "" {
			return nil, fmt.Errorf("harness_profiles[%d].profile_id is required", i)
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("duplicate harness profile selection %q", id)
		}
		seen[id] = struct{}{}
		p, ok := c.Profiles[id]
		if !ok {
			return nil, fmt.Errorf("unknown harness profile %q", id)
		}
		out = append(out, ResolvedSelection{Selection: s, Profile: p})
	}
	return out, nil
}

// ValidateSelections reports whether all selections resolve to catalog entries.
func (c *Catalog) ValidateSelections(selections []workflow.HarnessProfileSelection) error {
	_, err := c.ResolveSelections(selections)
	return err
}

// RequiredProfiles returns resolved selections whose catalog tier is required.
func (c *Catalog) RequiredProfiles(selections []workflow.HarnessProfileSelection) ([]ResolvedSelection, error) {
	resolved, err := c.ResolveSelections(selections)
	if err != nil {
		return nil, err
	}
	var required []ResolvedSelection
	for _, r := range resolved {
		if r.Profile.Tier == TierRequired {
			required = append(required, r)
		}
	}
	return required, nil
}

// ValidIDsSorted returns the catalog's profile IDs in deterministic order. Used
// to build a precise "select only from: …" hint when a selection fails to
// resolve, so an architecture-generation retry sees the valid options inline.
func (c *Catalog) ValidIDsSorted() []string {
	if c == nil {
		return nil
	}
	ids := make([]string, 0, len(c.Profiles))
	for id := range c.Profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ProfilesSorted returns catalog profiles ordered by ID for deterministic
// prompt rendering and tests.
func (c *Catalog) ProfilesSorted() []Profile {
	if c == nil {
		return nil
	}
	ids := make([]string, 0, len(c.Profiles))
	for id := range c.Profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Profile, 0, len(ids))
	for _, id := range ids {
		out = append(out, c.Profiles[id])
	}
	return out
}

// ResolvedSelection pairs an architecture selection with its catalog profile.
type ResolvedSelection struct {
	Selection workflow.HarnessProfileSelection
	Profile   Profile
}

func loadEmbedded(c *Catalog) error {
	entries, err := builtInCatalogFS.ReadDir("catalog")
	if err != nil {
		return fmt.Errorf("read embedded harness catalog: %w", err)
	}
	seen := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		name := filepath.Join("catalog", entry.Name())
		data, err := builtInCatalogFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read embedded harness catalog %s: %w", name, err)
		}
		if err := mergeCatalogFile(c, data, name, seen, false); err != nil {
			return err
		}
	}
	return nil
}

func loadWorkspaceOverrides(c *Catalog, repoRoot string) error {
	dir := filepath.Join(repoRoot, ".semspec", "harness-catalog")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read workspace harness catalog %s: %w", dir, err)
	}
	seen := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !(strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read workspace harness catalog %s: %w", path, err)
		}
		if err := mergeCatalogFile(c, data, path, seen, true); err != nil {
			return err
		}
	}
	return nil
}

func mergeCatalogFile(c *Catalog, data []byte, source string, layerSeen map[string]string, override bool) error {
	var file catalogFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse harness catalog %s: %w", source, err)
	}
	if len(file.Profiles) == 0 {
		return fmt.Errorf("harness catalog %s has no profiles", source)
	}
	for i, profile := range file.Profiles {
		if err := validateProfile(profile); err != nil {
			return fmt.Errorf("harness catalog %s profile[%d]: %w", source, i, err)
		}
		if prev, ok := layerSeen[profile.ID]; ok {
			return fmt.Errorf("duplicate harness profile ID %q in %s and %s", profile.ID, prev, source)
		}
		layerSeen[profile.ID] = source
		if _, exists := c.Profiles[profile.ID]; exists && !override {
			return fmt.Errorf("duplicate built-in harness profile ID %q", profile.ID)
		}
		c.Profiles[profile.ID] = profile
	}
	return nil
}

func validateProfile(p Profile) error {
	switch {
	case strings.TrimSpace(p.ID) == "":
		return errors.New("id is required")
	case strings.TrimSpace(p.Domain) == "":
		return fmt.Errorf("profile %q domain is required", p.ID)
	case strings.TrimSpace(p.Summary) == "":
		return fmt.Errorf("profile %q summary is required", p.ID)
	case strings.TrimSpace(p.Cost) == "":
		return fmt.Errorf("profile %q cost is required", p.ID)
	case !validTier(p.Tier):
		return fmt.Errorf("profile %q has malformed tier %q", p.ID, p.Tier)
	case !validOrchestration(p.Orchestration):
		return fmt.Errorf("profile %q has malformed orchestration %q", p.ID, p.Orchestration)
	case p.Orchestration == OrchestrationServices && len(p.Images) == 0:
		return fmt.Errorf("profile %q orchestration is services but images is empty", p.ID)
	case len(p.Proves) == 0:
		return fmt.Errorf("profile %q proves is required", p.ID)
	case len(p.Covers) == 0:
		return fmt.Errorf("profile %q covers is required", p.ID)
	case len(p.RunnerSupport) == 0:
		return fmt.Errorf("profile %q runner_support is required", p.ID)
	case len(p.RequiredAssertions) == 0:
		return fmt.Errorf("profile %q required_assertions is required", p.ID)
	case len(p.EvidenceAnchors) == 0:
		return fmt.Errorf("profile %q evidence_anchors is required", p.ID)
	}
	if err := validateNonEmptyList(p.ID, "proves", p.Proves); err != nil {
		return err
	}
	if err := validateNonEmptyList(p.ID, "runner_support", p.RunnerSupport); err != nil {
		return err
	}
	if err := validateNonEmptyList(p.ID, "required_assertions", p.RequiredAssertions); err != nil {
		return err
	}
	if err := validateNonEmptyList(p.ID, "evidence_anchors", p.EvidenceAnchors); err != nil {
		return err
	}
	for k, vals := range p.Covers {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("profile %q covers contains an empty key", p.ID)
		}
		if len(vals) == 0 {
			return fmt.Errorf("profile %q covers[%q] is empty", p.ID, k)
		}
		if err := validateNonEmptyList(p.ID, "covers."+k, vals); err != nil {
			return err
		}
	}
	for i, image := range p.Images {
		if strings.TrimSpace(image.Name) == "" {
			return fmt.Errorf("profile %q images[%d].name is required", p.ID, i)
		}
	}
	for i, port := range p.Ports {
		if strings.TrimSpace(port.Name) == "" {
			return fmt.Errorf("profile %q ports[%d].name is required", p.ID, i)
		}
		if port.ContainerPort <= 0 {
			return fmt.Errorf("profile %q ports[%d].container_port must be positive", p.ID, i)
		}
		if strings.TrimSpace(port.Protocol) == "" {
			return fmt.Errorf("profile %q ports[%d].protocol is required", p.ID, i)
		}
	}
	return nil
}

func validateNonEmptyList(profileID, field string, vals []string) error {
	for i, v := range vals {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("profile %q %s[%d] is empty", profileID, field, i)
		}
	}
	return nil
}

func validTier(tier string) bool {
	switch tier {
	case TierRequired, TierCompatibility, TierHeavy:
		return true
	default:
		return false
	}
}

// validOrchestration accepts the empty string (caller falls back to inference)
// or any of the declared orchestration constants.
func validOrchestration(o string) bool {
	switch strings.TrimSpace(o) {
	case "", OrchestrationServices, OrchestrationTestcontainers, OrchestrationPureFixture:
		return true
	default:
		return false
	}
}

func resolveRepoRoot(repoRoot string) string {
	if repoRoot != "" {
		return repoRoot
	}
	if env := os.Getenv("SEMSPEC_REPO_PATH"); env != "" {
		return env
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}
