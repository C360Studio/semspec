package prompt

import (
	"fmt"
	"strings"
)

// FormatArchitectureContext renders a markdown summary of the architecture
// surface for prompt injection. Each section is emitted only when its slice is
// non-empty, so callers populate just the sections their role needs and get a
// faithful projection of those facts. Returns the empty string when the whole
// projection is empty.
//
// This is the single faithful graph→role projection (Plan B consolidation:
// pre-rendering happens in component code because the source data lives in
// workflow types the prompt package shouldn't depend on transitively).
func FormatArchitectureContext(p ArchitectureProjection) string {
	if len(p.Actors) == 0 && len(p.Integrations) == 0 &&
		len(p.Components) == 0 && len(p.Upstreams) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Architecture Context\n\n")
	sb.WriteString("Ground your work in the architecture facts below. They are the architect's resolved decisions — do not re-derive or contradict them.\n\n")

	writeComponentsSection(&sb, p.Components)
	writeUpstreamsSection(&sb, p.Upstreams)
	writeActorsSection(&sb, p.Actors)
	writeIntegrationsSection(&sb, p.Integrations)

	return sb.String()
}

// FormatUpstreamResolutions renders only the architect-resolved external
// dependencies — the load-bearing surface for the developer and the per-task
// reviewer. The Coordinate is build-manifest-ready; the APIs carry the exact
// symbols/signatures so the dev integrates against resolved facts instead of
// hallucinating a coordinate the architect already pinned. Returns the empty
// string when there are no resolutions.
func FormatUpstreamResolutions(upstreams []UpstreamResolutionInfo) string {
	if len(upstreams) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Resolved Upstream Dependencies\n\n")
	sb.WriteString("The architect already resolved these external dependencies. Use these exact coordinates and API surfaces — do NOT invent versions, package names, or method signatures.\n\n")
	writeUpstreamsBody(&sb, upstreams)
	return sb.String()
}

func writeComponentsSection(sb *strings.Builder, components []ComponentInfo) {
	if len(components) == 0 {
		return
	}
	sb.WriteString("### Components\n\n")
	for _, c := range components {
		fmt.Fprintf(sb, "- **%s** — %s\n", c.Name, c.Responsibility)
		if len(c.Capabilities) > 0 {
			fmt.Fprintf(sb, "  - capabilities: %s\n", strings.Join(c.Capabilities, ", "))
		}
		if len(c.UpstreamRefs) > 0 {
			fmt.Fprintf(sb, "  - depends on upstream: %s\n", strings.Join(c.UpstreamRefs, ", "))
		}
		if len(c.ImplementationFiles) > 0 {
			fmt.Fprintf(sb, "  - files: %s\n", strings.Join(c.ImplementationFiles, ", "))
		}
	}
	sb.WriteString("\n")
}

func writeUpstreamsSection(sb *strings.Builder, upstreams []UpstreamResolutionInfo) {
	if len(upstreams) == 0 {
		return
	}
	sb.WriteString("### Resolved Upstream Dependencies\n\n")
	writeUpstreamsBody(sb, upstreams)
}

func writeUpstreamsBody(sb *strings.Builder, upstreams []UpstreamResolutionInfo) {
	for _, u := range upstreams {
		role := u.Role
		if role == "" {
			role = "runtime_dep"
		}
		fmt.Fprintf(sb, "- **%s** `%s` (%s)\n", u.Name, u.Coordinate, role)
		if u.SourceRef != "" {
			fmt.Fprintf(sb, "  - source: %s\n", u.SourceRef)
		}
		if len(u.UsedBy) > 0 {
			fmt.Fprintf(sb, "  - used by: %s\n", strings.Join(u.UsedBy, ", "))
		}
		for _, api := range u.APIs {
			fmt.Fprintf(sb, "  - `%s`", api.Symbol)
			if api.Kind != "" {
				fmt.Fprintf(sb, " (%s)", api.Kind)
			}
			if api.Signature != "" {
				fmt.Fprintf(sb, ": `%s`", api.Signature)
			}
			sb.WriteString("\n")
			// Import is the paste-ready, verified fully-qualified reference —
			// render it FIRST and prominently so the dev uses it verbatim instead
			// of guessing the package and rediscovering it (2026-06-07 mavsdk thrash).
			if api.Import != "" {
				fmt.Fprintf(sb, "    - import: `%s`", api.Import)
				if api.Artifact != "" {
					fmt.Fprintf(sb, " (from artifact `%s`)", api.Artifact)
				}
				sb.WriteString("\n")
			} else if api.Artifact != "" {
				fmt.Fprintf(sb, "    - artifact: `%s`\n", api.Artifact)
			}
			if api.Lifecycle != "" {
				fmt.Fprintf(sb, "    - lifecycle: %s\n", api.Lifecycle)
			}
			if api.Notes != "" {
				fmt.Fprintf(sb, "    - note: %s\n", api.Notes)
			}
		}
	}
	sb.WriteString("\n")
}

func writeActorsSection(sb *strings.Builder, actors []ActorInfo) {
	if len(actors) == 0 {
		return
	}
	sb.WriteString("### Actors\n\n")
	for _, a := range actors {
		fmt.Fprintf(sb, "- **%s** (%s)", a.Name, a.Type)
		if len(a.Triggers) > 0 {
			fmt.Fprintf(sb, ": %s", strings.Join(a.Triggers, ", "))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
}

func writeIntegrationsSection(sb *strings.Builder, integrations []IntegrationInfo) {
	if len(integrations) == 0 {
		return
	}
	sb.WriteString("### Integration Points\n\n")
	for _, ip := range integrations {
		fmt.Fprintf(sb, "- **%s** (%s, %s)\n", ip.Name, ip.Direction, ip.Protocol)
	}
	sb.WriteString("\n")
}
