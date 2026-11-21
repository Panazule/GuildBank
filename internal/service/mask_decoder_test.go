package service

import (
	"strings"
	"testing"
)

func TestClassMaskDecoderApprovedFixtures(t *testing.T) {
	decoder := NewClassMaskDecoder()

	tests := []struct {
		name string
		mask uint64
		want string
	}{
		{name: "Blade of Carnage", mask: 157, want: "WAR,PAL,RNG,SHD,BRD"},
		{name: "all classes", mask: 65535, want: "ALL"},
		{name: "blank mask", mask: 0, want: ""},
		{name: "Flayed Skin Cap", mask: 1040, want: "SHD,NEC"},
		{name: "single class", mask: 1, want: "WAR"},
		{name: "highest bit", mask: 32768, want: "BER"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decoder.Decode(tc.mask)
			if got.Value != tc.want {
				t.Fatalf("Decode(%d) = %q, want %q", tc.mask, got.Value, tc.want)
			}
			if len(got.Warnings) != 0 {
				t.Fatalf("Decode(%d) warnings = %+v, want none", tc.mask, got.Warnings)
			}
		})
	}
}

func TestRaceMaskDecoderApprovedFixtures(t *testing.T) {
	decoder := NewRaceMaskDecoder()

	tests := []struct {
		name string
		mask uint64
		want string
	}{
		{name: "Blade of Carnage", mask: 65535, want: "ALL"},
		{name: "blank mask", mask: 0, want: ""},
		{name: "Flayed Skin Cap", mask: 5, want: "Human,Erudite"},
		{name: "single race", mask: 1, want: "Human"},
		{name: "multi-word race", mask: 8192, want: "Vah Shir"},
		{name: "highest bit", mask: 32768, want: "Drakkin"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decoder.Decode(tc.mask)
			if got.Value != tc.want {
				t.Fatalf("Decode(%d) = %q, want %q", tc.mask, got.Value, tc.want)
			}
			if len(got.Warnings) != 0 {
				t.Fatalf("Decode(%d) warnings = %+v, want none", tc.mask, got.Warnings)
			}
		})
	}
}

func TestMaskDecodersWarnOnUndefinedHighBitsWithoutFailing(t *testing.T) {
	classDecoder := NewClassMaskDecoder()
	raceDecoder := NewRaceMaskDecoder()

	tests := []struct {
		name      string
		decoder   interface{ Decode(uint64) MaskDecodeResult }
		field     string
		mask      uint64
		wantValue string
		wantHigh  uint64
	}{
		{
			name:      "class high bit with defined low bits",
			decoder:   classDecoder,
			field:     "ClassList",
			mask:      uint64(1)<<16 | 157,
			wantValue: "WAR,PAL,RNG,SHD,BRD",
			wantHigh:  1,
		},
		{
			name:      "class high bits with blank low bits",
			decoder:   classDecoder,
			field:     "ClassList",
			mask:      uint64(1)<<16 | 1<<20,
			wantValue: "",
			wantHigh:  uint64(1)<<4 | 1,
		},
		{
			name:      "class high bits with ALL low bits",
			decoder:   classDecoder,
			field:     "ClassList",
			mask:      uint64(0x1_0000) | 65535,
			wantValue: "ALL",
			wantHigh:  1,
		},
		{
			name:      "race high bit with defined low bits",
			decoder:   raceDecoder,
			field:     "RaceList",
			mask:      uint64(1)<<16 | 5,
			wantValue: "Human,Erudite",
			wantHigh:  1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.decoder.Decode(tc.mask)
			if got.Value != tc.wantValue {
				t.Fatalf("Decode(%d) = %q, want %q", tc.mask, got.Value, tc.wantValue)
			}
			if len(got.Warnings) != 1 {
				t.Fatalf("Decode(%d) warnings = %+v, want exactly one", tc.mask, got.Warnings)
			}
			warning := got.Warnings[0]
			if warning.Code != MaskWarningUndefinedHighBits {
				t.Fatalf("warning code = %q, want %q", warning.Code, MaskWarningUndefinedHighBits)
			}
			if warning.Field != tc.field {
				t.Fatalf("warning field = %q, want %q", warning.Field, tc.field)
			}
			if warning.Mask != tc.mask {
				t.Fatalf("warning mask = %d, want %d", warning.Mask, tc.mask)
			}
			if warning.HighBits != tc.wantHigh {
				t.Fatalf("warning high bits = %d, want %d", warning.HighBits, tc.wantHigh)
			}
			if warning.Detail == "" {
				t.Fatal("warning detail is empty")
			}
		})
	}
}

func TestMaskDecoderDefinitions(t *testing.T) {
	if MaskDefinedBits != 16 {
		t.Fatalf("MaskDefinedBits = %d, want 16", MaskDefinedBits)
	}
	if AllMask != 65535 {
		t.Fatalf("AllMask = %d, want 65535", AllMask)
	}
	got := NewClassMaskDecoder().Decode(uint64(1)<<16 | 157)
	if !strings.Contains(got.Warnings[0].Detail, "undefined") {
		t.Fatalf("warning detail %q does not explain undefined bits", got.Warnings[0].Detail)
	}
}
