package structuralvalidator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

func TestCheckJavaImplementationCompleteness_FailsPlaceholderMarkers(t *testing.T) {
	dir := t.TempDir()
	rel := "src/main/java/org/acme/MavlinkDirectHandler.java"
	writeFile(t, dir, rel, `package org.acme;

public class MavlinkDirectHandler {
    // Dummy checksum until wire encoding is implemented.
    public int checksum() { return 0; }
}
`)

	got := CheckJavaImplementationCompleteness(dir, []string{rel})
	if got.Passed {
		t.Fatalf("Passed = true, want false for placeholder marker")
	}
	if !strings.Contains(got.Stderr, "Dummy checksum") {
		t.Fatalf("Stderr = %q, want marker evidence", got.Stderr)
	}
}

func TestCheckOSHModuleProviderRegistration_RequiresServiceLoaderEntry(t *testing.T) {
	dir := t.TempDir()
	provider := "src/main/java/org/sensorhub/impl/sensor/mavsdk/MavsdkSensorDescriptor.java"
	writeFile(t, dir, provider, `package org.sensorhub.impl.sensor.mavsdk;

import org.sensorhub.api.module.IModuleProvider;

public class MavsdkSensorDescriptor implements IModuleProvider {
}
`)

	missing := CheckOSHModuleProviderRegistration(dir)
	if missing.Passed {
		t.Fatalf("Passed = true, want false with no service-loader file")
	}
	if !strings.Contains(missing.Stderr, "org.sensorhub.impl.sensor.mavsdk.MavsdkSensorDescriptor") {
		t.Fatalf("Stderr = %q, want provider class evidence", missing.Stderr)
	}

	writeFile(t, dir, oshModuleProviderServicePath, "org.sensorhub.impl.sensor.mavsdk.MavsdkSensorDescriptor\n")
	registered := CheckOSHModuleProviderRegistration(dir)
	if !registered.Passed {
		t.Fatalf("Passed = false after service-loader registration: %+v", registered)
	}
}

func TestExecute_RunsJavaQualityChecks(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{Version: "1"})
	rel := "src/main/java/org/acme/ControlMapper.java"
	writeFile(t, dir, rel, `package org.acme;

public class ControlMapper {
    // Not implemented: command status mapping.
}
`)

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "java-quality",
		FilesModified: []string{rel},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Passed {
		t.Fatalf("Passed = true, want false when Java completeness check fails")
	}
	found := false
	for _, check := range result.CheckResults {
		if check.Name == "java-implementation-completeness" {
			found = true
			if check.Passed {
				t.Fatalf("java-implementation-completeness passed unexpectedly: %+v", check)
			}
		}
	}
	if !found {
		t.Fatalf("java-implementation-completeness did not run; results=%+v", result.CheckResults)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
