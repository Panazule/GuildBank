package service

import (
	"regexp"
	"strings"

	"github.com/Panazule/guildbank/internal/domain"
)

var (
	locationSlotSuffix = regexp.MustCompile(`(?i)\s*-\s*Slot\s*\d+$`)
	locationGeneral    = regexp.MustCompile(`(?i)^General\s*(\d+)$`)
	locationBank       = regexp.MustCompile(`(?i)^Bank\s*(\d+)$`)
	locationSharedBank = regexp.MustCompile(`(?i)^SharedBank\s*(\d+)$`)
	locationHoard      = regexp.MustCompile(`(?i)^Hoard(?:\s*\d+)?$`)
	reservedPrefix     = regexp.MustCompile(`(?i)^(General|Bank|SharedBank|Hoard)`)
	spellPrefix        = regexp.MustCompile(`(?i)^(Spell|Song|Ancient)\s*:`)
)

// NormalizedLocation is the canonical selector group for one raw location.
type NormalizedLocation struct {
	Group    string
	Sort     string
	Warnings []domain.ImportWarning
}

// locationProblem classifies why a raw location did not produce a recognized
// group. An empty problem means the value is recognized, or is a non-reserved
// string that legitimately groups as Unspecified.
type locationProblem string

const (
	locationProblemBlank    locationProblem = "blank"
	locationProblemPattern  locationProblem = "pattern_invalid"
	locationProblemOverlong locationProblem = "overlong"
)

// normalizeLocationValue is the single D-05 computation used by both wrappers.
// It returns the canonical group and, when applicable, the reason the raw value
// could not be grouped.
func normalizeLocationValue(raw string) (string, locationProblem) {
	value := strings.Join(strings.Fields(raw), " ")
	value = locationSlotSuffix.ReplaceAllString(value, "")
	value = strings.TrimSpace(value)

	if value == "" {
		return "Unspecified", locationProblemBlank
	}

	group := ""
	switch {
	case locationGeneral.MatchString(value):
		group = "General " + normalizeDigits(locationGeneral.FindStringSubmatch(value)[1])
	case locationBank.MatchString(value):
		group = "Bank " + normalizeDigits(locationBank.FindStringSubmatch(value)[1])
	case locationSharedBank.MatchString(value):
		group = "SharedBank " + normalizeDigits(locationSharedBank.FindStringSubmatch(value)[1])
	case locationHoard.MatchString(value):
		group = "Hoard"
	case reservedPrefix.MatchString(value):
		return "Unspecified", locationProblemPattern
	default:
		group = "Unspecified"
	}

	if len([]rune(group)) > 64 {
		return "Unspecified", locationProblemOverlong
	}
	return group, ""
}

// NormalizeLocation applies D-05. LocationGroup is the authoritative selector
// value; LocationRaw is retained by the caller only for display/audit.
func NormalizeLocation(raw string) NormalizedLocation {
	group, problem := normalizeLocationValue(raw)
	switch problem {
	case locationProblemBlank:
		return NormalizedLocation{
			Group: group,
			Sort:  NaturalSortKey(group),
			Warnings: []domain.ImportWarning{{
				Code: "location_blank", Field: "Location",
				Detail: "raw location was empty; grouped as Unspecified",
			}},
		}
	case locationProblemPattern:
		return NormalizedLocation{
			Group: group,
			Sort:  NaturalSortKey(group),
			Warnings: []domain.ImportWarning{{
				Code: "location_pattern_invalid", Field: "Location",
				Detail: "location begins with a reserved bank prefix but does not match its pattern",
			}},
		}
	case locationProblemOverlong:
		return NormalizedLocation{
			Group: group,
			Sort:  NaturalSortKey(group),
			Warnings: []domain.ImportWarning{{
				Code: "location_pattern_invalid", Field: "Location",
				Detail: "normalized location exceeds 64 characters",
			}},
		}
	default:
		return NormalizedLocation{Group: group, Sort: NaturalSortKey(group)}
	}
}

// NormalizeAdminLocation returns the same group and sort as NormalizeLocation
// but reports admin-facing warnings: a blank value is expected, while any
// non-blank value that still groups as Unspecified is surfaced once so the
// administrator knows the raw value was retained but not grouped.
func NormalizeAdminLocation(raw string) NormalizedLocation {
	group, problem := normalizeLocationValue(raw)
	normalized := NormalizedLocation{Group: group, Sort: NaturalSortKey(group)}
	if problem == locationProblemBlank || group != "Unspecified" {
		return normalized
	}
	normalized.Warnings = []domain.ImportWarning{{
		Code: "location_unrecognized", Field: "sourceLocation",
		Detail: "raw location was retained but does not match a known bank location; grouped as Unspecified",
	}}
	return normalized
}

func normalizeDigits(digits string) string {
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return "0"
	}
	return digits
}

// ClassifyItem applies the Spell/Song/Ancient prefix rule. Equality is checked
// before the colon; an item containing "Ancient" without a colon stays Item.
func ClassifyItem(name string) domain.ItemType {
	if spellPrefix.MatchString(strings.TrimSpace(name)) {
		return domain.ItemTypeSpell
	}
	return domain.ItemTypeItem
}
