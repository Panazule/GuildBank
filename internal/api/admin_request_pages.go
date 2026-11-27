package api

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// adminRequestFilterView is the queue's filter state. The officer queue is
// oldest-first by design and the order is not selectable.
type adminRequestFilterView struct {
	Status     string
	Kind       string
	Character  string
	Item       string
	From       string
	To         string
	Actionable bool
	PageSize   string
}

// adminRequestRowView is one queue row. AgeClass drives the 2-day/7-day colour
// bands and StatusLabel carries the derived short-issue text.
type adminRequestRowView struct {
	ID            int64
	ItemName      string
	ItemID        int64
	Quantity      uint32
	Character     string
	RequesterKind string
	RequesterName string
	IsSelf        bool
	IsPublic      bool
	StatusLabel   string
	StatusClass   string
	CreatedAtISO  string
	CreatedAtText string
	AgeText       string
	AgeClass      string
	OnHand        uint32
	LotActive     bool
	ShortStock    bool
	ShortIssued   bool
	Issued        uint32
	MovementID    int64
	DetailURL     string
}

type adminRequestsPageData struct {
	pageData
	Requests     []adminRequestRowView
	Filter       adminRequestFilterView
	StatusOpts   []inventorySelectOption
	KindOpts     []inventorySelectOption
	PageSizeOpts []inventorySelectOption
	Total        int
	Page         int
	PageSize     int
	TotalPages   int
	Empty        bool
	Filtered     bool
	BeyondLast   bool
	ClearURL     string
	PrevURL      string
	NextURL      string
	FirstURL     string
	LastURL      string
	LastPageURL  string
	PageLinks    []inventoryPageLink
}

type adminRequestDetailPageData struct {
	pageData
	Request adminRequestRowView

	RequesterKind  string
	RequesterName  string
	ContactDiscord string
	CharacterName  string
	ItemName       string
	ItemID         int64
	RequestedQty   uint32
	RequestNote    string
	SelfRequested  bool
	CanDecide      bool

	LotFound        bool
	LotActive       bool
	LotDeleted      bool
	LotGivenTo      string
	LotLocation     string
	LotModAtValue   string
	LotModAtText    string
	LotModBy        string
	OnHand          uint32
	ShortStock      bool
	ApprovePossible bool
	IssueQuantity   uint32
	ExpectedModAt   string

	DecisionNote   string
	DecidedByName  string
	DecidedAtText  string
	IssuedQuantity uint32
	ShortIssued    bool
	MovementID     int64

	ApproveAction string
	DenyAction    string
	LotDetailURL  string
	LotHistoryURL string
	History       []auditEntryView
}

// adminRequestRowOf renders one request. The officer's own row gets a neutral
// "You" chip, never a restriction (design section 8.6).
func adminRequestRowOf(request domain.InventoryRequest, selfUserID int64, now time.Time) adminRequestRowView {
	label, class := requestStatusLabel(request)
	ageText, ageClass := adminRequestAge(request.CreatedAt, now)
	row := adminRequestRowView{
		ID: request.RequestID, ItemName: request.ItemName, ItemID: request.GuildBankItemID,
		Quantity: request.RequestedQuantity, Character: request.CharacterName,
		RequesterKind: string(request.RequesterKind), RequesterName: request.RequesterUserName,
		IsPublic:    request.RequesterKind == domain.RequesterPublic,
		StatusLabel: label, StatusClass: class,
		CreatedAtISO:  request.CreatedAt.UTC().Format(time.RFC3339Nano),
		CreatedAtText: request.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"),
		AgeText:       ageText, AgeClass: ageClass,
		OnHand: request.CurrentOnHand, LotActive: request.LotActive,
		ShortStock: request.Status == domain.RequestPending && request.LotActive &&
			request.CurrentOnHand < request.RequestedQuantity,
		DetailURL: "/admin/requests/" + strconv.FormatInt(request.RequestID, 10),
	}
	if request.RequesterUserID != nil && selfUserID > 0 && *request.RequesterUserID == selfUserID {
		row.IsSelf = true
	}
	if request.IssuedQuantity != nil {
		row.Issued = *request.IssuedQuantity
		row.ShortIssued = request.ShortIssued()
	}
	if request.CompletedMovementID != nil {
		row.MovementID = *request.CompletedMovementID
	}
	return row
}

