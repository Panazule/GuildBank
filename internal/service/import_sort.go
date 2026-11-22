package service

import (
	"strings"
	"unicode"
)

// NaturalSortKey implements the persisted natural sort key described by
// DatabaseDesign.md §9.5. Text is lower-cased, non-alphanumeric runs collapse
// to one space, numeric runs are zero-padded to ten digits, and runs are joined
// with ASCII 0x01 so natural order is also byte order.
func NaturalSortKey(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	inSpace := false
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			inSpace = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			inSpace = false
		default:
			r = unicode.ToLower(r)
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
				inSpace = false
				continue
			}
			if !inSpace && b.Len() > 0 {
				b.WriteByte(' ')
				inSpace = true
			}
		}
	}
	normalized := strings.TrimSpace(b.String())
	if normalized == "" {
		return ""
	}

	runs := strings.Split(normalized, " ")
	out := make([]string, 0, len(runs))
	for _, run := range runs {
		if run == "" {
			continue
		}
		if isASCIIDigits(run) {
			if len(run) < 10 {
				run = strings.Repeat("0", 10-len(run)) + run
			}
		}
		out = append(out, run)
	}
	return strings.Join(out, "\x01")
}

func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// TruncateRunes bounds a persisted sort key at a rune boundary.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
