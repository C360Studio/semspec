// Package qarender renders qa.yml GitHub-Actions-compatible `services:` blocks
// from harness catalog selections.
//
// Phase 1a (ADR-039): pure transformation. Render takes a slice of resolved
// catalog selections and returns a yaml.Node that callers can splice into a
// larger qa.yml document. The renderer reads catalog metadata only (no docker,
// no plan state, no filesystem). Profiles whose Orchestration is anything other
// than `services` are intentionally skipped — testcontainers and pure-fixture
// profiles do not get qa-runner service blocks; their integration concerns live
// in agent test code (testcontainers) or in-process fixtures (pure-fixture).
package qarender

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// Options tunes rendering. Zero-value Options renders container ports
// without host-port binding (GHA assigns a random host port at job time).
type Options struct {
	// PortOffset, when non-zero, is added to each profile's container_port to
	// derive an explicit host port. Used by callers (e.g. plan-manager) to
	// avoid host-port collisions when multiple plans run in parallel against
	// the same profile. The container-internal port is preserved unchanged so
	// tests can rely on canonical port constants.
	PortOffset int
}

// Render returns a yaml.Node holding the GHA `services:` mapping derived from
// selections. The returned node is a MappingNode whose keys are service names
// and whose values are MappingNodes with image/env/ports/options as required by
// nektos/act and GitHub Actions. An empty selection list (or one with no
// services-orchestrated entries) yields an empty MappingNode rather than nil so
// callers can splice the result unconditionally.
func Render(selections []harnesscatalog.ResolvedSelection, opts Options) (*yaml.Node, error) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if len(selections) == 0 {
		return root, nil
	}
	seenServiceNames := map[string]string{}
	for i, sel := range selections {
		if sel.Profile.EffectiveOrchestration() != harnesscatalog.OrchestrationServices {
			continue
		}
		if len(sel.Profile.Images) == 0 {
			return nil, fmt.Errorf("qarender: profile %q declares orchestration=services but has no images", sel.Profile.ID)
		}
		name := ServiceName(sel.Profile.ID)
		if prev, ok := seenServiceNames[name]; ok {
			return nil, fmt.Errorf("qarender: profiles %q and %q both render to service name %q", prev, sel.Profile.ID, name)
		}
		seenServiceNames[name] = sel.Profile.ID

		serviceNode, err := renderService(sel.Profile, opts)
		if err != nil {
			return nil, fmt.Errorf("qarender: render profile[%d] %q: %w", i, sel.Profile.ID, err)
		}
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
		keyNode.HeadComment = profileHeadComment(sel.Profile)
		root.Content = append(root.Content, keyNode, serviceNode)
	}
	return root, nil
}

// RenderYAML marshals Render's output into a YAML string suitable for splicing
// directly under a `services:` key in qa.yml. The returned string never has a
// leading `services:` prefix; callers control where to splice it.
func RenderYAML(selections []harnesscatalog.ResolvedSelection, opts Options) (string, error) {
	node, err := Render(selections, opts)
	if err != nil {
		return "", err
	}
	if len(node.Content) == 0 {
		return "", nil
	}
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return "", fmt.Errorf("qarender: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("qarender: close encoder: %w", err)
	}
	return buf.String(), nil
}

// ServiceName converts a catalog profile ID into a GHA-valid service name by
// replacing dots with hyphens. GHA service names must match [a-zA-Z0-9_-]+ and
// are used as DNS hostnames inside the job network, so dots are not allowed.
func ServiceName(profileID string) string {
	return strings.ReplaceAll(profileID, ".", "-")
}

func profileHeadComment(p harnesscatalog.Profile) string {
	var b strings.Builder
	b.WriteString("Profile: ")
	b.WriteString(strings.TrimSpace(p.ID))
	if len(p.Readiness) > 0 {
		b.WriteString("\nReadiness (operator must enforce in qa-runner; not emitted as docker healthcheck):")
		for _, r := range p.Readiness {
			b.WriteString("\n  - ")
			b.WriteString(strings.TrimSpace(r))
		}
	}
	return b.String()
}

func renderService(p harnesscatalog.Profile, opts Options) (*yaml.Node, error) {
	svc := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	appendScalarKV(svc, "image", p.Images[0].Name)

	if len(p.Env) > 0 {
		envNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := make([]string, 0, len(p.Env))
		for k := range p.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			envNode.Content = append(envNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p.Env[k]},
			)
		}
		svc.Content = append(svc.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "env"},
			envNode,
		)
	}

	if len(p.Ports) > 0 {
		portsNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, port := range p.Ports {
			mapping, err := portMapping(port, opts)
			if err != nil {
				return nil, err
			}
			portsNode.Content = append(portsNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: mapping},
			)
		}
		svc.Content = append(svc.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "ports"},
			portsNode,
		)
	}

	return svc, nil
}

func appendScalarKV(parent *yaml.Node, key, value string) {
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

func portMapping(port harnesscatalog.PortRef, opts Options) (string, error) {
	if port.ContainerPort <= 0 {
		return "", fmt.Errorf("port %q has non-positive container_port %d", port.Name, port.ContainerPort)
	}
	protocol := strings.ToLower(strings.TrimSpace(port.Protocol))
	if opts.PortOffset == 0 {
		if protocol == "" {
			return fmt.Sprintf("%d", port.ContainerPort), nil
		}
		return fmt.Sprintf("%d/%s", port.ContainerPort, protocol), nil
	}
	hostPort := port.ContainerPort + opts.PortOffset
	if protocol == "" {
		return fmt.Sprintf("%d:%d", hostPort, port.ContainerPort), nil
	}
	return fmt.Sprintf("%d:%d/%s", hostPort, port.ContainerPort, protocol), nil
}
