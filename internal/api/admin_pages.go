package api

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// availabilityLabel maps the stable state enum to the owner-approved display
// label. The JSON/API values never change; only the rendered text does.
func availabilityLabel(state domain.AvailabilityState) string {
	switch state {
	case domain.AvailabilityOpen:
		return "Persona"
	case domain.AvailabilityBazaar:
		return "Bazaar"
	default:
		return "Mains"
	}
}

// adminOtherGroupLabel is the presentation of the persisted "Unspecified"
// group. The stored value and the API contract keep "Unspecified"; only the
// rendered label changes.
const adminOtherGroupLabel = "Other"

func adminGroupLabel(group string) string {
	if group == "Unspecified" {
		return adminOtherGroupLabel
	}
	return group
}

func adminClassText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	if strings.EqualFold(value, "ALL") {
		return "All"
	}
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ", ")
}

func adminItemViewOf(item domain.AdminInventoryItem) adminItemView {
	now := time.Now().UTC()
	state := service.Availability(now, item.DateOpen, item.DateBazaar)
	// A movement-derived issued row is a movement, not a lot: it is always
	// Issued and is returned through its movement route (D5/D6). A removed lot
	// that carries a recipient is the legacy whole-stack issue; it keeps the
	// lot-level Return route.
	movementBacked := item.MovementID > 0
	statusLabel, statusClass := "Active", "status-Completed"
	switch {
	case movementBacked:
		statusLabel, statusClass = "Issued", "status-Issued"
	case item.DeletedFg && strings.TrimSpace(item.GivenTo) != "":
		statusLabel, statusClass = "Issued", "status-Issued"
	case item.DeletedFg:
		statusLabel, statusClass = "Removed", "status-Failed"
	}
	recipient := strings.TrimSpace(item.GivenTo)
	issuedURL := adminIssuedURL(item)
	returnURL := ""
	rowKey := "l:" + strconv.FormatInt(item.ID, 10)
	issuedQuantity := item.Quantity
	if movementBacked {
		issuedURL = ""
		if recipient != "" {
			issuedURL = adminIssuedRecipientURL(recipient)
		}
		rowKey = "m:" + strconv.FormatInt(item.MovementID, 10)
		returnURL = "/admin/inventory/movements/" + strconv.FormatInt(item.MovementID, 10) + "/return"
		issuedQuantity = item.IssuedQuantity
	}
	view := adminItemView{
		ID: item.ID, EQUID: item.EQUID, Name: item.Name, ItemType: string(item.ItemType),
		Quantity: item.Quantity, ClassText: adminClassText(item.ClassString),
		RaceText: adminClassText(item.RaceString), MetadataMissing: item.MetadataMissingFg,
		UnresolvedIdentity: item.HasUnresolvedIdentity(),
		SourceLocation:     item.SourceLocation, LocationGroup: item.LocationGroup,
		LocationGroupLabel: adminGroupLabel(item.LocationGroup),
		GivenTo:            item.GivenTo, Deleted: item.DeletedFg,
		// Restore is offered only for a plain administrative removal. An issued
		// row is handled by Return, and a row absorbed by consolidation names a
		// survivor and cannot be restored.
		Restorable:  item.DeletedFg && strings.TrimSpace(item.GivenTo) == "" && item.ConsolidatedInto == nil,
		StatusLabel: statusLabel, StatusClass: statusClass,
		IssuedURL:      issuedURL,
		DateAddedISO:   item.DateAdded.Format("2006-01-02"),
		DateAddedText:  item.DateAdded.Format("Jan 2, 2006"),
		DateOpenISO:    item.DateOpen.Format("2006-01-02"),
		DateOpenText:   item.DateOpen.Format("Jan 2, 2006"),
		DateBazaarISO:  item.DateBazaar.Format("2006-01-02"),
		DateBazaarText: item.DateBazaar.Format("Jan 2, 2006"),
		Availability:   state, AvailabilityLabel: availabilityLabel(state),
		ModAtValue:     item.ModAt.UTC().Format(time.RFC3339Nano),
		ModAtText:      item.ModAt.UTC().Format("2006-01-02 15:04 UTC"),
		ModBy:          item.ModBy,
		MovementBacked: movementBacked, MovementID: item.MovementID, RowKey: rowKey,
		IssuedQuantity: issuedQuantity, IssuedBy: item.IssuedBy, ReturnURL: returnURL,
	}
	if movementBacked && !item.IssuedAt.IsZero() {
		view.IssuedAtISO = item.IssuedAt.UTC().Format(time.RFC3339Nano)
		view.IssuedAtText = item.IssuedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	return view
}

// adminIssuedURL returns the recipient's issued queue for an issued row and ""
// for every other state. The empty value keeps the recipient and status cells
// plain text on active and administratively removed rows.
func adminIssuedURL(item domain.AdminInventoryItem) string {
	recipient := strings.TrimSpace(item.GivenTo)
	if !item.DeletedFg || recipient == "" {
		return ""
	}
	return adminIssuedRecipientURL(recipient)
}

func auditEntryViewOf(entry domain.AuditEntry) auditEntryView {
	oldValues := "{}"
	if len(entry.OldValues) > 0 {
		oldValues = string(entry.OldValues)
	}
	newValues := "{}"
	if len(entry.NewValues) > 0 {
		newValues = string(entry.NewValues)
	}
	return auditEntryView{
		AuditID: entry.AuditID, EventDate: entry.EventDate.UTC().Format(time.RFC3339Nano),
		EventDateText: entry.EventDate.UTC().Format("2006-01-02 15:04:05 UTC"),
		UserName:      entry.UserName, EventType: entry.EventType, TableName: entry.TableName,
		RecordID: entry.RecordID, CorrelationID: entry.CorrelationID, SourceIP: entry.SourceIP,
		Notes: entry.Notes, OldValues: oldValues, NewValues: newValues,
	}
}

func adminFilterViewOf(filter domain.AdminInventoryFilter) adminFilterView {
	view := adminFilterView{
		Search: filter.Search, GivenTo: filter.GivenTo, Metadata: string(filter.Metadata),
		LocationGroup: filter.LocationGroup, ItemType: string(filter.ItemType),
		Status: string(filter.Status), Sort: string(filter.Sort),
		PageSize: strconv.Itoa(filter.PageSize),
	}
	if filter.ID > 0 {
		view.ID = strconv.FormatInt(filter.ID, 10)
	}
	if filter.EQUID > 0 {
		view.EQUID = strconv.FormatUint(filter.EQUID, 10)
	}
	return view
}

func adminItemTypeOptions(selected string) []inventorySelectOption {
	return []inventorySelectOption{
		{Value: "", Label: "All types", Selected: selected == ""},
		{Value: string(domain.ItemTypeItem), Label: "Items", Selected: selected == string(domain.ItemTypeItem)},
		{Value: string(domain.ItemTypeSpell), Label: "Spells", Selected: selected == string(domain.ItemTypeSpell)},
	}
}

func adminMetadataOptions(selected string) []inventorySelectOption {
	return []inventorySelectOption{
		{Value: string(domain.AdminMetadataAny), Label: "Any metadata", Selected: selected == string(domain.AdminMetadataAny)},
		{Value: string(domain.AdminMetadataMissing), Label: "Metadata missing", Selected: selected == string(domain.AdminMetadataMissing)},
		{Value: string(domain.AdminMetadataPresent), Label: "Metadata present", Selected: selected == string(domain.AdminMetadataPresent)},
	}
}

func adminStatusOptions(selected string) []inventorySelectOption {
	return []inventorySelectOption{
		{Value: string(domain.AdminStatusActive), Label: "Active", Selected: selected == string(domain.AdminStatusActive)},
		{Value: string(domain.AdminStatusIssued), Label: "Issued", Selected: selected == string(domain.AdminStatusIssued)},
		{Value: string(domain.AdminStatusRemoved), Label: "Removed", Selected: selected == string(domain.AdminStatusRemoved)},
		{Value: string(domain.AdminStatusAll), Label: "All", Selected: selected == string(domain.AdminStatusAll)},
	}
}

// adminIssuedRecipientURL is the recipient's issued queue: the same admin list
// filtered to issued lots and to one recipient. It is the only link target the
// recipient and issued-badge links use.
func adminIssuedRecipientURL(recipient string) string {
	values := url.Values{}
	values.Set("status", string(domain.AdminStatusIssued))
	values.Set("givenTo", recipient)
	return adminInventoryListPath + "?" + values.Encode()
}

// adminStatusTabs builds the lifecycle-state shortcuts. Each tab keeps every
// active filter except the status and resets the page to 1.
func adminStatusTabs(filter domain.AdminInventoryFilter) []adminStatusTab {
	tabs := make([]adminStatusTab, 0, len(domain.AdminInventoryStatuses))
	for _, status := range domain.AdminInventoryStatuses {
		candidate := filter
		candidate.Status = status
		tabs = append(tabs, adminStatusTab{
			Value:   string(status),
			Label:   strings.ToUpper(string(status[:1])) + string(status[1:]),
			URL:     adminListURL(candidate, 1),
			Current: filter.Status == status,
		})
	}
	return tabs
}

func adminSortOptions(selected domain.AdminInventorySort) []inventorySortOption {
	labels := map[domain.AdminInventorySort]string{
		domain.AdminSortModifiedNewest: "Modified (newest)", domain.AdminSortModifiedOldest: "Modified (oldest)",
		domain.AdminSortName: "Name (ascending)", domain.AdminSortNameDesc: "Name (descending)",
		domain.AdminSortLocation: "Location", domain.AdminSortQuantity: "Quantity",
		domain.AdminSortDateAdded: "Date added (oldest)", domain.AdminSortDateAddedDesc: "Date added (newest)",
		domain.AdminSortDateOpen: "Open date (earliest)", domain.AdminSortDateOpenDesc: "Open date (latest)",
		domain.AdminSortDateBazaar: "Bazaar date (earliest)", domain.AdminSortDateBazaarDesc: "Bazaar date (latest)",
		domain.AdminSortRecipient: "Recipient (ascending)", domain.AdminSortRecipientDesc: "Recipient (descending)",
	}
	out := make([]inventorySortOption, 0, len(domain.AdminInventorySorts))
	for _, sort := range domain.AdminInventorySorts {
		out = append(out, inventorySortOption{Value: string(sort), Label: labels[sort], Selected: sort == selected})
	}
	return out
}

func adminPageSizeOptions(selected int) []inventorySelectOption {
	values := []int{25, 50, 100, 200}
	out := make([]inventorySelectOption, 0, len(values))
	for _, value := range values {
		out = append(out, inventorySelectOption{
			Value: strconv.Itoa(value), Label: strconv.Itoa(value), Selected: value == selected,
		})
	}
	return out
}

func adminListURL(filter domain.AdminInventoryFilter, page int) string {
	values := url.Values{}
	if filter.Search != "" {
		values.Set("q", filter.Search)
	}
	if filter.ID > 0 {
		values.Set("id", strconv.FormatInt(filter.ID, 10))
	}
	if filter.EQUID > 0 {
		values.Set("equid", strconv.FormatUint(filter.EQUID, 10))
	}
	if filter.GivenTo != "" {
		values.Set("givenTo", filter.GivenTo)
	}
	if filter.Metadata != "" && filter.Metadata != domain.AdminMetadataAny {
		values.Set("metadata", string(filter.Metadata))
	}
	if filter.LocationGroup != "" {
		values.Set("locationGroup", filter.LocationGroup)
	}
	if filter.ItemType != "" {
		values.Set("itemType", string(filter.ItemType))
	}
	if filter.Status != "" && filter.Status != domain.AdminStatusActive {
		values.Set("status", string(filter.Status))
	}
	if filter.Sort != "" && filter.Sort != domain.AdminSortModifiedNewest {
		values.Set("sort", string(filter.Sort))
	}
	if filter.PageSize != 0 && filter.PageSize != service.AdminDefaultPageSize {
		values.Set("pageSize", strconv.Itoa(filter.PageSize))
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/admin/inventory?" + encoded
	}
	return "/admin/inventory"
}

// adminListPageOf builds the admin list view model, including pager links that
// preserve every active filter.
func adminListPageOf(page domain.AdminInventoryPage, groups []string) adminListPageData {
	items := make([]adminItemView, 0, len(page.Records))
	for _, record := range page.Records {
		items = append(items, adminItemViewOf(record))
	}
	filter := page.Filter
	data := adminListPageData{
		pageData: pageData{Title: "Admin inventory"},
		Ready:    true, Items: items, Filter: adminFilterViewOf(filter),
		ItemTypeOpts: adminItemTypeOptions(string(filter.ItemType)),
		MetadataOpts: adminMetadataOptions(string(filter.Metadata)),
		StatusOpts:   adminStatusOptions(string(filter.Status)),
		SortOpts:     adminSortOptions(filter.Sort),
		PageSizeOpts: adminPageSizeOptions(filter.PageSize),
		Total:        page.Total, Page: page.Page, PageSize: page.PageSize,
		TotalPages: page.TotalPages,
		Empty:      page.Total == 0,
		BeyondLast: page.Total > 0 && page.Page > page.TotalPages,
		ClearURL:   "/admin/inventory",
		SelfURL:    adminListURL(filter, page.Page),
		StatusTabs: adminStatusTabs(filter),
	}
	// Active + recipient is the one combination that can never match: active
	// rows carry no recipient. Explain the empty result and offer the issued
	// queue for the same recipient instead of leaving a dead end.
	if filter.Status == domain.AdminStatusActive && filter.GivenTo != "" {
		issued := filter
		issued.Status = domain.AdminStatusIssued
		data.IssuedHint = true
		data.IssuedHintURL = adminListURL(issued, 1)
	}
	selected := filter.LocationGroup
	data.LocationGroups = []adminLocationOption{{Value: "", Label: "Any location", Selected: selected == ""}}
	for _, group := range groups {
		data.LocationGroups = append(data.LocationGroups, adminLocationOption{
			Value: group, Label: adminGroupLabel(group), Selected: group == selected,
		})
	}
	if page.TotalPages > 0 {
		if page.Page > 1 {
			previous := page.Page - 1
			if previous > page.TotalPages {
				previous = page.TotalPages
			}
			data.PrevURL = adminListURL(filter, previous)
			data.FirstURL = adminListURL(filter, 1)
		}
		if page.Page < page.TotalPages {
			data.NextURL = adminListURL(filter, page.Page+1)
			data.LastURL = adminListURL(filter, page.TotalPages)
		}
		data.LastPageURL = adminListURL(filter, page.TotalPages)
		data.PageLinks = adminPageLinks(filter, page.Page, page.TotalPages)
	}
	return data
}

func adminPageLinks(filter domain.AdminInventoryFilter, page, totalPages int) []inventoryPageLink {
	links := make([]inventoryPageLink, 0, 5)
	if totalPages < 1 {
		return links
	}
	center := page
	if center > totalPages {
		center = totalPages
	}
	if center < 1 {
		center = 1
	}
	start := center - 2
	if start < 1 {
		start = 1
	}
	end := start + 4
	if end > totalPages {
		end = totalPages
		start = end - 4
		if start < 1 {
			start = 1
		}
	}
	for candidate := start; candidate <= end; candidate++ {
		links = append(links, inventoryPageLink{
			Page: candidate, URL: adminListURL(filter, candidate), Current: candidate == page,
		})
	}
	return links
}
