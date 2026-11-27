package api

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// characterPickerView is the single presentation model for the character
// control (design section 5.6). Mode is "select" iff the account has at least
// one profile character.
type characterPickerView struct {
	Mode     string   // "select" | "text"
	Options  []string // populated in "select" mode
	Selected string   // populated in "select" mode
	Value    string   // populated in "text" mode
	HelpLink string   // "/account/profile" when Mode == "text"
}

// requestFormValues is the submitted state preserved across a failed POST.
type requestFormValues struct {
	ItemID         int64
	Quantity       uint32
	CharacterName  string
	Note           string
	ExpectedModAt  string
	IdempotencyKey string
}

type requestItemView struct {
	ID       int64
	Name     string
	Quantity uint32
	Location string
}

type requestFormPageData struct {
	pageData
	Item           requestItemView
	Picker         characterPickerView
	Quantity       uint32
	Note           string
	ExpectedModAt  string
	IdempotencyKey string
}

// requestRowView is one request as the member's list, receipt and detail pages
// render it. StatusLabel carries the derived short-issue and unavailable text so
// no template decides it inline.
type requestRowView struct {
	ID            int64
	ItemName      string
	Quantity      uint32
	Character     string
	StatusLabel   string
	StatusClass   string
	CreatedAtISO  string
	CreatedAtText string
	MovementID    int64
	Available     bool
}

type requestsMinePageData struct {
	pageData
	Requests      []requestRowView
	StatusOptions []inventorySelectOption
	FilterStatus  string
	Filtered      bool
	Total         int
	Page          int
	PageSize      int
	TotalPages    int
	Empty         bool
	BeyondLast    bool
	PrevURL       string
	NextURL       string
	FirstURL      string
	LastURL       string
	LastPageURL   string
	PageLinks     []inventoryPageLink
}

type requestDetailPageData struct {
	pageData
	Request           requestRowView
	Note              string
	DecisionNote      string
	DecidedByName     string
	DecidedAtText     string
	RequestedQuantity uint32
	IssuedQuantity    uint32
	ShortIssued       bool
	MovementID        int64
	CanCancel         bool
}

func requestFormPageOf(lot domain.AdminInventoryItem, characters []domain.Character,
	values requestFormValues) requestFormPageData {
	return requestFormPageData{
		Item: requestItemView{
			ID: lot.ID, Name: lot.Name, Quantity: lot.Quantity,
			Location: lot.SourceLocation,
		},
		Picker:         characterPickerOf(characters, values.CharacterName),
		Quantity:       values.Quantity,
		Note:           values.Note,
		ExpectedModAt:  values.ExpectedModAt,
		IdempotencyKey: values.IdempotencyKey,
	}
}

// characterPickerOf chooses the dropdown variant when the account has profile
// characters. The first name is preselected because the profile has no main
// character flag; a posted name is matched case-insensitively and the stored
// spelling wins.
func characterPickerOf(characters []domain.Character, posted string) characterPickerView {
	if len(characters) == 0 {
		return characterPickerView{Mode: "text", Value: posted, HelpLink: "/account/profile"}
	}
	options := make([]string, 0, len(characters))
	for _, character := range characters {
		options = append(options, character.Name)
	}
	selected := characters[0].Name
	for _, character := range characters {
		if posted != "" && strings.EqualFold(character.Name, posted) {
			selected = character.Name
			break
		}
	}
	return characterPickerView{Mode: "select", Options: options, Selected: selected}
}

func requestRowOf(request domain.InventoryRequest) requestRowView {
	label, class := requestStatusLabel(request)
	movementID := int64(0)
	if request.CompletedMovementID != nil {
		movementID = *request.CompletedMovementID
	}
	return requestRowView{
		ID: request.RequestID, ItemName: request.ItemName,
		Quantity: request.RequestedQuantity, Character: request.CharacterName,
		StatusLabel: label, StatusClass: class,
		CreatedAtISO:  request.CreatedAt.UTC().Format(time.RFC3339Nano),
		CreatedAtText: request.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"),
		MovementID:    movementID, Available: request.Available(),
	}
}

