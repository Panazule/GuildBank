package service

import "testing"

func TestIsContainerLocation(t *testing.T) {
	cases := []struct {
		raw       string
		container bool
	}{
		{"General 1", true},
		{"General 1-Slot1", false},
		{"General 1 - Slot 12", false},
		{"Bank 4", true},
		{"Bank 4-Slot7", false},
		{"SharedBank 2-Slot1", false},
		{"Hoard 1", true},
		{"Hoard", true},
		{"Equipment", true},
		{"", true},
	}
	for _, tc := range cases {
		if got := IsContainerLocation(tc.raw); got != tc.container {
			t.Fatalf("IsContainerLocation(%q) = %v, want %v", tc.raw, got, tc.container)
		}
	}
}

func TestDefaultImportSelection(t *testing.T) {
	cases := []struct {
		group     string
		container bool
		want      bool
	}{
		{"General 1", true, false},
		{"General 1", false, true},
		{"Bank 4", true, false},
		{"Bank 4", false, true},
		{"SharedBank 2", true, false},
		{"SharedBank 2", false, true},
		// Hoard is the exception: every Hoard row is a container and every
		// Hoard row is selected by default (IMP-05).
		{"Hoard", true, true},
		{"Hoard", false, true},
		// Unspecified is never auto-selected (IMP-02).
		{"Unspecified", false, false},
		{"Unspecified", true, false},
		{"", false, false},
	}
	for _, tc := range cases {
		if got := DefaultImportSelection(tc.group, tc.container); got != tc.want {
			t.Fatalf("DefaultImportSelection(%q, %v) = %v, want %v", tc.group, tc.container, got, tc.want)
		}
	}
}
