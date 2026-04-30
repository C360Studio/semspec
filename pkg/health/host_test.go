package health

import (
	"runtime"
	"testing"
)

func TestCaptureHost_FillsRuntimeFields(t *testing.T) {
	h := CaptureHost("v0.0.0-test")
	if h.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", h.OS, runtime.GOOS)
	}
	if h.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", h.Arch, runtime.GOARCH)
	}
	// SemspecVersion is either the build-info value or the fallback;
	// both are acceptable. The point is it's never empty.
	if h.SemspecVersion == "" {
		t.Error("SemspecVersion should never be empty (fallback supplied)")
	}
}

func TestCaptureHost_Ollama_NotPopulatedHere(t *testing.T) {
	// CaptureHost intentionally leaves Ollama nil; CaptureOllama is
	// the source of that field. This test pins the contract so a
	// future change can't quietly fold the two together.
	h := CaptureHost("v0.0.0-test")
	if h.Ollama != nil {
		t.Errorf("Ollama should be nil after CaptureHost; got %+v", h.Ollama)
	}
}

func TestLooksLikeSemstreamsModule(t *testing.T) {
	cases := map[string]bool{
		"github.com/c360studio/semstreams":         true,
		"github.com/c360studio/semstreams/pkg/foo": true,
		"github.com/example/semstreams":            true,
		"github.com/example/other":                 false,
		"":                                         false,
	}
	for path, want := range cases {
		if got := looksLikeSemstreamsModule(path); got != want {
			t.Errorf("looksLikeSemstreamsModule(%q) = %v, want %v", path, got, want)
		}
	}
}
