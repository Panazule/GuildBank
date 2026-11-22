package domain

import "time"

// AuditFilter is the validated audit-search query. Exact-value filters only;
// From and To are inclusive UTC calendar dates.
type AuditFilter struct {
	EventType     string
	User          string
	Table         string
	RecordID      string
	CorrelationID string
	From          *time.Time
	To            *time.Time
	Page          int
	PageSize      int
}

// AuditPage is one page of audit entries plus the total under the filters.
type AuditPage struct {
	Entries    []AuditEntry
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	Filter     AuditFilter
}
