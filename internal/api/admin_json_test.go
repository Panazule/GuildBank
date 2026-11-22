package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

func TestRejectDuplicateJSONKeys(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"simple", `{"a":1,"b":2}`, false},
		{"duplicate top-level", `{"a":1,"a":2}`, true},
		{"duplicate nested", `{"outer":{"a":1,"a":2}}`, true},
		{"array values", `{"items":[{"id":1},{"id":2}]}`, false},
		{"array of strings", `{"tags":["a","a"]}`, false},
		{"string values repeated", `{"a":"x","b":"x"}`, false},
		{"duplicate after array", `{"items":[1,2],"items":[3]}`, true},
		{"malformed", `{"a":`, true},
	}
	for _, c := range cases {
		err := rejectDuplicateJSONKeys([]byte(c.body))
		if (err != nil) != c.wantErr {
			t.Fatalf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}

func TestDecimalUintAcceptsNumbersAndStringsOnly(t *testing.T) {
	cases := []struct {
		raw   string
		value uint64
		set   bool
		fail  bool
	}{
		{`28813`, 28813, true, false},
		{`"28813"`, 28813, true, false},
		{`null`, 0, false, false},
		{`-1`, 0, false, true},
		{`1.5`, 0, false, true},
		{`1e3`, 0, false, true},
		{`" 12"`, 0, false, true},
		{`"12a"`, 0, false, true},
		{`""`, 0, false, true},
	}
	for _, c := range cases {
		var value decimalUint
		err := json.Unmarshal([]byte(c.raw), &value)
		if (err != nil) != c.fail {
			t.Fatalf("%s: err = %v, want fail %v", c.raw, err, c.fail)
		}
		if !c.fail && (value.Value != c.value || value.Set != c.set) {
			t.Fatalf("%s: value = %+v", c.raw, value)
		}
	}
}

func TestParseOptionalDateContract(t *testing.T) {
	fields := make(map[string]string)
	if got := parseOptionalDate("dateOpen", nil, fields); got.Set || len(fields) != 0 {
		t.Fatalf("absent key = %+v fields %v", got, fields)
	}
	raw := json.RawMessage(`null`)
	if got := parseOptionalDate("dateOpen", &raw, fields); !got.Set || got.Value != nil {
		t.Fatalf("null key = %+v", got)
	}
	raw = json.RawMessage(`"2026-12-05"`)
	if got := parseOptionalDate("dateOpen", &raw, fields); !got.Set || got.Value == nil ||
		got.Value.Format("2006-01-02") != "2026-12-05" {
		t.Fatalf("date key = %+v", got)
	}
	for _, bad := range []string{`""`, `"2026-12-05T00:00:00Z"`, `"999-01-01"`, `"1000-01-01 "`, `12`} {
		raw = json.RawMessage(bad)
		candidate := make(map[string]string)
		parseOptionalDate("dateBazaar", &raw, candidate)
		if candidate["dateBazaar"] == "" {
			t.Fatalf("%s was accepted", bad)
		}
	}
	if raw = json.RawMessage(`"1000-01-01"`); func() bool {
		got := parseOptionalDate("dateOpen", &raw, fields)
		return got.Value != nil && got.Value.Format("2006-01-02") == "1000-01-01"
	}() == false {
		t.Fatal("1000-01-01 was rejected")
	}
}

func TestAvailabilityLabels(t *testing.T) {
	cases := map[domain.AvailabilityState]string{
		domain.AvailabilityUpcoming: "Mains",
		domain.AvailabilityOpen:     "Persona",
		domain.AvailabilityBazaar:   "Bazaar",
	}
	for state, want := range cases {
		if got := availabilityLabel(state); got != want {
			t.Fatalf("availabilityLabel(%s) = %s, want %s", state, got, want)
		}
	}
}

func TestAvailabilityOwnerBoundariesAsLabels(t *testing.T) {
	dateOpen := time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)
	dateBazaar := time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		today time.Time
		want  string
	}{
		{time.Date(2026, 12, 4, 23, 59, 0, 0, time.UTC), "Mains"},
		{time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC), "Persona"},
		{time.Date(2026, 12, 20, 23, 59, 0, 0, time.UTC), "Persona"},
		{time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC), "Bazaar"},
		{time.Date(2026, 12, 21, 23, 59, 0, 0, time.UTC), "Bazaar"},
		{time.Date(2026, 12, 22, 0, 0, 0, 0, time.UTC), "Bazaar"},
	}
	for _, tc := range cases {
		state := service.Availability(tc.today, dateOpen, dateBazaar)
		if got := availabilityLabel(state); got != tc.want {
			t.Fatalf("label on %s = %s (%s), want %s", tc.today.Format("2006-01-02"), got, state, tc.want)
		}
	}
	// Equal open and bazaar dates leave no Persona interval (PD5-40).
	if got := availabilityLabel(service.Availability(dateOpen, dateOpen, dateOpen)); got != "Bazaar" {
		t.Fatalf("equal-date label = %s, want Bazaar", got)
	}
}

func TestImmutableFieldsReportsFirstField(t *testing.T) {
	raw := json.RawMessage(`1`)
	fields := immutableFields([]struct {
		name string
		raw  *json.RawMessage
	}{{"equid", nil}, {"dateAdded", &raw}})
	if fields["dateAdded"] == "" || len(fields) != 1 {
		t.Fatalf("fields = %v", fields)
	}
}
