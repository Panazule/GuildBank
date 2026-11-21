package service

import (
	"fmt"
	"strings"
)

const (
	// MaskDefinedBits is the width of the class and race masks defined by
	// Design.md L418-L504. Bits above this range are outside the contract.
	MaskDefinedBits = 16
	// AllMask is the defined mask value that means "all classes" or "all
	// races" (Design.md L455-L457 and L502-L504).
	AllMask = uint64(1)<<MaskDefinedBits - 1
	// MaskWarningUndefinedHighBits identifies a non-fatal mask warning.
	MaskWarningUndefinedHighBits = "undefined_high_bits"
)

// MaskWarning records a value that decoded successfully but violates the
// published mask contract. Decoders never fail solely because of this
// condition; future import and inventory workflows can forward the warning to
// their chosen sink (E-14).
type MaskWarning struct {
	Code     string
	Field    string
	Mask     uint64
	HighBits uint64
	Detail   string
}

// MaskDecodeResult is the decoded logical string plus any non-fatal warnings.
type MaskDecodeResult struct {
	Value    string
	Warnings []MaskWarning
}

// decodeMask applies the approved PD-20 rules: only the low 16 bits are
// decoded, 65535 means ALL and zero means a blank string. Undefined high bits
// are reported as a warning without discarding the defined low-bit result.
func decodeMask(mask uint64, labels []string, field string) MaskDecodeResult {
	result := MaskDecodeResult{}
	if high := mask >> MaskDefinedBits; high != 0 {
		result.Warnings = append(result.Warnings, MaskWarning{
			Code:     MaskWarningUndefinedHighBits,
			Field:    field,
			Mask:     mask,
			HighBits: high,
			Detail: fmt.Sprintf(
				"%s value %d uses undefined bits above bit %d; decoded low %d bits only",
				field, mask, MaskDefinedBits-1, MaskDefinedBits,
			),
		})
	}

	low := mask & AllMask
	switch low {
	case 0:
		return result
	case AllMask:
		result.Value = "ALL"
		return result
	}

	values := make([]string, 0, len(labels))
	for bit, label := range labels {
		if low&(uint64(1)<<uint(bit)) != 0 {
			values = append(values, label)
		}
	}
	result.Value = strings.Join(values, ",")
	return result
}
