package projectmanager

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
	"github.com/c360studio/semspec/workflow/harnesscatalog/qarender"
)

// defaultActJobImage is the act runner image injected into the `container:`
// block alongside any catalog-rendered `services:` block. nektos/act only
// attaches a job to its per-job service network when the job declares a
// `container:` — without it the job falls back to host networking and
// service-name DNS resolution fails. Verified empirically 2026-05-28 in
// ADR-039 Phase 1b. See [[act-dood-services-require-container-block]].
const defaultActJobImage = "catthehacker/ubuntu:act-latest"

// QAWorkflowDecision captures the pure decision DecideQAWorkflowInjection
// makes from selections + project config. Returned to callers so the wire-up
// site can log Reason for forensics and the unit tests can assert on a single
// struct rather than scraping byproducts.
//
// Inject=false means: caller should fall through to EnsureQAWorkflow (write
// the language-templated scaffold if no qa.yml exists, otherwise leave the
// operator's file alone).
//
// Inject=true means: caller must build the base workflow via BuildQAWorkflow,
// pass through InjectServicesIntoIntegrationJob with ServicesYAML and
// ContainerImage, and write the result, overwriting any existing qa.yml.
type QAWorkflowDecision struct {
	Inject         bool
	Reason         string
	ServicesYAML   string
	ContainerImage string
}

// DecideQAWorkflowInjection inspects resolved catalog selections and project
// config to decide whether plan-manager should inject a services-enriched
// qa.yml at QA dispatch. Pure: no filesystem, no graph, no network — the
// seam the wire-up site is expected to test against (see
// [[feedback_seam_coverage_pattern]]).
//
// Returns Inject=false with a Reason string for any case the renderer must
// not touch the workspace qa.yml: operator opt-out, no services-orchestrated
// selections, empty catalog render. The wire-up site logs Reason so a
// missing inject is debuggable post-mortem.
func DecideQAWorkflowInjection(selections []harnesscatalog.ResolvedSelection, pc *workflow.ProjectConfig) (QAWorkflowDecision, error) {
	if pc != nil && pc.QASkipServiceInjection {
		return QAWorkflowDecision{
			Inject: false,
			Reason: "operator opted out via qa_skip_service_injection",
		}, nil
	}

	yamlBlock, err := qarender.RenderYAML(selections, qarender.Options{})
	if err != nil {
		return QAWorkflowDecision{}, fmt.Errorf("render catalog services: %w", err)
	}
	if strings.TrimSpace(yamlBlock) == "" {
		return QAWorkflowDecision{
			Inject: false,
			Reason: "no services-orchestrated harness profiles selected",
		}, nil
	}

	return QAWorkflowDecision{
		Inject:         true,
		Reason:         "injecting catalog services into qa.yml integration job",
		ServicesYAML:   yamlBlock,
		ContainerImage: defaultActJobImage,
	}, nil
}

// InjectServicesIntoIntegrationJob parses a base qa.yml document, splices a
// `container:` block and the provided `services:` mapping into the
// `integration` job, and returns the re-emitted YAML. The base document is
// expected to come from BuildQAWorkflow — a top-level `jobs.integration:`
// mapping is required.
//
// servicesYAML is the output of qarender.RenderYAML: zero or more service
// mapping entries, NOT wrapped in a parent `services:` key. The function
// wraps them under the integration job's `services:` key on splice.
//
// containerImage is the image name written under `container:` for the
// integration job (typically defaultActJobImage). Empty falls through to
// defaultActJobImage so the act DooD constraint is satisfied by default.
func InjectServicesIntoIntegrationJob(baseYAML, servicesYAML, containerImage string) (string, error) {
	if strings.TrimSpace(baseYAML) == "" {
		return "", fmt.Errorf("baseYAML is empty")
	}
	if strings.TrimSpace(servicesYAML) == "" {
		return baseYAML, nil
	}
	if strings.TrimSpace(containerImage) == "" {
		containerImage = defaultActJobImage
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(baseYAML), &doc); err != nil {
		return "", fmt.Errorf("parse base qa.yml: %w", err)
	}

	servicesNode, err := parseServicesYAML(servicesYAML)
	if err != nil {
		return "", err
	}

	integrationJob, err := findIntegrationJob(&doc)
	if err != nil {
		return "", err
	}

	if err := ensureContainerBlock(integrationJob, containerImage); err != nil {
		return "", err
	}
	if err := ensureServicesBlock(integrationJob, servicesNode); err != nil {
		return "", err
	}

	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", fmt.Errorf("encode injected qa.yml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("close encoder: %w", err)
	}
	return buf.String(), nil
}

