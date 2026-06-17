package qareviewer

import (
	"strings"

	"github.com/c360studio/semspec/workflow"
)

func classifyQAFailures(failures []workflow.QAFailure) []workflow.QAFailure {
	if len(failures) == 0 {
		return failures
	}
	out := append([]workflow.QAFailure(nil), failures...)
	for i := range out {
		if out[i].Category == "" {
			out[i].Category = classifyQAFailure(out[i])
		}
	}
	return out
}

func classifyQAFailure(f workflow.QAFailure) workflow.QAFailureCategory {
	text := strings.ToLower(strings.Join([]string{
		f.JobName,
		f.StepName,
		f.TestName,
		f.Message,
		f.LogExcerpt,
	}, "\n"))

	if containsAny(text,
		"standalone project",
		"clean-room",
		"clean room",
		"duplicate root",
		"root element",
		"composite build",
		"includebuild",
		"include build",
		"settings.gradle",
		"settings.gradle.kts",
		"gradle wrapper",
	) {
		return workflow.QAFailureCategoryTopology
	}

	if containsAny(text,
		"gradle build failed during configuration",
		"failed during configuration",
		"could not configure",
		"failed to configure",
		"could not resolve",
		"dependency resolution",
		"build.gradle",
		"build.gradle.kts",
		"pom.xml",
		"go.mod",
		"package.json",
		"npm install",
		"pnpm install",
		"maven",
	) {
		return workflow.QAFailureCategoryBuildConfig
	}

	if f.TestName != "" {
		return workflow.QAFailureCategoryTestFailure
	}
	return ""
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
