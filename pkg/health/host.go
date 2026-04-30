package health

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// CaptureHost reads runtime/debug build info plus runtime.GOOS/GOARCH
// to populate HostInfo. Pure: no I/O. The Ollama field is left nil; the
// caller fills it from CaptureOllama if applicable.
//
// fallbackVersion is used for SemspecVersion when build info is
// unreadable (e.g. `go run` without a module cache, or a stripped
// binary). Capture callers typically pass the linker-baked version.
func CaptureHost(fallbackVersion string) HostInfo {
	h := HostInfo{
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		SemspecVersion: fallbackVersion,
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return h
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		h.SemspecVersion = info.Main.Version
	}
	for _, dep := range info.Deps {
		if isSemstreamsModule(dep.Path) {
			h.SemstreamsVersion = dep.Version
			break
		}
	}
	return h
}

// isSemstreamsModule matches the semstreams Go module path. Kept as a
// helper so future module renames are a one-line change.
func isSemstreamsModule(path string) bool {
	return strings.HasSuffix(path, "/semstreams") ||
		strings.Contains(path, "/semstreams/")
}
