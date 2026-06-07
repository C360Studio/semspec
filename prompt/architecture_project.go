package prompt

import "github.com/c360studio/semspec/workflow"

// ProjectArchitecture converts a workflow.ArchitectureDocument into the
// prompt-package ArchitectureProjection. This is the SINGLE faithful graph→role
// projection: every role builds its architecture context from this one function
// (selecting the sections it needs) so the lossy per-role re-projection drift
// that dropped upstream_resolutions for the dev, Sarah, Bob, and recovery can't
// recur. Returns the zero projection when arch is nil.
func ProjectArchitecture(arch *workflow.ArchitectureDocument) ArchitectureProjection {
	if arch == nil {
		return ArchitectureProjection{}
	}
	return ArchitectureProjection{
		Actors:       projectActors(arch.Actors),
		Integrations: projectIntegrations(arch.Integrations),
		Components:   projectComponents(arch.ComponentBoundaries),
		Upstreams:    ProjectUpstreams(arch),
	}
}

// ProjectUpstreams converts the architect's resolved external dependencies into
// the prompt view. Standalone because the developer + per-task reviewer need
// just this slice (rendered via FormatUpstreamResolutions) without the rest of
// the architecture surface. Returns nil when arch is nil or has no resolutions.
func ProjectUpstreams(arch *workflow.ArchitectureDocument) []UpstreamResolutionInfo {
	if arch == nil || len(arch.UpstreamResolutions) == 0 {
		return nil
	}
	out := make([]UpstreamResolutionInfo, len(arch.UpstreamResolutions))
	for i, u := range arch.UpstreamResolutions {
		apis := make([]APISurfaceInfo, len(u.APIs))
		for j, a := range u.APIs {
			apis[j] = APISurfaceInfo{
				Symbol:    a.Symbol,
				Import:    a.Import,
				Artifact:  a.Artifact,
				Kind:      a.Kind,
				Signature: a.Signature,
				Lifecycle: a.Lifecycle,
				Notes:     a.Notes,
				Citation:  a.Citation,
			}
		}
		out[i] = UpstreamResolutionInfo{
			Name:       u.Name,
			Coordinate: u.Coordinate,
			SourceRef:  u.SourceRef,
			Role:       u.Role,
			UsedBy:     append([]string(nil), u.UsedBy...),
			APIs:       apis,
		}
	}
	return out
}

func projectActors(actors []workflow.ActorDef) []ActorInfo {
	if len(actors) == 0 {
		return nil
	}
	out := make([]ActorInfo, len(actors))
	for i, a := range actors {
		out[i] = ActorInfo{Name: a.Name, Type: a.Type, Triggers: append([]string(nil), a.Triggers...)}
	}
	return out
}

func projectIntegrations(integrations []workflow.IntegrationPoint) []IntegrationInfo {
	if len(integrations) == 0 {
		return nil
	}
	out := make([]IntegrationInfo, len(integrations))
	for i, ip := range integrations {
		out[i] = IntegrationInfo{Name: ip.Name, Direction: ip.Direction, Protocol: ip.Protocol}
	}
	return out
}

func projectComponents(components []workflow.ComponentDef) []ComponentInfo {
	if len(components) == 0 {
		return nil
	}
	out := make([]ComponentInfo, len(components))
	for i, c := range components {
		out[i] = ComponentInfo{
			Name:                c.Name,
			Responsibility:      c.Responsibility,
			UpstreamRefs:        append([]string(nil), c.UpstreamRefs...),
			ImplementationFiles: append([]string(nil), c.ImplementationFiles...),
			Capabilities:        append([]string(nil), c.Capabilities...),
		}
	}
	return out
}
