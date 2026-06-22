package workflow

import (
	"reflect"
	"testing"
)

func TestIsConcreteScopedFile(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"build.gradle", true},
		{"README.md", true},
		{"src/main/java/org/x/Foo.java", true},
		{"Dockerfile", true},
		{"Makefile", true},
		{"src", false},           // bare directory name
		{"src/", false},          // dir entry (NormalizeFilePath would strip the slash; still no ext)
		{"src/**/*.java", false}, // glob
		{"api/*.go", false},      // glob
		{"somedir", false},       // extensionless, not well-known
	}
	for _, c := range cases {
		if got := IsConcreteScopedFile(c.in); got != c.want {
			t.Errorf("IsConcreteScopedFile(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestUnownedScopedIncludeFiles(t *testing.T) {
	comp := func(name string, files ...string) ComponentDef {
		return ComponentDef{Name: name, ImplementationFiles: files}
	}
	tests := []struct {
		name       string
		scope      Scope
		components []ComponentDef
		want       []string
	}{
		{
			name:       "orphaned includes are flagged (sorted)",
			scope:      Scope{Include: []string{"build.gradle", "README.md"}},
			components: []ComponentDef{comp("core", "src/main/java/Foo.java")},
			want:       []string{"README.md", "build.gradle"},
		},
		{
			name:       "owned include passes",
			scope:      Scope{Include: []string{"build.gradle"}},
			components: []ComponentDef{comp("core", "build.gradle", "src/main/java/Foo.java")},
			want:       nil,
		},
		{
			name:       "directory include is exempt (concreteness)",
			scope:      Scope{Include: []string{"src/", "api/"}},
			components: []ComponentDef{comp("core", "src/main/java/Foo.java")},
			want:       nil,
		},
		{
			name:       "glob include is exempt (concreteness)",
			scope:      Scope{Include: []string{"src/**/*.java"}},
			components: []ComponentDef{comp("core", "src/main/java/Foo.java")},
			want:       nil,
		},
		{
			name:       "do_not_touch include is exempt (read-only reference)",
			scope:      Scope{Include: []string{"build.gradle"}, DoNotTouch: []string{"build.gradle"}},
			components: []ComponentDef{comp("core", "src/main/java/Foo.java")},
			want:       nil,
		},
		{
			name:       "shared ownership across components still counts as owned",
			scope:      Scope{Include: []string{"build.gradle"}},
			components: []ComponentDef{comp("a", "src/A.java"), comp("b", "build.gradle", "src/B.java")},
			want:       nil,
		},
		{
			name:       "duplicate include is reported once",
			scope:      Scope{Include: []string{"build.gradle", "build.gradle"}},
			components: []ComponentDef{comp("core", "src/main/java/Foo.java")},
			want:       []string{"build.gradle"},
		},
		{
			name:       "no components — every concrete include is unowned",
			scope:      Scope{Include: []string{"build.gradle", "src/"}},
			components: nil,
			want:       []string{"build.gradle"},
		},
		{
			// Load-bearing: a component owning Foo.java implicitly owns the
			// canonical companion test path via ExpandFileScopeWithCompanionTests,
			// so listing that test path in scope.include must NOT be flagged.
			name:       "companion test path of an owned source is owned",
			scope:      Scope{Include: []string{"src/test/java/org/x/FooTest.java"}},
			components: []ComponentDef{comp("core", "src/main/java/org/x/Foo.java")},
			want:       nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnownedScopedIncludeFiles(tt.scope, tt.components)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UnownedScopedIncludeFiles() = %v, want %v", got, tt.want)
			}
		})
	}
}