// adminRequestAge formats the queue age and its colour band: amber at two days
// and red at seven.
func adminRequestAge(created, now time.Time) (string, string) {
	if created.IsZero() {
		return "—", "age-fresh"
	}
	age := now.Sub(created.UTC())
	if age < 0 {
		age = 0
	}
	class := "age-fresh"
	switch {
	case age >= 7*24*time.Hour:
		class = "age-critical"
	case age >= 2*24*time.Hour:
		class = "age-warning"
	}
	switch {
	case age < time.Hour:
		return "just now", class
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours())), class
	default:
		return fmt.Sprintf("%dd", int(age.Hours()/24)), class
	}
}

func adminRequestFilterViewOf(filter domain.RequestFilter) adminRequestFilterView {
	view := adminRequestFilterView{
		Status: string(filter.Status), Kind: string(filter.RequesterKind),
		Character: filter.CharacterName, Item: filter.ItemName,
		Actionable: filter.Actionable, PageSize: strconv.Itoa(filter.PageSize),
	}
	if !filter.From.IsZero() {
		view.From = filter.From.UTC().Format("2006-01-02")
	}
	if !filter.To.IsZero() {
		view.To = filter.To.UTC().Format("2006-01-02")
	}
	return view
}

func adminRequestStatusOptions(selected string) []inventorySelectOption {
	return []inventorySelectOption{
		{Value: "all", Label: "All statuses", Selected: selected == ""},
		{Value: string(domain.RequestPending), Label: "Pending", Selected: selected == string(domain.RequestPending)},
		{Value: string(domain.RequestCompleted), Label: "Completed", Selected: selected == string(domain.RequestCompleted)},
		{Value: string(domain.RequestDenied), Label: "Denied", Selected: selected == string(domain.RequestDenied)},
		{Value: string(domain.RequestCancelled), Label: "Cancelled", Selected: selected == string(domain.RequestCancelled)},
	}
}

func adminRequestKindOptions(selected string) []inventorySelectOption {
	return []inventorySelectOption{
		{Value: "", Label: "All requesters", Selected: selected == ""},
		{Value: string(domain.RequesterMember), Label: "Members", Selected: selected == string(domain.RequesterMember)},
		{Value: string(domain.RequesterPublic), Label: "Public", Selected: selected == string(domain.RequesterPublic)},
	}
}

