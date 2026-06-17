package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileSystemDetectorDetectsBrownfieldTopologyFacts(t *testing.T) {
	root := t.TempDir()
	writeDetectionFile(t, root, "go.mod", "module example.com/root\n\ngo 1.22\n")
	writeDetectionFile(t, root, "go.work", "go 1.22\n\nuse (\n\t./gateway\n)\n")
	writeDetectionFile(t, root, "gateway/go.mod", "module example.com/root/gateway\n\ngo 1.22\n")
	writeDetectionFile(t, root, "ui/package.json", `{"workspaces":{"packages":["packages/*","tools/*"]}}`)
	writeDetectionFile(t, root, "ui/pnpm-workspace.yaml", "packages:\n  - packages/*\n")
	writeDetectionFile(t, root, "settings.gradle", `
pluginManagement { repositories { gradlePluginPortal() } }
rootProject.name = 'osh-addons'
include ':sensorhub-driver-mavsdk', ':sensorhub-tools'
includeBuild '../osh-core'
`)
	writeDetectionFile(t, root, "sensorhub-driver-mavsdk/build.gradle", "plugins { id 'java-library' }\n")
	writeDetectionFile(t, root, "services/rust/Cargo.toml", "[workspace]\nmembers = [\"crates/*\"]\n")

	result, err := NewFileSystemDetector().Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	assertTopologyFact(t, result, "repository_root", ".", filepath.Base(root))
	assertTopologyFact(t, result, "build_root", "go.mod", "go_module")
	assertTopologyFact(t, result, "workspace_root", "go.work", "go_workspace")
	assertTopologyFact(t, result, "module_include", "go.work", "./gateway")
	assertTopologyFact(t, result, "build_root", "gateway/go.mod", "go_module")
	assertTopologyFact(t, result, "package_root", "ui/package.json", "node_package")
	assertTopologyFact(t, result, "module_include", "ui/package.json", "packages/*")
	assertTopologyFact(t, result, "module_include", "ui/package.json", "tools/*")
	assertTopologyFact(t, result, "workspace_root", "ui/pnpm-workspace.yaml", "pnpm_workspace")
	assertTopologyFact(t, result, "workspace_root", "settings.gradle", "gradle_settings")
	assertTopologyFact(t, result, "module_include", "settings.gradle", ":sensorhub-driver-mavsdk")
	assertTopologyFact(t, result, "module_include", "settings.gradle", ":sensorhub-tools")
	assertTopologyFact(t, result, "composite_build", "settings.gradle", "../osh-core")
	assertTopologyFact(t, result, "build_root", "sensorhub-driver-mavsdk/build.gradle", "gradle_project")
	assertTopologyFact(t, result, "build_root", "services/rust/Cargo.toml", "rust_manifest")
	assertTopologyFact(t, result, "workspace_root", "services/rust/Cargo.toml", "rust_workspace")
}

func TestFileSystemDetectorTopologySkipsGeneratedAndDependencyDirs(t *testing.T) {
	root := t.TempDir()
	writeDetectionFile(t, root, "go.mod", "module example.com/root\n\ngo 1.22\n")
	writeDetectionFile(t, root, "node_modules/pkg/package.json", `{"name":"ignored"}`)
	writeDetectionFile(t, root, "build/tmp/go.mod", "module example.com/generated\n\ngo 1.22\n")
	writeDetectionFile(t, root, ".gradle/settings.gradle", "include ':ignored'\n")
	writeDetectionFile(t, root, "vendor/lib/Cargo.toml", "[package]\nname = \"ignored\"\n")

	result, err := NewFileSystemDetector().Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	assertTopologyFact(t, result, "build_root", "go.mod", "go_module")
	assertNoTopologyFactPath(t, result, "node_modules/pkg/package.json")
	assertNoTopologyFactPath(t, result, "build/tmp/go.mod")
	assertNoTopologyFactPath(t, result, ".gradle/settings.gradle")
	assertNoTopologyFactPath(t, result, "vendor/lib/Cargo.toml")
}

func writeDetectionFile(t *testing.T, root, rel, content string) {
	t.Helper()

	fullPath := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func assertTopologyFact(t *testing.T, result *DetectionResult, kind, path, value string) {
	t.Helper()

	if hasTopologyFact(result, kind, path, value) {
		return
	}
	t.Fatalf("missing topology fact kind=%q path=%q value=%q in %#v", kind, path, value, result.TopologyFacts)
}

func hasTopologyFact(result *DetectionResult, kind, path, value string) bool {
	for _, fact := range result.TopologyFacts {
		if fact.Kind == kind && fact.Path == path && fact.Value == value {
			return true
		}
	}
	return false
}

func assertNoTopologyFactPath(t *testing.T, result *DetectionResult, path string) {
	t.Helper()

	for _, fact := range result.TopologyFacts {
		if fact.Path == path {
			t.Fatalf("unexpected topology fact for %q: %#v", path, fact)
		}
	}
}
