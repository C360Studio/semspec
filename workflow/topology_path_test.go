package workflow

import "testing"

func TestIsTopologyControlledPath(t *testing.T) {
	tests := map[string]bool{
		"go.mod":                     true,
		"./settings.gradle":          true,
		"services/api/package.json":  true,
		"plugins/new-driver/gradlew": true,
		"plugins/new-driver/gradle/wrapper/gradle-wrapper.properties": true,
		"src/main/java/Driver.java":                                   false,
		"README.md":                                                   false,
		"../outside/settings.gradle":                                  false,
	}

	for p, want := range tests {
		if got := IsTopologyControlledPath(p); got != want {
			t.Errorf("IsTopologyControlledPath(%q) = %v, want %v", p, got, want)
		}
	}
}
