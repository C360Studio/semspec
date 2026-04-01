//go:build integration

package workflow

import (
	"context"
	"testing"
)

func TestLoadScenarios_NilTripleWriter_ReturnsEmpty(t *testing.T) {
	got, err := LoadScenarios(context.Background(), nil, "any-plan")
	if err != nil {
		t.Fatalf("LoadScenarios() with nil tw should not error, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadScenarios() = %d items, want 0", len(got))
	}
}

func TestSaveScenarios_InvalidSlug(t *testing.T) {
	err := SaveScenarios(context.Background(), nil, []Scenario{}, "invalid slug!")
	if err == nil {
		t.Error("SaveScenarios() with invalid slug should return error")
	}
}

func TestLoadScenarios_InvalidSlug(t *testing.T) {
	_, err := LoadScenarios(context.Background(), nil, "invalid slug!")
	if err == nil {
		t.Error("LoadScenarios() with invalid slug should return error")
	}
}
