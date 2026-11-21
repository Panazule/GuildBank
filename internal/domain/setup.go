// Package domain holds specification types that are shared by the service,
// repository and API layers. It contains no I/O and imports no other
// application package.
//
// Phase 0 defines only setup state; inventory, import and audit types arrive
// with the phases that own them.
package domain

// SetupState is the D-02 first-run state derived from the SQLite Users table.
// It is not derived from the existence of the database file.
type SetupState int

const (
	// SetupUnknown means the state could not be determined.
	SetupUnknown SetupState = iota
	// SetupPending means no administrator account exists.
	SetupPending
	// SetupComplete means at least one administrator account exists.
	SetupComplete
)

func (s SetupState) String() string {
	switch s {
	case SetupPending:
		return "pending"
	case SetupComplete:
		return "complete"
	default:
		return "unknown"
	}
}
