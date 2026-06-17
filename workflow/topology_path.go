package workflow

import (
	"path"
	"strings"
)

// IsTopologyControlledPath reports whether p names a build, workspace, package,
// or standalone-project file whose creation changes repository topology.
func IsTopologyControlledPath(p string) bool {
	p = NormalizeFilePath(p)
	if p == "" {
		return false
	}
	base := path.Base(p)
	switch base {
	case "go.mod",
		"go.work",
		"package.json",
		"pnpm-workspace.yaml",
		"lerna.json",
		"nx.json",
		"pyproject.toml",
		"requirements.txt",
		"setup.py",
		"Pipfile",
		"Cargo.toml",
		"pom.xml",
		"build.gradle",
		"build.gradle.kts",
		"settings.gradle",
		"settings.gradle.kts",
		"composer.json",
		"Gemfile",
		"gradlew",
		"gradlew.bat":
		return true
	}
	if strings.HasSuffix(base, ".csproj") {
		return true
	}
	return p == "gradle/wrapper/gradle-wrapper.properties" ||
		strings.HasSuffix(p, "/gradle/wrapper/gradle-wrapper.properties")
}
