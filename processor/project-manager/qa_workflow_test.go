package projectmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestBuildQAWorkflow_NilConfigFallsBackToGo(t *testing.T) {
	body := BuildQAWorkflow(nil)
	if !strings.Contains(body, "setup-go") {
		t.Errorf("nil config should fall back to Go template, got:\n%s", body)
	}
}

func TestBuildQAWorkflow_JavaPrimary(t *testing.T) {
	pc := &workflow.ProjectConfig{
		Languages: []workflow.LanguageInfo{
			{Name: "Java", Primary: true},
		},
	}
	body := BuildQAWorkflow(pc)
	checks := map[string]bool{
		"setup-java":            true,
		"distribution: temurin": true,
		"java-version: '21'":    true,
		"./gradlew test":        true,
		"setup-go":              false, // wrong-language toolchain
		"go-version":            false,
	}
	for needle, want := range checks {
		got := strings.Contains(body, needle)
		if got != want {
			t.Errorf("Java workflow: contains(%q) = %v, want %v\nBody:\n%s", needle, got, want, body)
		}
	}
}

func TestBuildQAWorkflow_JavaWithCustomTestCommand(t *testing.T) {
	pc := &workflow.ProjectConfig{
		Languages:     []workflow.LanguageInfo{{Name: "Java", Primary: true}},
		QATestCommand: "./mvnw verify",
	}
	body := BuildQAWorkflow(pc)
	if !strings.Contains(body, "./mvnw verify") {
		t.Errorf("custom test command should appear: %s", body)
	}
	if strings.Contains(body, "./gradlew test") {
		t.Errorf("default gradlew command should be replaced by custom: %s", body)
	}
}

func TestBuildQAWorkflow_KotlinTreatedAsJava(t *testing.T) {
	pc := &workflow.ProjectConfig{
		Languages: []workflow.LanguageInfo{{Name: "Kotlin", Primary: true}},
	}
	body := BuildQAWorkflow(pc)
	if !strings.Contains(body, "setup-java") {
		t.Errorf("Kotlin should use Java template (Gradle/JDK), got:\n%s", body)
	}
}

func TestBuildQAWorkflow_PythonPrimary(t *testing.T) {
	pc := &workflow.ProjectConfig{
		Languages: []workflow.LanguageInfo{{Name: "Python", Primary: true}},
	}
	body := BuildQAWorkflow(pc)
	if !strings.Contains(body, "setup-python") || !strings.Contains(body, "pytest") {
		t.Errorf("Python template missing setup-python or pytest:\n%s", body)
	}
}

func TestBuildQAWorkflow_NodePrimary(t *testing.T) {
	pc := &workflow.ProjectConfig{
		Languages: []workflow.LanguageInfo{{Name: "TypeScript", Primary: true}},
	}
	body := BuildQAWorkflow(pc)
	if !strings.Contains(body, "setup-node") || !strings.Contains(body, "npm test") {
		t.Errorf("Node/TS template missing setup-node or npm test:\n%s", body)
	}
}

func TestBuildQAWorkflow_RustPrimary(t *testing.T) {
	pc := &workflow.ProjectConfig{
		Languages: []workflow.LanguageInfo{{Name: "Rust", Primary: true}},
	}
	body := BuildQAWorkflow(pc)
	if !strings.Contains(body, "rust-toolchain") || !strings.Contains(body, "cargo test") {
		t.Errorf("Rust template missing toolchain or cargo test:\n%s", body)
	}
}

func TestBuildQAWorkflow_AlwaysEmitsIntegrationJob(t *testing.T) {
	for _, lang := range []string{"Go", "Java", "Python", "TypeScript", "Rust"} {
		pc := &workflow.ProjectConfig{
			Languages: []workflow.LanguageInfo{{Name: lang, Primary: true}},
		}
		body := BuildQAWorkflow(pc)
		if !strings.Contains(body, "integration:") {
			t.Errorf("%s template missing integration job:\n%s", lang, body)
		}
		if !strings.Contains(body, "actions/checkout@v4") {
			t.Errorf("%s template missing checkout step:\n%s", lang, body)
		}
	}
}

func TestEnsureQAWorkflow_WritesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	pc := &workflow.ProjectConfig{
		Languages: []workflow.LanguageInfo{{Name: "Java", Primary: true}},
	}
	logger := &captureLogger{}

	if err := EnsureQAWorkflow(dir, pc, logger); err != nil {
		t.Fatalf("EnsureQAWorkflow: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(dir, ".github", "workflows", "qa.yml"))
	if err != nil {
		t.Fatalf("read qa.yml: %v", err)
	}
	if !strings.Contains(string(written), "setup-java") {
		t.Errorf("written workflow should be Java-flavored:\n%s", written)
	}
	if !logger.infoSeen {
		t.Error("scaffolding should log Info on write")
	}
}

func TestEnsureQAWorkflow_LeavesExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	custom := "name: Custom Workflow\n# user-authored — must not be overwritten\n"
	abs := filepath.Join(dir, ".github", "workflows", "qa.yml")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(custom), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	pc := &workflow.ProjectConfig{
		Languages: []workflow.LanguageInfo{{Name: "Java", Primary: true}},
	}
	if err := EnsureQAWorkflow(dir, pc, &captureLogger{}); err != nil {
		t.Fatalf("EnsureQAWorkflow: %v", err)
	}

	got, _ := os.ReadFile(abs)
	if string(got) != custom {
		t.Errorf("existing user-authored qa.yml was overwritten:\nwant: %q\ngot:  %q", custom, got)
	}
}

func TestEnsureQAWorkflow_HandlesNilProjectConfig(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureQAWorkflow(dir, nil, &captureLogger{}); err != nil {
		t.Fatalf("nil projectConfig should still scaffold a default (Go fallback): %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".github", "workflows", "qa.yml"))
	if !strings.Contains(string(body), "setup-go") {
		t.Errorf("nil config should produce Go fallback:\n%s", body)
	}
}

// captureLogger is a minimal Logger that records whether Info/Warn fired.
type captureLogger struct {
	infoSeen bool
	warnSeen bool
}

func (l *captureLogger) Info(_ string, _ ...any) { l.infoSeen = true }
func (l *captureLogger) Warn(_ string, _ ...any) { l.warnSeen = true }
