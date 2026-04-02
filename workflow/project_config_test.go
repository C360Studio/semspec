package workflow

import "testing"

func TestStandards_ForRole(t *testing.T) {
	tests := []struct {
		name    string
		items   []Standard
		role    string
		wantIDs []string
	}{
		{
			name:    "empty roles matches all",
			items:   []Standard{{ID: "global", Roles: nil}},
			role:    "developer",
			wantIDs: []string{"global"},
		},
		{
			name:    "specific role match",
			items:   []Standard{{ID: "dev-only", Roles: []string{"developer"}}},
			role:    "developer",
			wantIDs: []string{"dev-only"},
		},
		{
			name:    "specific role no match",
			items:   []Standard{{ID: "reviewer-only", Roles: []string{"reviewer"}}},
			role:    "developer",
			wantIDs: nil,
		},
		{
			name: "mixed items filtered by role",
			items: []Standard{
				{ID: "global", Roles: nil},
				{ID: "dev-only", Roles: []string{"developer"}},
				{ID: "reviewer-only", Roles: []string{"reviewer"}},
			},
			role:    "developer",
			wantIDs: []string{"global", "dev-only"},
		},
		{
			name:    "empty items returns nil",
			items:   nil,
			role:    "developer",
			wantIDs: nil,
		},
		{
			name:    "multi-role standard matches any listed role",
			items:   []Standard{{ID: "shared", Roles: []string{"developer", "plan-reviewer"}}},
			role:    "plan-reviewer",
			wantIDs: []string{"shared"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Standards{Items: tt.items}
			got := s.ForRole(tt.role)

			if len(got) != len(tt.wantIDs) {
				t.Fatalf("ForRole(%q) returned %d items, want %d", tt.role, len(got), len(tt.wantIDs))
			}
			for i, want := range tt.wantIDs {
				if got[i].ID != want {
					t.Errorf("ForRole(%q)[%d].ID = %q, want %q", tt.role, i, got[i].ID, want)
				}
			}
		})
	}
}
