package architecturegenerator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// fakeVerifier returns canned results keyed by group:artifact (maven) or URL,
// and an optional error to exercise the non-fatal skip path.
type fakeVerifier struct {
	exists map[string]bool
	err    error
}

func (f fakeVerifier) Exists(_ context.Context, ch workflow.CoordinateCheck) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	key := ch.URL
	if ch.IsMavenCheck() {
		key = ch.Group + ":" + ch.Artifact
	}
	return f.exists[key], nil
}

func newTestComponent(v upstreamVerifier) *Component {
	return &Component{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		verifier: v,
	}
}

func TestValidateUpstreamCoordinates(t *testing.T) {
	ctx := context.Background()

	t.Run("fabricated maven coordinate is rejected", func(t *testing.T) {
		c := newTestComponent(fakeVerifier{exists: map[string]bool{}}) // nothing exists
		rule, msg := c.validateUpstreamCoordinates(ctx, []workflow.UpstreamResolution{
			{Name: "Meshtastic", Coordinate: "org.meshtastic:protobufs:2.7.24"},
		})
		if rule != "upstream_coordinate_resolution" {
			t.Fatalf("rule=%q want upstream_coordinate_resolution (msg=%q)", rule, msg)
		}
		if !strings.Contains(msg, "does not resolve on Maven Central") {
			t.Errorf("msg should cite the numFound=0 evidence: %q", msg)
		}
		if !strings.Contains(msg, "unresolved") {
			t.Errorf("retry hint should offer the honest non-Maven kinds: %q", msg)
		}
	})

	t.Run("real maven coordinate passes", func(t *testing.T) {
		c := newTestComponent(fakeVerifier{exists: map[string]bool{"io.mavsdk:mavsdk": true}})
		if rule, msg := c.validateUpstreamCoordinates(ctx, []workflow.UpstreamResolution{
			{Name: "MAVSDK", Coordinate: "io.mavsdk:mavsdk:3.16.0"},
		}); rule != "" {
			t.Fatalf("real coordinate rejected: rule=%q msg=%q", rule, msg)
		}
	})

	t.Run("source_build with unreachable url is rejected", func(t *testing.T) {
		c := newTestComponent(fakeVerifier{exists: map[string]bool{}})
		rule, msg := c.validateUpstreamCoordinates(ctx, []workflow.UpstreamResolution{
			{Name: "OSH", ResolutionKind: "source_build", Coordinate: "org.sensorhub:sensorhub-core:2.0.1",
				SourceRef: "https://github.com/nope/does-not-exist"},
		})
		if rule != "upstream_coordinate_resolution" {
			t.Fatalf("rule=%q want rejection (msg=%q)", rule, msg)
		}
		if !strings.Contains(msg, "source_ref") {
			t.Errorf("hint should guide a real source_ref: %q", msg)
		}
	})

	t.Run("source_build with reachable url passes", func(t *testing.T) {
		c := newTestComponent(fakeVerifier{exists: map[string]bool{"https://github.com/opensensorhub/osh-core": true}})
		if rule, _ := c.validateUpstreamCoordinates(ctx, []workflow.UpstreamResolution{
			{Name: "OSH", ResolutionKind: "source_build", SourceRef: "https://github.com/opensensorhub/osh-core"},
		}); rule != "" {
			t.Fatalf("reachable source rejected: rule=%q", rule)
		}
	})

	t.Run("unresolved kind is not checked", func(t *testing.T) {
		c := newTestComponent(fakeVerifier{exists: map[string]bool{}})
		if rule, _ := c.validateUpstreamCoordinates(ctx, []workflow.UpstreamResolution{
			{Name: "Mystery", ResolutionKind: "unresolved"},
		}); rule != "" {
			t.Fatalf("unresolved must pass the deterministic gate, got rule=%q", rule)
		}
	})

	t.Run("verification error is non-fatal (skip, not reject)", func(t *testing.T) {
		c := newTestComponent(fakeVerifier{err: errors.New("network down")})
		if rule, _ := c.validateUpstreamCoordinates(ctx, []workflow.UpstreamResolution{
			{Name: "Meshtastic", Coordinate: "org.meshtastic:protobufs:2.7.24"},
		}); rule != "" {
			t.Fatalf("network error must skip (degrade), not reject; got rule=%q", rule)
		}
	})

	t.Run("nil verifier is a no-op", func(t *testing.T) {
		c := &Component{} // no verifier, no logger
		if rule, _ := c.validateUpstreamCoordinates(ctx, []workflow.UpstreamResolution{
			{Name: "X", Coordinate: "org.x:y:1"},
		}); rule != "" {
			t.Fatalf("nil verifier should no-op, got rule=%q", rule)
		}
	})
}
