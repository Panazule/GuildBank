package service

import "strings"

// raceMaskLabels is ordered by bit value so decoded strings follow the
// specification's stable order (Design.md L470-L500).
var raceMaskLabels = [...]string{
	"Human",     // 1
	"Barbarian", // 2
	"Erudite",   // 4
	"Wood Elf",  // 8
	"High Elf",  // 16
	"Dark Elf",  // 32
	"Half Elf",  // 64
	"Dwarf",     // 128
	"Troll",     // 256
	"Ogre",      // 512
	"Halfling",  // 1024
	"Gnome",     // 2048
	"Iksar",     // 4096
	"Vah Shir",  // 8192
	"Froglok",   // 16384
	"Drakkin",   // 32768
}

// RaceMaskDecoder converts a logical RaceList bitmask into the comma-separated
// race names stored in GuildBankItems.RaceString. It has no repository
// dependency and can be reused by import and inventory workflows.
type RaceMaskDecoder struct{}

// RaceValues returns a copy of the canonical race labels in decoder order.
// Inventory filter validation and UI select lists share this source.
func RaceValues() []string {
	return append([]string(nil), raceMaskLabels[:]...)
}

// CanonicalRace maps a case-insensitive race label to its canonical spelling,
// including labels with spaces such as "Wood Elf".
func CanonicalRace(value string) (string, bool) {
	for _, label := range raceMaskLabels {
		if strings.EqualFold(value, label) {
			return label, true
		}
	}
	return "", false
}

// NewRaceMaskDecoder builds the race-mask decoder.
func NewRaceMaskDecoder() *RaceMaskDecoder {
	return &RaceMaskDecoder{}
}

// Decode applies PD-19/PD-20 without failing on undefined high bits.
func (d *RaceMaskDecoder) Decode(mask uint64) MaskDecodeResult {
	return decodeMask(mask, raceMaskLabels[:], "RaceList")
}
