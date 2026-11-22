package service

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	ImportMaxBytes    = 10 << 20
	ImportMaxLines    = 50_000
	ImportMaxLineSize = 64 << 10
	ImportMaxRows     = 100_000
	ImportMaxQuantity = 10_000
	ImportMaxName     = 128
	ImportMaxLocation = 128
)

var canonicalHeader = []string{"Location", "Name", "ID", "Count", "Slots"}

// ParseError is a fatal, line-numbered import failure. The caller persists the
// message in ImportBatch.FailureReason.
type ParseError struct {
	Code    string
	Line    int
	Field   string
	Message string
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s — %s", e.Line, e.Code, e.Message)
	}
	return fmt.Sprintf("%s — %s", e.Code, e.Message)
}

// ParsedRow is one accepted physical data row before normalization/metadata.
type ParsedRow struct {
	SourceLine int
	Location   string
	Name       string
	EQUID      uint64
	Quantity   uint32
}

// ParseResult carries accepted rows and the counters that explain every
// physical line that did not become a row.
type ParseResult struct {
	Rows           []ParsedRow
	SourceLines    int
	StructuralRows int
	DiscardedRows  int
	BlankRows      int
}

// ParseImport reads the canonical tab-delimited InventoryExport format.
func ParseImport(body []byte) (ParseResult, error) {
	if len(body) > ImportMaxBytes {
		return ParseResult{}, &ParseError{Code: "limit_exceeded", Message: "file exceeds 10 MiB"}
	}
	if !utf8.Valid(body) {
		return ParseResult{}, &ParseError{Code: "invalid_utf8", Message: "file is not valid UTF-8"}
	}

	lines := splitPhysicalLines(body)
	if len(lines) > ImportMaxLines {
		return ParseResult{}, &ParseError{Code: "limit_exceeded", Message: "file exceeds 50,000 lines"}
	}
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	if len(lines) > 0 {
		lines[0] = strings.TrimPrefix(lines[0], "\ufeff")
	}

	result := ParseResult{SourceLines: len(lines)}
	headerSeen := false
	for index, line := range lines {
		lineNo := index + 1
		if len(line) > ImportMaxLineSize {
			return ParseResult{}, &ParseError{Code: "limit_exceeded", Line: lineNo, Message: "line exceeds 64 KiB"}
		}
		if strings.TrimSpace(line) == "" {
			result.BlankRows++
			continue
		}
		fields := splitTSV(line)
		if isCanonicalHeader(fields) {
			if headerSeen {
				return ParseResult{}, &ParseError{Code: "duplicate_header", Line: lineNo, Message: "canonical header appears more than once"}
			}
			headerSeen = true
			result.StructuralRows++
			continue
		}
		if !headerSeen {
			return ParseResult{}, &ParseError{Code: "invalid_header", Line: lineNo, Message: "first non-blank line must be Location, Name, ID, Count, Slots"}
		}
		if len(fields) < 3 {
			return ParseResult{}, &ParseError{Code: "too_few_fields", Line: lineNo, Field: "row", Message: "a data row must have at least 3 fields"}
		}
		if len(fields) > 5 {
			return ParseResult{}, &ParseError{Code: "too_many_fields", Line: lineNo, Field: "row", Message: "a data row must have at most 5 fields"}
		}
		if fields[1] == "Name" && !positiveDigits(fields[2]) {
			result.StructuralRows++
			continue
		}
		id := strings.TrimSpace(fields[2])
		name := strings.TrimSpace(fields[1])
		if id == "" || !digitsOnly(id) || name == "" {
			result.DiscardedRows++
			continue
		}
		equid, err := strconv.ParseUint(id, 10, 32)
		if err != nil || equid == 0 {
			if err != nil && digitsOnly(id) {
				return ParseResult{}, &ParseError{Code: "invalid_id", Line: lineNo, Field: "ID", Message: "ID exceeds the 32-bit item identifier range"}
			}
			result.DiscardedRows++
			continue
		}
		if utf8.RuneCountInString(name) > ImportMaxName {
			return ParseResult{}, &ParseError{Code: "field_too_long", Line: lineNo, Field: "Name", Message: "Name exceeds 128 characters"}
		}
		location := strings.TrimSpace(fields[0])
		if utf8.RuneCountInString(location) > ImportMaxLocation {
			return ParseResult{}, &ParseError{Code: "field_too_long", Line: lineNo, Field: "Location", Message: "Location exceeds 128 characters"}
		}
		quantity := uint32(1)
		if len(fields) == 5 {
			count := strings.TrimSpace(fields[3])
			if !digitsOnly(count) {
				return ParseResult{}, &ParseError{Code: "invalid_quantity", Line: lineNo, Field: "Count", Message: fmt.Sprintf("Count must be a positive whole number (got %q)", quoteValue(count))}
			}
			parsed, err := strconv.ParseUint(count, 10, 32)
			if err != nil || parsed < 1 || parsed > ImportMaxQuantity {
				return ParseResult{}, &ParseError{Code: "invalid_quantity", Line: lineNo, Field: "Count", Message: fmt.Sprintf("Count must be between 1 and %d (got %q)", ImportMaxQuantity, quoteValue(count))}
			}
			quantity = uint32(parsed)
		}
		result.Rows = append(result.Rows, ParsedRow{
			SourceLine: lineNo, Location: location, Name: name,
			EQUID: equid, Quantity: quantity,
		})
	}
	if !headerSeen {
		if len(result.Rows) == 0 && result.BlankRows == result.SourceLines {
			return ParseResult{}, &ParseError{Code: "file_empty", Message: "file contains no importable rows"}
		}
		return ParseResult{}, &ParseError{Code: "invalid_header", Line: 1, Message: "canonical header is missing"}
	}
	if len(result.Rows) == 0 {
		return ParseResult{}, &ParseError{Code: "file_empty", Message: "file contains no importable rows"}
	}
	return result, nil
}

func splitPhysicalLines(body []byte) []string {
	if len(body) == 0 {
		return nil
	}
	text := string(body)
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// splitTSV preserves interior empty fields but drops trailing empty fields, as
// the supplied export uses a trailing tab for an empty slot marker.
func splitTSV(line string) []string {
	fields := strings.Split(line, "\t")
	for len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	return fields
}

func isCanonicalHeader(fields []string) bool {
	if len(fields) != len(canonicalHeader) {
		return false
	}
	for i := range canonicalHeader {
		if fields[i] != canonicalHeader[i] {
			return false
		}
	}
	return true
}

func digitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func positiveDigits(value string) bool {
	if !digitsOnly(value) {
		return false
	}
	value = strings.TrimLeft(value, "0")
	return value != ""
}

func quoteValue(value string) string {
	value = strings.ToValidUTF8(value, "")
	runes := []rune(value)
	if len(runes) > 40 {
		value = string(runes[:40]) + "…"
	}
	return strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " ")
}