// requestStatusLabel derives the badge text of design section 4.3. The short
// issue variant is the only way a member learns from the list that the guild
// gave them four of the five they asked for.
func requestStatusLabel(request domain.InventoryRequest) (string, string) {
	switch request.Status {
	case domain.RequestPending:
		if !request.Available() {
			return "Pending · item unavailable", "Pending"
		}
		return "Pending", "Pending"
	case domain.RequestCompleted:
		if request.ShortIssued() {
			return fmt.Sprintf("Completed · %d of %d", *request.IssuedQuantity, request.RequestedQuantity), "Completed"
		}
		return "Completed", "Completed"
	case domain.RequestDenied:
		return "Denied", "Denied"
	case domain.RequestCancelled:
		return "Cancelled", "Cancelled"
	default:
		return string(request.Status), "Pending"
	}
}

func requestsMinePageOf(page domain.RequestPage) requestsMinePageData {
	rows := make([]requestRowView, 0, len(page.Requests))
	for _, request := range page.Requests {
		rows = append(rows, requestRowOf(request))
	}
	filter := page.Filter
	data := requestsMinePageData{
		Requests: rows, FilterStatus: string(filter.Status),
		Filtered: filter.Status != "", Total: page.Total,
		Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages,
		Empty: page.Total == 0, BeyondLast: page.Total > 0 && page.Page > page.TotalPages,
	}
	data.StatusOptions = []inventorySelectOption{
		{Value: "", Label: "All statuses", Selected: filter.Status == ""},
		{Value: string(domain.RequestPending), Label: "Pending", Selected: filter.Status == domain.RequestPending},
		{Value: string(domain.RequestCompleted), Label: "Completed", Selected: filter.Status == domain.RequestCompleted},
		{Value: string(domain.RequestDenied), Label: "Denied", Selected: filter.Status == domain.RequestDenied},
		{Value: string(domain.RequestCancelled), Label: "Cancelled", Selected: filter.Status == domain.RequestCancelled},
	}
	if page.TotalPages > 0 {
		if page.Page > 1 {
			previous := page.Page - 1
			if previous > page.TotalPages {
				previous = page.TotalPages
			}
			data.PrevURL = requestMineURL(filter.Status, previous)
			data.FirstURL = requestMineURL(filter.Status, 1)
		}
		if page.Page < page.TotalPages {
			data.NextURL = requestMineURL(filter.Status, page.Page+1)
			data.LastURL = requestMineURL(filter.Status, page.TotalPages)
		}
		data.LastPageURL = requestMineURL(filter.Status, page.TotalPages)
		data.PageLinks = requestMinePageLinks(filter.Status, page.Page, page.TotalPages)
	}
	return data
}

func requestMineURL(status domain.RequestStatus, page int) string {
	values := url.Values{}
	if status != "" {
		values.Set("status", string(status))
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/my/requests?" + encoded
	}
	return "/my/requests"
}

func requestMinePageLinks(status domain.RequestStatus, page, totalPages int) []inventoryPageLink {
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
			Page: candidate, URL: requestMineURL(status, candidate), Current: candidate == page,
		})
	}
	return links
}

func requestDetailPageOf(request domain.InventoryRequest) requestDetailPageData {
	data := requestDetailPageData{
		Request:           requestRowOf(request),
		Note:              request.Note,
		DecisionNote:      request.DecisionNote,
		DecidedByName:     request.DecidedByName,
		RequestedQuantity: request.RequestedQuantity,
		CanCancel:         request.Status == domain.RequestPending,
	}
	if request.DecidedAt != nil {
		data.DecidedAtText = request.DecidedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	if request.IssuedQuantity != nil {
		data.IssuedQuantity = *request.IssuedQuantity
		data.ShortIssued = request.ShortIssued()
	}
	if request.CompletedMovementID != nil {
		data.MovementID = *request.CompletedMovementID
	}
	return data
}
