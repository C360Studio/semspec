package workflow

import "testing"

func TestParseMavenCoordinate(t *testing.T) {
	tests := []struct {
		name       string
		coordinate string
		wantOK     bool
		wantGroup  string
		wantArt    string
	}{
		{"real maven", "org.sensorhub:sensorhub-core:2.0.1", true, "org.sensorhub", "sensorhub-core"},
		{"mavsdk", "io.mavsdk:mavsdk:3.16.0", true, "io.mavsdk", "mavsdk"},
		{"protobuf-java", "com.google.protobuf:protobuf-java:3.25.3", true, "com.google.protobuf", "protobuf-java"},
		{"npm prefix", "npm:react@18.2.0", false, "", ""},
		{"pypi prefix", "pypi:requests==2.31.0", false, "", ""},
		{"github url", "github.com/opensensorhub/osh-core@v2.0.0", false, "", ""},
		{"sitl pseudo", "mavlink:px4-sitl", false, "", ""},
		{"vague hint", "OSH 2.x", false, "", ""},
		{"empty", "", false, "", ""},
		{"two parts", "org.foo:bar", false, "", ""},
		{"npm as gav", "npm:react:latest", false, "", ""},
		{"go pseudo", "go:example.com/x:v1", false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, a, _, ok := ParseMavenCoordinate(tt.coordinate)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v want %v (coord %q)", ok, tt.wantOK, tt.coordinate)
			}
			if ok && (g != tt.wantGroup || a != tt.wantArt) {
				t.Fatalf("got %s:%s want %s:%s", g, a, tt.wantGroup, tt.wantArt)
			}
		})
	}
}

func TestEffectiveResolutionKind(t *testing.T) {
	tests := []struct {
		name string
		u    UpstreamResolution
		want ResolutionKind
	}{
		{"explicit source_build", UpstreamResolution{ResolutionKind: "source_build", Coordinate: "org.x:y:1"}, ResolutionKindSourceBuild},
		{"explicit unresolved", UpstreamResolution{ResolutionKind: "unresolved"}, ResolutionKindUnresolved},
		{"explicit kmp", UpstreamResolution{ResolutionKind: "kmp_multiplatform", Coordinate: "org.x:y:1"}, ResolutionKindKMP},
		{"empty + maven shape", UpstreamResolution{Coordinate: "org.meshtastic:protobufs:2.7.24"}, ResolutionKindMavenCentral},
		{"empty + non-maven", UpstreamResolution{Coordinate: "mavlink:px4-sitl"}, resolutionKindUnknown},
		{"empty + github", UpstreamResolution{Coordinate: "github.com/x/y@v1"}, resolutionKindUnknown},
		{"unrecognized falls back to inference", UpstreamResolution{ResolutionKind: "bogus", Coordinate: "io.mavsdk:mavsdk:3.16.0"}, ResolutionKindMavenCentral},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.EffectiveResolutionKind(); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestPlanUpstreamChecks(t *testing.T) {
	res := []UpstreamResolution{
		// maven-shaped, empty kind → maven check (the fabrication case)
		{Name: "Meshtastic", Coordinate: "org.meshtastic:protobufs:2.7.24"},
		// explicit source_build with http source_ref → URL check
		{Name: "OSH Core", ResolutionKind: "source_build", Coordinate: "org.sensorhub:sensorhub-core:2.0.1",
			SourceRef: "https://github.com/opensensorhub/osh-core"},
		// unresolved → no check
		{Name: "Mystery", ResolutionKind: "unresolved"},
		// non-maven coordinate, empty kind → no check
		{Name: "SITL", Coordinate: "mavlink:px4-sitl", SourceRef: "https://docs.px4.io"},
		// maven with a distinct per-API artifact → two maven checks
		{Name: "MAVSDK", Coordinate: "io.mavsdk:mavsdk:3.16.0", APIs: []APISurface{
			{Symbol: "System", Artifact: "io.mavsdk:mavsdk-server:3.16.0"},
		}},
		// source_build with a local /sources path → no URL probe (not http)
		{Name: "Local", ResolutionKind: "source_build", SourceRef: "/sources/foo/pom.xml"},
	}
	checks := PlanUpstreamChecks(res)

	var maven, url int
	keys := map[string]bool{}
	for _, ch := range checks {
		if ch.IsMavenCheck() {
			maven++
			keys[ch.Group+":"+ch.Artifact] = true
		}
		if ch.IsURLCheck() {
			url++
		}
	}
	// maven checks: meshtastic protobufs, mavsdk, mavsdk-server = 3
	if maven != 3 {
		t.Fatalf("maven checks = %d want 3 (%v)", maven, keys)
	}
	if !keys["io.mavsdk:mavsdk-server"] {
		t.Fatalf("expected per-API artifact mavsdk-server to be checked: %v", keys)
	}
	// url checks: OSH core only (SITL is unknown-kind; Local is non-http)
	if url != 1 {
		t.Fatalf("url checks = %d want 1", url)
	}
}

func TestPlanUpstreamChecksAPIArtifactCheckedRegardlessOfKind(t *testing.T) {
	// A source_build parent must not be a laundering route for a fabricated
	// Maven sub-artifact the dev would put on the classpath (go-reviewer H2/M1).
	res := []UpstreamResolution{
		{Name: "Sneaky", ResolutionKind: "source_build", SourceRef: "/sources/local",
			APIs: []APISurface{{Symbol: "X", Artifact: "com.fake:smuggled:1.0"}}},
	}
	checks := PlanUpstreamChecks(res)
	var found bool
	for _, ch := range checks {
		if ch.IsMavenCheck() && ch.Group == "com.fake" && ch.Artifact == "smuggled" {
			found = true
		}
	}
	if !found {
		t.Fatalf("api.Artifact under source_build must still be Maven-checked, got %+v", checks)
	}
}

func TestPlanUpstreamChecksDedup(t *testing.T) {
	res := []UpstreamResolution{
		{Name: "A", Coordinate: "io.mavsdk:mavsdk:3.16.0"},
		{Name: "B", Coordinate: "io.mavsdk:mavsdk:3.17.0"}, // same g:a, different version
	}
	if got := len(PlanUpstreamChecks(res)); got != 1 {
		t.Fatalf("expected dedup by group:artifact, got %d checks", got)
	}
}

func TestDecideUpstreamVerdict(t *testing.T) {
	maven := CoordinateCheck{ResolutionName: "X", Kind: ResolutionKindMavenCentral, Coordinate: "a:b:1", Group: "a", Artifact: "b"}
	src := CoordinateCheck{ResolutionName: "Y", Kind: ResolutionKindSourceBuild, URL: "https://example.com"}

	if rej, _ := DecideUpstreamVerdict(maven, true); rej {
		t.Fatal("exists=true must not reject")
	}
	if rej, reason := DecideUpstreamVerdict(maven, false); !rej || reason == "" {
		t.Fatalf("maven numFound=0 must reject with reason, got rej=%v reason=%q", rej, reason)
	}
	if rej, _ := DecideUpstreamVerdict(src, false); !rej {
		t.Fatal("source_build unreachable must reject")
	}
}
