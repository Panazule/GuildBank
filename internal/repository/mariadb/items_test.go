package mariadb

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

func validText(value string) sql.NullString {
	return sql.NullString{String: value, Valid: true}
}

func TestItemFromValuesMapsPhysicalColumnsToLogicalContract(t *testing.T) {
	tests := []struct {
		name        string
		id          int64
		itemName    sql.NullString
		classes     sql.NullString
		races       sql.NullString
		wantEQUID   uint64
		wantName    string
		wantClasses uint64
		wantRaces   uint64
	}{
		{
			name:        "Blade of Carnage",
			id:          25210,
			itemName:    validText("Blade of Carnage"),
			classes:     validText("157"),
			races:       validText("65535"),
			wantEQUID:   25210,
			wantName:    "Blade of Carnage",
			wantClasses: 157,
			wantRaces:   65535,
		},
		{
			name:        "Flayed Skin Cap",
			id:          10802,
			itemName:    validText("Flayed Skin Cap"),
			classes:     validText("1040"),
			races:       validText("5"),
			wantEQUID:   10802,
			wantName:    "Flayed Skin Cap",
			wantClasses: 1040,
			wantRaces:   5,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := itemFromValues(tc.id, tc.itemName, tc.classes, tc.races)
			if err != nil {
				t.Fatalf("itemFromValues: %v", err)
			}
			want := domain.Item{
				EQUID:     tc.wantEQUID,
				Name:      tc.wantName,
				ClassList: tc.wantClasses,
				RaceList:  tc.wantRaces,
			}
			if got != want {
				t.Fatalf("item = %+v, want %+v", got, want)
			}
		})
	}
}

func TestItemFromValuesTreatsBlankMasksAsZero(t *testing.T) {
	got, err := itemFromValues(1, validText("Item"), sql.NullString{}, validText("  "))
	if err != nil {
		t.Fatalf("itemFromValues: %v", err)
	}
	if got.ClassList != 0 || got.RaceList != 0 {
		t.Fatalf("masks = (%d, %d), want (0, 0)", got.ClassList, got.RaceList)
	}
}

func TestItemFromValuesRejectsInvalidSourceValues(t *testing.T) {
	tests := []struct {
		name  string
		id    int64
		item  sql.NullString
		class sql.NullString
		race  sql.NullString
		want  string
	}{
		{name: "negative id", id: -1, item: validText("Item"), class: validText("1"), race: validText("1"), want: "negative"},
		{name: "missing name", id: 1, item: sql.NullString{}, class: validText("1"), race: validText("1"), want: "no name"},
		{name: "malformed classes", id: 1, item: validText("Item"), class: validText("12x"), race: validText("1"), want: "classes"},
		{name: "malformed races", id: 1, item: validText("Item"), class: validText("1"), race: validText("x12"), want: "races"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := itemFromValues(tc.id, tc.item, tc.class, tc.race)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

type errorScanner struct{ err error }

func (s errorScanner) Scan(...any) error { return s.err }

func TestScanItemTranslatesNoRows(t *testing.T) {
	_, err := scanItem(errorScanner{err: sql.ErrNoRows})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}
