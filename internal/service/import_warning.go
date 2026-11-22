package service

import (
	"encoding/json"
	"fmt"

	"github.com/Panazule/guildbank/internal/domain"
)

const warningTextMax = 1000

// MarshalWarnings serializes ImportList.WarningText with a 1,000-character cap.
// metadata_missing is retained first; a truncation marker is appended when the
// full array does not fit.
func MarshalWarnings(warnings []domain.ImportWarning) ([]byte, error) {
	if len(warnings) == 0 {
		return nil, nil
	}
	ordered := append([]domain.ImportWarning(nil), warnings...)
	prioritizeWarnings(ordered)
	body, err := json.Marshal(ordered)
	if err != nil {
		return nil, fmt.Errorf("import: encode warnings: %w", err)
	}
	if len(body) <= warningTextMax {
		return body, nil
	}
	kept := make([]domain.ImportWarning, 0, len(ordered))
	for _, warning := range ordered {
		trial := append(append([]domain.ImportWarning(nil), kept...), warning)
		trial = append(trial, domain.ImportWarning{Code: "warnings_truncated"})
		candidate, _ := json.Marshal(trial)
		if len(candidate) > warningTextMax {
			break
		}
		kept = append(kept, warning)
	}
	kept = append(kept, domain.ImportWarning{Code: "warnings_truncated"})
	body, _ = json.Marshal(kept)
	return body, nil
}

// ParseWarnings reads stored warning JSON. Unreadable legacy text is treated as
// no warnings; it never fails a review read.
func ParseWarnings(raw string) []domain.ImportWarning {
	if raw == "" {
		return nil
	}
	var warnings []domain.ImportWarning
	if err := json.Unmarshal([]byte(raw), &warnings); err != nil {
		return nil
	}
	return warnings
}

func prioritizeWarnings(warnings []domain.ImportWarning) {
	// Stable partition: metadata_missing first, then the rest in stored order.
	out := make([]domain.ImportWarning, 0, len(warnings))
	for _, warning := range warnings {
		if warning.Code == ItemWarningMetadataMissing {
			out = append(out, warning)
		}
	}
	for _, warning := range warnings {
		if warning.Code != ItemWarningMetadataMissing {
			out = append(out, warning)
		}
	}
	copy(warnings, out)
}
