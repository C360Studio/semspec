package architecturegenerator

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// upstreamVerifier performs the system-side reality check for an upstream
// resolution coordinate (issue #126). It is an interface so the arch-gen
// validator can be unit-tested with a fake — the production path hits the
// network, which makes the check non-gameable (the SYSTEM verifies, the model
// cannot self-report "I checked it").
type upstreamVerifier interface {
	// Exists reports whether the artifact/source behind a check actually
	// resolves. A non-nil error means the check could not be performed (network
	// / infra / ambiguous response) — the caller treats that as "skip", NOT
	// "fabricated", so the gate degrades to current behavior rather than blocking
	// on infra trouble or false-rejecting on an inconclusive signal.
	Exists(ctx context.Context, check workflow.CoordinateCheck) (bool, error)
}

// mavenCentralRepoBase is the authoritative Maven Central artifact repository.
// We probe it (not the search.maven.org solr index) because the index lags
// publication by hours — a real, freshly-published coordinate can report
// numFound=0 there, which would false-reject a legitimate dep — and it
// rate-limits aggressively. The repo's maven-metadata.xml is immediate,
// static-served (cheap HEAD), and version-agnostic at the group:artifact level.
const mavenCentralRepoBase = "https://repo1.maven.org/maven2"

// httpUpstreamVerifier is the production verifier. Maven checks HEAD the
// artifact's maven-metadata.xml on Maven Central; source_build checks probe the
// source_ref URL.
type httpUpstreamVerifier struct {
	client   *http.Client
	repoBase string // injectable for tests; defaults to Maven Central
}

func newHTTPUpstreamVerifier() *httpUpstreamVerifier {
	return &httpUpstreamVerifier{
		client:   &http.Client{Timeout: 6 * time.Second},
		repoBase: mavenCentralRepoBase,
	}
}

func (v *httpUpstreamVerifier) Exists(ctx context.Context, check workflow.CoordinateCheck) (bool, error) {
	switch {
	case check.IsMavenCheck():
		found, err := v.mavenArtifactExists(ctx, check.Group, check.Artifact)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
		if check.AllowJVMSuffix {
			// KMP publishes per-platform artifacts; the Java-consumable form is
			// the "-jvm" variant. Accept either.
			return v.mavenArtifactExists(ctx, check.Group, check.Artifact+"-jvm")
		}
		return false, nil
	case check.IsURLCheck():
		return v.urlResolves(ctx, check.URL)
	default:
		// Nothing to verify — treat as existing (no-op check).
		return true, nil
	}
}

// mavenArtifactExists HEADs the artifact's maven-metadata.xml on Maven Central.
// 200 → the group:artifact is published; 404/410 → it is not (the fabrication
// signal); any other status → error so the caller SKIPS rather than
// false-rejecting on an inconclusive response.
func (v *httpUpstreamVerifier) mavenArtifactExists(ctx context.Context, group, artifact string) (bool, error) {
	groupPath := strings.ReplaceAll(group, ".", "/")
	u := fmt.Sprintf("%s/%s/%s/maven-metadata.xml", v.repoBase, groupPath, artifact)
	return v.headDefinitive(ctx, u)
}

// urlResolves reports whether an http(s) source_ref resolves. GitHub answers
// HEAD on repo/tag URLs, so HEAD keeps it cheap.
func (v *httpUpstreamVerifier) urlResolves(ctx context.Context, target string) (bool, error) {
	return v.headDefinitive(ctx, target)
}

// headDefinitive performs a HEAD and maps the status to a definitive verdict:
//   - <400            → exists (the client already followed any redirect)
//   - 404 / 410       → definitively missing
//   - everything else → error, so the caller skips (405 Method-Not-Allowed,
//     401/403 auth, 429 rate-limit, 5xx) — these do NOT prove absence and must
//     not false-reject a real artifact/URL.
func (v *httpUpstreamVerifier) headDefinitive(ctx context.Context, target string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "semspec-arch-gen/1.0 (+upstream-coordinate-reality-check)")
	resp, err := v.client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode < 400:
		return true, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return false, nil
	default:
		return false, fmt.Errorf("inconclusive status %d for %s", resp.StatusCode, target)
	}
}