// findIntegrationJob walks the document tree to locate jobs.integration. The
// base templates BuildQAWorkflow emits always carry this key — anything else
// is a programming error and surfaces as a clear failure rather than silently
// landing the injection in the wrong place.
func findIntegrationJob(doc *yaml.Node) (*yaml.Node, error) {
	if doc == nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("base qa.yml is not a top-level mapping")
	}
	top := doc.Content[0]
	jobs := mappingChild(top, "jobs")
	if jobs == nil {
		return nil, fmt.Errorf("base qa.yml has no `jobs:` mapping")
	}
	integration := mappingChild(jobs, "integration")
	if integration == nil {
		return nil, fmt.Errorf("base qa.yml has no `jobs.integration:` mapping")
	}
	return integration, nil
}

// parseServicesYAML wraps the raw services block in a synthetic root so the
// yaml decoder produces a MappingNode we can splice. qarender.RenderYAML emits
// a sequence of `name:` keys without a parent — we add the parent here.
func parseServicesYAML(servicesYAML string) (*yaml.Node, error) {
	var holder yaml.Node
	wrapped := "root:\n" + indentBlock(servicesYAML, "  ")
	if err := yaml.Unmarshal([]byte(wrapped), &holder); err != nil {
		return nil, fmt.Errorf("parse services yaml: %w", err)
	}
	if len(holder.Content) == 0 || holder.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("services yaml is not a mapping")
	}
	root := mappingChild(holder.Content[0], "root")
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("services yaml did not parse to a mapping")
	}
	return root, nil
}

func ensureContainerBlock(job *yaml.Node, image string) error {
	if existing := mappingChild(job, "container"); existing != nil {
		return nil
	}
	containerNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	containerNode.Content = append(containerNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "image"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: image},
	)
	keyNode := &yaml.Node{
		Kind:        yaml.ScalarNode,
		Tag:         "!!str",
		Value:       "container",
		HeadComment: "ADR-039 Phase 1c: act DooD requires container: alongside services: so the\njob attaches to the per-job service network and resolves service-name DNS.",
	}
	job.Content = insertAfter(job.Content, "runs-on", keyNode, containerNode)
	return nil
}

func ensureServicesBlock(job *yaml.Node, services *yaml.Node) error {
	if existing := mappingChild(job, "services"); existing != nil {
		return fmt.Errorf("base qa.yml already declares jobs.integration.services; set qa_skip_service_injection to opt out of catalog injection")
	}
	keyNode := &yaml.Node{
		Kind:        yaml.ScalarNode,
		Tag:         "!!str",
		Value:       "services",
		HeadComment: "Rendered from architecture.harness_profiles[] (ADR-039 Phase 1c).",
	}
	job.Content = insertAfter(job.Content, "container", keyNode, services)
	return nil
}

// insertAfter walks key/value pairs of a mapping and inserts (key, value)
// immediately after the named key. If anchor is not present, the new pair is
// appended at the end — keeps the function total even when the base template
// shape drifts.
func insertAfter(content []*yaml.Node, anchor string, key, value *yaml.Node) []*yaml.Node {
	for i := 0; i+1 < len(content); i += 2 {
		if content[i].Kind == yaml.ScalarNode && content[i].Value == anchor {
			out := make([]*yaml.Node, 0, len(content)+2)
			out = append(out, content[:i+2]...)
			out = append(out, key, value)
			out = append(out, content[i+2:]...)
			return out
		}
	}
	return append(content, key, value)
}

func mappingChild(node *yaml.Node, name string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}

func indentBlock(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
