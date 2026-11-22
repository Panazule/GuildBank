package service

import (
	"regexp"
	"strings"
)

const (
	// UnspecifiedLocationGroup is the canonical D-05 group for a raw location
	// that is not a bank location. The persisted value never changes; the UI
	// presents it as "Other (not a bank location)" and never auto-selects it
	// (IMP-02).
	UnspecifiedLocationGroup = "Unspecified"
	// HoardLocationGroup is the single canonical AM-04 hoard group. Hoard rows
	// are a special location family: they are always selected by default, even
	// though a Hoard row has no slot suffix (IMP-05).
	HoardLocationGroup = "Hoard"
)

// trailingSlotSuffix matches one trailing "-Slot<digits>" segment. A raw
// location without one is a container location: the row describes the bag that
// occupies the bank slot rather than an item sitting in that slot.
var trailingSlotSuffix = regexp.MustCompile(`(?i)\s*-\s*Slot\s*\d+\s*$`)

// IsContainerLocation reports whether raw describes a container occupying a
// bank slot. The raw value is whitespace-normalized first so the classification
// does not depend on the collector's padding.
func IsContainerLocation(raw string) bool {
	value := strings.Join(strings.Fields(raw), " ")
	return !trailingSlotSuffix.MatchString(value)
}

// DefaultImportSelection is the single staging-time default-selection rule
// (IMP-04/IMP-05). It is applied only when a batch is staged; changing the rule
// never rewrites an existing batch's persisted selection.
//
//   - Unspecified ("Other"), the not-a-bank-location group, is never selected.
//   - Hoard is a special location family: every Hoard row is selected.
//   - Every other group selects slot contents but not the container row itself,
//     because the bag occupying the slot is not guild property to import.
func DefaultImportSelection(group string, container bool) bool {
	switch group {
	case "", UnspecifiedLocationGroup:
		return false
	case HoardLocationGroup:
		return true
	default:
		return !container
	}
}
