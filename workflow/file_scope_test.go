package workflow

import "testing"

func TestCompanionTestPaths_JavaMainSource(t *testing.T) {
	got := CompanionTestPaths("src/main/java/org/sensorhub/impl/sensor/mavsdk/UnmannedSystem.java")
	want := "src/test/java/org/sensorhub/impl/sensor/mavsdk/UnmannedSystemTest.java"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("CompanionTestPaths() = %v, want [%s]", got, want)
	}
}

func TestExpandFileScopeWithCompanionTests_DedupesExistingCompanion(t *testing.T) {
	got := ExpandFileScopeWithCompanionTests([]string{
		"src/main/java/com/acme/Foo.java",
		"src/test/java/com/acme/FooTest.java",
		"src/main/java/com/acme/Foo.java",
	})
	want := []string{
		"src/main/java/com/acme/Foo.java",
		"src/test/java/com/acme/FooTest.java",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q; all=%v", i, got[i], want[i], got)
		}
	}
}

func TestCompanionTestPaths_NonJavaMainSourceNoExpansion(t *testing.T) {
	for _, p := range []string{
		"src/test/java/com/acme/FooTest.java",
		"src/main/java/com/acme/FooTest.java",
		"src/Foo.java",
		"README.md",
	} {
		if got := CompanionTestPaths(p); len(got) != 0 {
			t.Fatalf("CompanionTestPaths(%q) = %v, want none", p, got)
		}
	}
}
