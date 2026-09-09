package file

import "testing"

func TestNormalizeMediaSelectionsCanonicalizesStableOrder(t *testing.T) {
	got, err := NormalizeMediaSelections([]MediaSelection{
		{ResourceID: " gallery ", SortOrder: 4},
		{ResourceID: "cover", SortOrder: 1, Role: MediaRoleCover},
		{ResourceID: "detail", SortOrder: 1},
	})
	if err != nil {
		t.Fatalf("NormalizeMediaSelections() error = %v", err)
	}
	want := []MediaSelection{
		{ResourceID: "cover", SortOrder: 0, Role: MediaRoleCover},
		{ResourceID: "detail", SortOrder: 1, Role: MediaRoleGallery},
		{ResourceID: "gallery", SortOrder: 2, Role: MediaRoleGallery},
	}
	if len(got) != len(want) {
		t.Fatalf("NormalizeMediaSelections() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("NormalizeMediaSelections()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestNormalizeMediaSelectionsRejectsDuplicateAndMultipleCovers(t *testing.T) {
	tests := []struct {
		name  string
		input []MediaSelection
	}{
		{name: "duplicate", input: []MediaSelection{{ResourceID: "res-1"}, {ResourceID: "res-1"}}},
		{name: "multiple covers", input: []MediaSelection{{ResourceID: "res-1", Role: MediaRoleCover}, {ResourceID: "res-2", Role: MediaRoleCover}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NormalizeMediaSelections(tt.input); err == nil {
				t.Fatal("NormalizeMediaSelections() error = nil, want validation error")
			}
		})
	}
}
