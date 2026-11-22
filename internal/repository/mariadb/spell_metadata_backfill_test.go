package mariadb

import (
	"database/sql"
	"testing"

	"github.com/Panazule/guildbank/internal/service"
)

func TestRepairedSpellMetadata(t *testing.T) {
	classes := service.NewClassMaskDecoder()
	races := service.NewRaceMaskDecoder()
	tests := []struct {
		name       string
		current    string
		currentRc  string
		classMask  sql.NullString
		raceMask   sql.NullString
		wantClass  string
		wantRace   string
		wantChange bool
		wantErr    bool
	}{
		{
			name: "fills blank spell metadata", current: "", currentRc: "",
			classMask: validText("128"), raceMask: validText("65535"),
			wantClass: "BRD", wantRace: "ALL", wantChange: true,
		},
		{
			name: "keeps populated metadata", current: "BRD", currentRc: "ALL",
			classMask: validText("128"), raceMask: validText("65535"),
			wantClass: "BRD", wantRace: "ALL", wantChange: false,
		},
		{
			name: "fills only the blank column", current: "BRD", currentRc: "",
			classMask: validText("8"), raceMask: validText("5"),
			wantClass: "BRD", wantRace: "Human,Erudite", wantChange: true,
		},
		{
			name: "never blanks a zero mask", current: "", currentRc: "",
			classMask: validText("0"), raceMask: validText("0"),
			wantClass: "", wantRace: "", wantChange: false,
		},
		{
			name: "rejects a malformed mask", current: "", currentRc: "",
			classMask: validText("not-a-number"), raceMask: validText("1"),
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			classString, raceString, changed, err := repairedSpellMetadata(
				tc.current, tc.currentRc, tc.classMask, tc.raceMask, classes, races)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("repairedSpellMetadata: %v", err)
			}
			if classString != tc.wantClass || raceString != tc.wantRace || changed != tc.wantChange {
				t.Fatalf("got (%q, %q, %v), want (%q, %q, %v)",
					classString, raceString, changed, tc.wantClass, tc.wantRace, tc.wantChange)
			}
		})
	}
}
