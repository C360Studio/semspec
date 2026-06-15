package structuralvalidator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/c360studio/semspec/workflow/payloads"
)

func checkGradleWrapperCompleteness(workDir string) payloads.CheckResult {
	gradlew := filepath.Join(workDir, "gradlew")
	if _, err := os.Stat(gradlew); err != nil {
		return payloads.CheckResult{
			Name:     "gradle-wrapper-completeness",
			Passed:   true,
			Required: true,
			Command:  "gradle-wrapper-completeness (internal)",
			Stdout:   "gradlew not present — wrapper completeness check skipped",
		}
	}

	required := []string{
		filepath.Join("gradle", "wrapper", "gradle-wrapper.properties"),
		filepath.Join("gradle", "wrapper", "gradle-wrapper.jar"),
	}
	var missing []string
	for _, rel := range required {
		if _, err := os.Stat(filepath.Join(workDir, rel)); err != nil {
			missing = append(missing, rel)
		}
	}
	if len(missing) == 0 {
		return payloads.CheckResult{
			Name:     "gradle-wrapper-completeness",
			Passed:   true,
			Required: true,
			Command:  "gradle-wrapper-completeness (internal)",
			Stdout:   "gradle wrapper files are complete",
		}
	}
	return payloads.CheckResult{
		Name:     "gradle-wrapper-completeness",
		Passed:   false,
		Required: true,
		Command:  "gradle-wrapper-completeness (internal)",
		ExitCode: 1,
		Stderr: fmt.Sprintf(
			"gradlew is present but required wrapper file(s) are missing: %s. The project cannot run ./gradlew test without these files.",
			strings.Join(missing, ", ")),
	}
}

func shouldCheckGradleWrapper(files []string, workDir string) bool {
	if _, err := os.Stat(filepath.Join(workDir, "gradlew")); err == nil {
		return true
	}
	for _, f := range files {
		clean := filepath.ToSlash(f)
		if clean == "gradlew" ||
			clean == "build.gradle" ||
			clean == "build.gradle.kts" ||
			strings.HasPrefix(clean, "gradle/wrapper/") {
			return true
		}
	}
	return false
}
