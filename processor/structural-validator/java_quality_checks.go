package structuralvalidator

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

const oshModuleProviderServicePath = "src/main/resources/META-INF/services/org.sensorhub.api.module.IModuleProvider"

var (
	javaClassNameRe      = regexp.MustCompile(`\b(?:class|record)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	javaPackageRe        = regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z_][A-Za-z0-9_.]*)\s*;`)
	javaModuleProviderRe = regexp.MustCompile(`\bimplements\s+[^{;]*\bIModuleProvider\b`)

	javaIncompleteMarkers = []struct {
		label string
		re    *regexp.Regexp
	}{
		{"placeholder implementation marker", regexp.MustCompile(`(?i)\b(dummy checksum|dummy implementation|fake implementation|placeholder|not implemented)\b`)},
		{"review-scaffold marker", regexp.MustCompile(`(?i)refactored to pass review`)},
	}
)

func shouldRunJavaQualityChecks(filesModified []string, workDir string) bool {
	if len(filesModified) == 0 {
		_, err := os.Stat(filepath.Join(workDir, "src", "main", "java"))
		return err == nil
	}
	for _, file := range workflow.NormalizeFilePaths(filesModified) {
		if strings.HasSuffix(file, ".java") || file == oshModuleProviderServicePath {
			return true
		}
	}
	return false
}

// CheckJavaImplementationCompleteness fails when production Java source
// contains high-signal placeholder or fabricated-implementation markers.
func CheckJavaImplementationCompleteness(workDir string, filesModified []string) payloads.CheckResult {
	files := mainJavaFilesToInspect(workDir, filesModified)
	if len(files) == 0 {
		return payloads.CheckResult{
			Name:     "java-implementation-completeness",
			Passed:   true,
			Required: true,
			Command:  "java-implementation-completeness (internal)",
			Stdout:   "no src/main/java files to inspect",
			Duration: "0s",
		}
	}

	var findings []string
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(rel)))
		if err != nil {
			findings = append(findings, fmt.Sprintf("%s: read failed: %v", rel, err))
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			for _, marker := range javaIncompleteMarkers {
				if marker.re.MatchString(line) {
					findings = append(findings, fmt.Sprintf("%s:%d %s: %s", rel, i+1, marker.label, strings.TrimSpace(line)))
					break
				}
			}
		}
	}

	if len(findings) == 0 {
		return payloads.CheckResult{
			Name:     "java-implementation-completeness",
			Passed:   true,
			Required: true,
			Command:  "java-implementation-completeness (internal)",
			Stdout:   fmt.Sprintf("%d src/main/java file(s) inspected, no placeholder markers found", len(files)),
			Duration: "0s",
		}
	}

	return payloads.CheckResult{
		Name:     "java-implementation-completeness",
		Passed:   false,
		Required: true,
		Command:  "java-implementation-completeness (internal)",
		Stderr:   "placeholder or review-scaffold marker in production Java source:\n" + strings.Join(limitStrings(findings, 10), "\n"),
		Duration: "0s",
	}
}

// CheckOSHModuleProviderRegistration fails when OSH module providers are not
// listed in the Java service-loader file required for module discovery.
func CheckOSHModuleProviderRegistration(workDir string) payloads.CheckResult {
	providers, err := findOSHModuleProviders(workDir)
	if err != nil {
		return payloads.CheckResult{
			Name:     "osh-module-provider-registration",
			Passed:   false,
			Required: true,
			Command:  "osh-module-provider-registration (internal)",
			Stderr:   fmt.Sprintf("scan src/main/java: %v", err),
			Duration: "0s",
		}
	}
	if len(providers) == 0 {
		return payloads.CheckResult{
			Name:     "osh-module-provider-registration",
			Passed:   true,
			Required: true,
			Command:  "osh-module-provider-registration (internal)",
			Stdout:   "no IModuleProvider implementations found",
			Duration: "0s",
		}
	}

	registered, err := readServiceLoaderEntries(filepath.Join(workDir, filepath.FromSlash(oshModuleProviderServicePath)))
	if err != nil {
		return payloads.CheckResult{
			Name:     "osh-module-provider-registration",
			Passed:   false,
			Required: true,
			Command:  "osh-module-provider-registration (internal)",
			Stderr:   fmt.Sprintf("found IModuleProvider implementation(s) %s but missing/read-failed %s: %v", strings.Join(providers, ", "), oshModuleProviderServicePath, err),
			Duration: "0s",
		}
	}

	var missing []string
	for _, provider := range providers {
		if !registered[provider] {
			missing = append(missing, provider)
		}
	}
	if len(missing) > 0 {
		return payloads.CheckResult{
			Name:     "osh-module-provider-registration",
			Passed:   false,
			Required: true,
			Command:  "osh-module-provider-registration (internal)",
			Stderr:   fmt.Sprintf("%s missing provider registration(s): %s", oshModuleProviderServicePath, strings.Join(missing, ", ")),
			Duration: "0s",
		}
	}

	return payloads.CheckResult{
		Name:     "osh-module-provider-registration",
		Passed:   true,
		Required: true,
		Command:  "osh-module-provider-registration (internal)",
		Stdout:   fmt.Sprintf("%d IModuleProvider implementation(s) registered", len(providers)),
		Duration: "0s",
	}
}

func mainJavaFilesToInspect(workDir string, filesModified []string) []string {
	if len(filesModified) == 0 {
		var out []string
		root := filepath.Join(workDir, "src", "main", "java")
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".java") {
				return nil
			}
			rel, relErr := filepath.Rel(workDir, path)
			if relErr == nil {
				out = append(out, filepath.ToSlash(rel))
			}
			return nil
		})
		sort.Strings(out)
		return out
	}
	var out []string
	seen := map[string]struct{}{}
	for _, file := range workflow.NormalizeFilePaths(filesModified) {
		if !strings.HasPrefix(file, "src/main/java/") || !strings.HasSuffix(file, ".java") {
			continue
		}
		if _, ok := seen[file]; ok {
			continue
		}
		seen[file] = struct{}{}
		out = append(out, file)
	}
	sort.Strings(out)
	return out
}

func findOSHModuleProviders(workDir string) ([]string, error) {
	root := filepath.Join(workDir, "src", "main", "java")
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var providers []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".java") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(data)
		if !javaModuleProviderRe.MatchString(content) {
			return nil
		}
		className := firstSubmatch(javaClassNameRe, content)
		if className == "" {
			return nil
		}
		pkg := firstSubmatch(javaPackageRe, content)
		if pkg != "" {
			providers = append(providers, pkg+"."+className)
			return nil
		}
		providers = append(providers, className)
		return nil
	})
	sort.Strings(providers)
	return providers, err
}

func readServiceLoaderEntries(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	return out, nil
}

func firstSubmatch(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func limitStrings(in []string, limit int) []string {
	if len(in) <= limit {
		return in
	}
	out := append([]string(nil), in[:limit]...)
	out = append(out, fmt.Sprintf("... %d more", len(in)-limit))
	return out
}
