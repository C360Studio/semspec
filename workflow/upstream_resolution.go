package workflow

import "strings"

// ResolutionKind classifies HOW an UpstreamResolution's artifact is consumed,
// which determines the system-side reality check applied at arch-gen (issue
// #126). The architect declares the kind so the system knows which authoritative
// signal to verify against — the check is performed by the SYSTEM, not
// self-reported by the model, which is what makes it non-gameable.
//
//   - maven_central: a published Java jar on Maven Central. Verified: the
//     coordinate (or a per-API artifact) returns numFound>0 on the Central
//     search index.
//   - source_build: built from source (git clone + local build / codegen).
//     Verified: the source_ref URL resolves (the repo/tag exists). This is the
//     honest kind for deps with no published jar — e.g. a project that only
//     ships .proto + a Wire/protoc build, or a github-source gradle module.
//   - kmp_multiplatform: a Kotlin Multiplatform package whose Java-consumable
//     form is a platform-suffixed artifact. Verified: the coordinate OR its
//     "-jvm" suffix resolves on Maven Central.
//   - unresolved: the architect could NOT find a consumable artifact. This is a
//     first-class HONEST outcome, not a fabrication — it is surfaced to the
//     reviewer/human rather than silently passed, and it never invents a
//     coordinate. No network check is applied.
//
// Empty kind is inferred from the coordinate shape (see EffectiveResolutionKind):
// a Maven-shaped "group:artifact:version" coordinate defaults to maven_central so
// records predating this field — and architects that omit it — still get the
// fabrication check. Non-Maven-shaped coordinates (npm:, pypi:, github.com/...,
// "mavlink:px4-sitl") infer to an empty/unknown kind that is intentionally NOT
// network-checked: the gate is scoped to the proven Maven-fabrication class and
// must not false-positive on ecosystems it does not verify.
type ResolutionKind string

// Resolution kinds an architect may declare; the gate verifies each differently.
const (
	ResolutionKindMavenCentral ResolutionKind = "maven_central"
	ResolutionKindSourceBuild  ResolutionKind = "source_build"
	ResolutionKindKMP          ResolutionKind = "kmp_multiplatform"
	ResolutionKindUnresolved   ResolutionKind = "unresolved"
	// resolutionKindUnknown is the inferred result for a coordinate whose shape
	// we do not verify (npm/pypi/git/sitl). Not a valid architect-declared value;
	// it simply means "no system-side check applies".
	resolutionKindUnknown ResolutionKind = ""
)

// ValidResolutionKinds lists the kinds an architect may declare. Used by the
// prompt/spec and by validation messaging. Excludes the inferred-only unknown.
var ValidResolutionKinds = []ResolutionKind{
	ResolutionKindMavenCentral,
	ResolutionKindSourceBuild,
	ResolutionKindKMP,
	ResolutionKindUnresolved,
}

// EffectiveResolutionKind returns the architect-declared kind, or infers one
// from the coordinate shape when the field is empty. The inference is
// deliberately conservative: only an unambiguous Maven coordinate maps to
// maven_central; everything else stays unknown (unchecked) so the gate never
// fires on an ecosystem it cannot authoritatively verify.
func (u UpstreamResolution) EffectiveResolutionKind() ResolutionKind {
	switch ResolutionKind(strings.TrimSpace(u.ResolutionKind)) {
	case ResolutionKindMavenCentral:
		return ResolutionKindMavenCentral
	case ResolutionKindSourceBuild:
		return ResolutionKindSourceBuild
	case ResolutionKindKMP:
		return ResolutionKindKMP
	case ResolutionKindUnresolved:
		return ResolutionKindUnresolved
	}
	// Empty/unrecognized: infer from the coordinate shape.
	if _, _, _, ok := ParseMavenCoordinate(u.Coordinate); ok {
		return ResolutionKindMavenCentral
	}
	return resolutionKindUnknown
}