// adminRequestQueueURL rebuilds the queue URL with the active filters. Paging
// preserves every predicate so a filtered queue never loses its scope.
func adminRequestQueueURL(filter domain.RequestFilter, page int) string {
	values := url.Values{}
	if filter.Status != "" {
		values.Set("status", string(filter.Status))
	} else {
		values.Set("status", "all")
	}
	if filter.RequesterKind != "" {
		values.Set("kind", string(filter.RequesterKind))
	}
	if filter.CharacterName != "" {
		values.Set("character", filter.CharacterName)
	}
	if filter.ItemName != "" {
		values.Set("item", filter.ItemName)
	}
	if !filter.From.IsZero() {
		values.Set("from", filter.From.UTC().Format("2006-01-02"))
	}
	if !filter.To.IsZero() {
		values.Set("to", filter.To.UTC().Format("2006-01-02"))
	}
	if filter.Actionable {
		values.Set("actionable", "1")
	}
	if filter.PageSize != 0 && filter.PageSize != domain.RequestDefaultPageSize {
		values.Set("pageSize", strconv.Itoa(filter.PageSize))
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	return "/admin/requests?" + values.Encode()
}

func adminRequestPageLinks(filter domain.RequestFilter, page, totalPages int) []inventoryPageLink {
	if totalPages < 1 {
		return nil
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
	links := make([]inventoryPageLink, 0, end-start+1)
	for candidate := start; candidate <= end; candidate++ {
		links = append(links, inventoryPageLink{
			Page: candidate, URL: adminRequestQueueURL(filter, candidate), Current: candidate == page,
		})
	}
	return links
}

func adminRequestsPageOf(page domain.RequestPage, selfUserID int64, now time.Time) adminRequestsPageData {
	rows := make([]adminRequestRowView, 0, len(page.Requests))
	for _, request := range page.Requests {
		rows = append(rows, adminRequestRowOf(request, selfUserID, now))
	}
	filter := page.Filter
	data := adminRequestsPageData{
		Requests: rows, Filter: adminRequestFilterViewOf(filter),
		StatusOpts:   adminRequestStatusOptions(string(filter.Status)),
		KindOpts:     adminRequestKindOptions(string(filter.RequesterKind)),
		PageSizeOpts: adminPageSizeOptions(filter.PageSize),
		Total:        page.Total, Page: page.Page, PageSize: page.PageSize,
		TotalPages: page.TotalPages,
		Empty:      page.Total == 0,
		Filtered: filter.Status != domain.RequestPending || filter.RequesterKind != "" ||
			filter.CharacterName != "" || filter.ItemName != "" || !filter.From.IsZero() ||
			!filter.To.IsZero() || filter.Actionable,
		BeyondLast: page.Total > 0 && page.Page > page.TotalPages,
		ClearURL:   "/admin/requests",
	}
	if page.TotalPages > 0 {
		if page.Page > 1 {
			previous := page.Page - 1
			if previous > page.TotalPages {
				previous = page.TotalPages
			}
			data.PrevURL = adminRequestQueueURL(filter, previous)
			data.FirstURL = adminRequestQueueURL(filter, 1)
		}
		if page.Page < page.TotalPages {
			data.NextURL = adminRequestQueueURL(filter, page.Page+1)
			data.LastURL = adminRequestQueueURL(filter, page.TotalPages)
		}
		data.LastPageURL = adminRequestQueueURL(filter, page.TotalPages)
		data.PageLinks = adminRequestPageLinks(filter, page.Page, page.TotalPages)
	}
	return data
}

// adminRequestDetailOf builds the decision page. The request's own projection
// (CurrentOnHand/LotActive) is authoritative for availability; the lot record
// adds location and version only when it still exists.
func adminRequestDetailOf(request domain.InventoryRequest, lot domain.AdminInventoryItem,
	lotFound bool, history []auditEntryView, selfUserID int64, now time.Time) adminRequestDetailPageData {
	row := adminRequestRowOf(request, selfUserID, now)
	id := strconv.FormatInt(request.RequestID, 10)
	data := adminRequestDetailPageData{
		Request:       row,
		RequesterKind: string(request.RequesterKind), RequesterName: request.RequesterUserName,
		ContactDiscord: request.ContactDiscord,
		CharacterName:  request.CharacterName, ItemName: request.ItemName,
		ItemID:       request.GuildBankItemID,
		RequestedQty: request.RequestedQuantity, RequestNote: request.Note,
		SelfRequested: row.IsSelf,
		CanDecide:     request.Status == domain.RequestPending,
		LotFound:      lotFound,
		LotActive:     request.LotActive,
		OnHand:        request.CurrentOnHand,
		ApproveAction: "/admin/requests/" + id + "/approve",
		DenyAction:    "/admin/requests/" + id + "/deny",
		LotDetailURL:  "/admin/inventory/" + strconv.FormatInt(request.GuildBankItemID, 10),
		LotHistoryURL: "/admin/inventory/" + strconv.FormatInt(request.GuildBankItemID, 10) + "/movements",
		History:       history,
	}
	if lotFound {
		data.LotDeleted = lot.DeletedFg
		data.LotGivenTo = lot.GivenTo
		data.LotLocation = lot.SourceLocation
		data.LotModBy = lot.ModBy
		data.ExpectedModAt = lot.ModAt.UTC().Format(time.RFC3339Nano)
		data.LotModAtValue = data.ExpectedModAt
		data.LotModAtText = lot.ModAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	data.ApprovePossible = data.CanDecide && data.LotActive && data.OnHand > 0
	data.ShortStock = data.ApprovePossible && data.OnHand < data.RequestedQty
	if data.ShortStock {
		data.IssueQuantity = data.OnHand
	}
	if request.IssuedQuantity != nil {
		data.IssuedQuantity = *request.IssuedQuantity
		data.ShortIssued = request.ShortIssued()
	}
	if request.CompletedMovementID != nil {
		data.MovementID = *request.CompletedMovementID
	}
	data.DecisionNote = request.DecisionNote
	data.DecidedByName = request.DecidedByName
	if request.DecidedAt != nil {
		data.DecidedAtText = request.DecidedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	return data
}
