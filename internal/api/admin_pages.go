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

// adminOtherGroupLabel is the IMP-02 presentation of the persisted
// "Unspecified" group. The stored value and the API contract keep
// "Unspecified"; only the rendered label changes.
const adminOtherGroupLabel = "Other (not a bank location)"

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
	return adminItemView{
		ID: item.ID, EQUID: item.EQUID, Name: item.Name, ItemType: string(item.ItemType),
		Quantity: item.Quantity, ClassText: adminClassText(item.ClassString),
		RaceText: adminClassText(item.RaceString), MetadataMissing: item.MetadataMissingFg,
		SourceLocation: item.SourceLocation, LocationGroup: item.LocationGroup,
		LocationGroupLabel: adminGroupLabel(item.LocationGroup),
		GivenTo:            item.GivenTo, Deleted: item.DeletedFg,
		DateAddedISO:   item.DateAdded.Format("2006-01-02"),
		DateAddedText:  item.DateAdded.Format("Jan 2, 2006"),
		DateOpenISO:    item.DateOpen.Format("2006-01-02"),
		DateOpenText:   item.DateOpen.Format("Jan 2, 2006"),
		DateBazaarISO:  item.DateBazaar.Format("2006-01-02"),
		DateBazaarText: item.DateBazaar.Format("Jan 2, 2006"),
		Availability:   state, AvailabilityLabel: availabilityLabel(state),
		ModAtValue: item.ModAt.UTC().Format(time.RFC3339Nano),
		ModAtText:  item.ModAt.UTC().Format("2006-01-02 15:04 UTC"),
		ModBy:      item.ModBy,
	}
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
		{Value: string(domain.AdminStatusRemoved), Label: "Removed", Selected: selected == string(domain.AdminStatusRemoved)},
		{Value: string(domain.AdminStatusAll), Label: "All", Selected: selected == string(domain.AdminStatusAll)},
	}
}

func adminSortOptions(selected domain.AdminInventorySort) []inventorySortOption {
	labels := map[domain.AdminInventorySort]string{
		domain.AdminSortModifiedNewest: "Modified (newest)", domain.AdminSortModifiedOldest: "Modified (oldest)",
		domain.AdminSortName: "Name", domain.AdminSortNameDesc: "Name (reverse)",
		domain.AdminSortLocation: "Location", domain.AdminSortQuantity: "Quantity",
		domain.AdminSortDateAdded: "Date added (oldest)", domain.AdminSortDateAddedDesc: "Date added (newest)",
		domain.AdminSortDateOpen: "Open date (earliest)", domain.AdminSortDateOpenDesc: "Open date (latest)",
		domain.AdminSortDateBazaar: "Bazaar date (earliest)", domain.AdminSortDateBazaarDesc: "Bazaar date (latest)",
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