// ParseMavenCoordinate parses a "group:artifact:version" coordinate. It returns
// ok=false for anything that is not an unambiguous Maven coordinate — npm:/pypi:
// prefixed strings (which split into 2 parts), versioned URLs ("github.com/x@v",
// which contain "/" or "@"), and vague hints. This is the gate's shape guard:
// only an ok=true coordinate is eligible for the Maven Central existence check.
func ParseMavenCoordinate(coordinate string) (group, artifact, version string, ok bool) {
	c := strings.TrimSpace(coordinate)
	if c == "" {
		return "", "", "", false
	}
	// Reject shapes that are clearly not bare Maven GAV coordinates.
	if strings.ContainsAny(c, "/@ ") {
		return "", "", "", false
	}
	parts := strings.Split(c, ":")
	if len(parts) != 3 {
		return "", "", "", false
	}
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return "", "", "", false
		}
	}
	// A Maven groupId is dotted (e.g. org.sensorhub); reject single-token
	// pseudo-coordinates like "npm:react:latest" where the first segment is a
	// known ecosystem prefix. We treat a missing dot in the group as non-Maven
	// only when it matches a known package-manager prefix; real groups always
	// contain a dot.
	switch strings.ToLower(parts[0]) {
	case "npm", "pypi", "pip", "gem", "cargo", "go", "nuget", "github.com", "git":
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// CoordinateCheck is one system-side verification the arch-gen gate must perform.
// It is produced purely from the architecture (PlanUpstreamChecks) so the I/O
// layer stays thin and the decision logic stays unit-testable offline.
type CoordinateCheck struct {
	// ResolutionName is the owning UpstreamResolution.Name, for messaging.
	ResolutionName string
	// Kind is the effective resolution kind driving which check applies.
	Kind ResolutionKind
	// Coordinate is the human-facing coordinate string being verified.
	Coordinate string

	// Maven check fields (set when Kind is maven_central or kmp_multiplatform).
	Group    string
	Artifact string
	// AllowJVMSuffix is true for KMP: the consumable Java artifact is often the
	// "-jvm" platform variant, so the verifier should accept either form.
	AllowJVMSuffix bool

	// URL check field (set when Kind is source_build): the source_ref that must
	// resolve to prove the source actually exists.
	URL string
}

// IsMavenCheck reports whether this check verifies a Maven Central artifact.
func (ch CoordinateCheck) IsMavenCheck() bool { return ch.Group != "" && ch.Artifact != "" }

// IsURLCheck reports whether this check verifies a source_ref URL resolves.
func (ch CoordinateCheck) IsURLCheck() bool { return ch.URL != "" }

// PlanUpstreamChecks decides — purely, no I/O — which coordinates the system must
// verify for a set of resolutions. It emits:
//   - one Maven check per maven_central / kmp_multiplatform resolution whose
//     coordinate parses as a Maven GAV;
//   - one Maven check per DISTINCT api.Artifact that parses as a Maven GAV (a
//     fabricated sub-artifact is as harmful as a fabricated top-level one);
//   - one URL check per source_build resolution that carries a source_ref.
//
// unresolved and unknown-kind resolutions produce no checks (honest / out of
// scope). Maven checks are de-duplicated by group:artifact.
func PlanUpstreamChecks(resolutions []UpstreamResolution) []CoordinateCheck {
	var checks []CoordinateCheck
	seenMaven := map[string]bool{}

	addMaven := func(name, coordinate string, allowJVM bool) {
		g, a, _, ok := ParseMavenCoordinate(coordinate)
		if !ok {
			return
		}
		key := g + ":" + a
		if seenMaven[key] {
			return
		}
		seenMaven[key] = true
		checks = append(checks, CoordinateCheck{
			ResolutionName: name,
			Kind:           ResolutionKindMavenCentral,
			Coordinate:     coordinate,
			Group:          g,
			Artifact:       a,
			AllowJVMSuffix: allowJVM,
		})
	}

	for _, u := range resolutions {
		kind := u.EffectiveResolutionKind()
		isKMP := kind == ResolutionKindKMP

		// Top-level coordinate is Maven-checked only when the dep is consumed AS
		// a published jar. source_build pulls nothing from Central (the dev
		// builds from source), so its top-level coordinate is informational.
		if kind == ResolutionKindMavenCentral || kind == ResolutionKindKMP {
			addMaven(u.Name, u.Coordinate, isKMP)
		}

		// Per-API artifacts are what the dev actually puts on the classpath, so a
		// fabricated api.Artifact is as harmful as a fabricated top-level one —
		// and is checked REGARDLESS of the parent kind. This closes the dodge of
		// declaring source_build/unresolved on the parent while smuggling a fake
		// Maven sub-artifact (go-reviewer H2/M1, 2026-06-07).
		for _, api := range u.APIs {
			if api.Artifact != "" {
				addMaven(u.Name, api.Artifact, isKMP)
			}
		}

		// source_build's source_ref must resolve (the git repo/tag must exist).
		if kind == ResolutionKindSourceBuild {
			if url := strings.TrimSpace(u.SourceRef); url != "" && looksLikeURL(url) {
				checks = append(checks, CoordinateCheck{
					ResolutionName: u.Name,
					Kind:           ResolutionKindSourceBuild,
					Coordinate:     u.Coordinate,
					URL:            url,
				})
			}
		}
	}
	return checks
}

// looksLikeURL is a cheap guard so we only HTTP-probe http(s) source refs, not
// local /sources/ paths.
func looksLikeURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// DecideUpstreamVerdict is the pure decision: given a check and whether the
// system found it to exist, returns whether the resolution is rejected and a
// factual reason (no remediation prose — the caller adds the retry hint). Only a
// DEFINITIVE non-existence rejects; the caller skips on verification error so
// network/infra trouble degrades to current behavior rather than blocking.
func DecideUpstreamVerdict(ch CoordinateCheck, exists bool) (rejected bool, reason string) {
	if exists {
		return false, ""
	}
	switch ch.Kind {
	case ResolutionKindMavenCentral:
		return true, "upstream resolution " + quote(ch.ResolutionName) + " declares Maven coordinate " +
			quote(ch.Coordinate) + " but it does not resolve on Maven Central (the system re-checked: 0 results)"
	case ResolutionKindSourceBuild:
		return true, "upstream resolution " + quote(ch.ResolutionName) + " is source_build but its source_ref " +
			quote(ch.URL) + " does not resolve (the system re-checked: not reachable)"
	default:
		return false, ""
	}
}

func quote(s string) string { return "\"" + s + "\"" }
