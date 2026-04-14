//go:build integration

package workflow

import (
	"context"
	"testing"
)

func TestSaveScenarios_InvalidSlug(t *testing.T) {
	err := SaveScenarios(context.Background(), nil, []Scenario{}, "invalid slug!")
	if err == nil {
		t.Error("SaveScenarios() with invalid slug should return error")
	}
}
