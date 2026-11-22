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

// NormalizeLocation applies D-05. LocationGroup is the authoritative selector
// value; LocationRaw is retained by the caller only for display/audit.
func NormalizeLocation(raw string) NormalizedLocation {
	value := strings.Join(strings.Fields(raw), " ")
	value = locationSlotSuffix.ReplaceAllString(value, "")
	value = strings.TrimSpace(value)

	if value == "" {
		return NormalizedLocation{
			Group: "Unspecified",
			Sort:  NaturalSortKey("Unspecified"),
			Warnings: []domain.ImportWarning{{
				Code: "location_blank", Field: "Location",
				Detail: "raw location was empty; grouped as Unspecified",
			}},
		}
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
		return NormalizedLocation{
			Group: "Unspecified",
			Sort:  NaturalSortKey("Unspecified"),
			Warnings: []domain.ImportWarning{{
				Code: "location_pattern_invalid", Field: "Location",
				Detail: "location begins with a reserved bank prefix but does not match its pattern",
			}},
		}
	default:
		group = "Unspecified"
	}

	if len([]rune(group)) > 64 {
		return NormalizedLocation{
			Group: "Unspecified",
			Sort:  NaturalSortKey("Unspecified"),
			Warnings: []domain.ImportWarning{{
				Code: "location_pattern_invalid", Field: "Location",
				Detail: "normalized location exceeds 64 characters",
			}},
		}
	}
	return NormalizedLocation{Group: group, Sort: NaturalSortKey(group)}
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
