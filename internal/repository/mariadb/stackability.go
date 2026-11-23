package mariadb

import (
	"context"
	"fmt"

	"github.com/Panazule/guildbank/internal/domain"
)

// SetStackabilityAllowlist replaces the temporary OD-01 EQUID allow-list. The
// approved target is EQUID-level metadata; until coverage is sufficient this
// in-process map is the explicit allow-list. An EQUID absent from the map is
// reported as unknown and cannot be partially issued.
func (s *Store) SetStackabilityAllowlist(allowlist map[uint64]domain.Stackability) error {
	if s == nil {
		return fmt.Errorf("mariadb: store is nil")
	}
	copied := make(map[uint64]domain.Stackability, len(allowlist))
	for equid, value := range allowlist {
		if equid == 0 {
			return fmt.Errorf("mariadb: stackability allow-list contains EQUID 0")
		}
		if !value.Valid() {
			return fmt.Errorf("mariadb: stackability allow-list has invalid value %q for EQUID %d", value, equid)
		}
		copied[equid] = value
	}
	s.stackabilityMu.Lock()
	s.stackability = copied
	s.stackabilityMu.Unlock()
	return nil
}

// LookupStackability returns the approved classification for each requested
// EQUID. Duplicate identifiers are returned once. An absent EQUID is always
// unknown; this method never infers stackability from name, type or quantity.
func (s *Store) LookupStackability(ctx context.Context, equids []uint64) (map[uint64]domain.Stackability, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make(map[uint64]domain.Stackability, len(equids))
	if s == nil {
		return result, nil
	}
	s.stackabilityMu.RLock()
	defer s.stackabilityMu.RUnlock()
	for _, equid := range equids {
		if equid == 0 {
			continue
		}
		if _, seen := result[equid]; seen {
			continue
		}
		value, ok := s.stackability[equid]
		if !ok {
			value = domain.StackabilityUnknown
		}
		result[equid] = value
	}
	return result, nil
}
