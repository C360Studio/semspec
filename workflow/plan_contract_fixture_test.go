package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadContractFixture(t *testing.T, name string) ContractPacket {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "contract", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var packet ContractPacket
	if err := json.Unmarshal(data, &packet); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
	return packet
}

func TestContractFixturesLoad(t *testing.T) {
	for _, name := range []string{
		"generic-brownfield.json",
		"mavlink-osh-clean-room-regression.json",
	} {
		t.Run(name, func(t *testing.T) {
			packet := loadContractFixture(t, name)
			if packet.ID == "" {
				t.Fatal("fixture missing ID")
			}
			if packet.Version != 1 {
				t.Fatalf("Version = %d, want 1", packet.Version)
			}
			if packet.Brief == "" {
				t.Fatal("fixture missing brief")
			}
			if len(packet.Constraints) == 0 {
				t.Fatal("fixture missing constraints")
			}
			if len(packet.TopologyFacts) == 0 {
				t.Fatal("fixture missing topology facts")
			}
		})
	}
}

func TestMavlinkOSHFixturePinsCleanRoomRegressionSignals(t *testing.T) {
	packet := loadContractFixture(t, "mavlink-osh-clean-room-regression.json")

	var hasCompositeSubstitution, hasForbiddenSettings bool
	for _, fact := range packet.TopologyFacts {
		switch {
		case fact.Kind == "composite_source_substitution":
			hasCompositeSubstitution = true
		case fact.Kind == "forbidden_file" && fact.Path == "settings.gradle":
			hasForbiddenSettings = true
		}
	}
	if !hasCompositeSubstitution {
		t.Fatal("fixture missing composite_source_substitution topology fact")
	}
	if !hasForbiddenSettings {
		t.Fatal("fixture missing forbidden settings.gradle topology fact")
	}
}
