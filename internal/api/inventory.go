package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

const publicInventoryTimeout = 5 * time.Second

type inventoryHandler struct {
	inventory *service.InventoryService
	auth      *service.AuthService
	views     *views
	logger    *slog.Logger
}

type publicInventoryItemResponse struct {
	ID              int64                    `json:"id"`
	EQUID           uint64                   `json:"equid"`
	Name            string                   `json:"name"`
	ItemType        domain.ItemType          `json:"itemType"`
	Quantity        uint32                   `json:"quantity"`
	ClassString     string                   `json:"classString"`
	RaceString      string                   `json:"raceString"`
	MetadataMissing bool                     `json:"metadataMissing"`
	DateAdded       string                   `json:"dateAdded"`
	DateOpen        string                   `json:"dateOpen"`
	DateBazaar      string                   `json:"dateBazaar"`
	Availability    domain.AvailabilityState `json:"availability"`
	EQResourceURL   *string                  `json:"eqResourceUrl"`
}

func (h *inventoryHandler) list(c *gin.Context) {
	filter, fields := inventoryFilterFrom(c)
	if len(fields) > 0 {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"one or more fields are invalid", fields)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), publicInventoryTimeout)
	defer cancel()
	page, err := h.inventory.ListPublic(ctx, filter)
	if err != nil {
		h.respondInventoryError(c, err)
		return
	}
	items := make([]publicInventoryItemResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, publicInventoryItemResponseOf(item))
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{"items": items},
		"meta": gin.H{
			"page": page.Page, "pageSize": page.PageSize, "total": page.Total,
			"totalPages": page.TotalPages, "sort": page.Sort,
		},
	})
}

func (h *inventoryHandler) get(c *gin.Context) {
	id, ok := h.parsePublicInventoryID(c, false)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), publicInventoryTimeout)
	defer cancel()
	item, err := h.inventory.GetPublic(ctx, id)
	if err != nil {
		h.respondInventoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"item": publicInventoryItemResponseOf(item)}})
}

func (h *inventoryHandler) page(c *gin.Context) {
	filter, fields := inventoryFilterFrom(c)
	if len(fields) > 0 {
		h.views.errorPage(c, http.StatusUnprocessableEntity, inventoryValidationMessage(fields))
		return
	}
	if !h.inventory.Ready() {
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The guild inventory is temporarily unavailable. Please try again after the database connection is restored.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), publicInventoryTimeout)
	defer cancel()
	result, err := h.inventory.ListPublic(ctx, filter)
	if err != nil {
		h.respondInventoryPageError(c, err)
		return
	}
	data := inventoryListPageDataOf(result, h.publicUser(c))
	data.CSRF = CSRF(c)
	h.views.inventoryList(c, http.StatusOK, data)
}

func (h *inventoryHandler) detailPage(c *gin.Context) {
	id, ok := h.parsePublicInventoryID(c, true)
	if !ok {
		return
	}
	if !h.inventory.Ready() {
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The guild inventory is temporarily unavailable. Please try again after the database connection is restored.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), publicInventoryTimeout)
	defer cancel()
	item, err := h.inventory.GetPublic(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			h.views.errorPage(c, http.StatusNotFound, "The requested inventory record does not exist.")
			return
		}
		h.respondInventoryPageError(c, err)
		return
	}
	h.views.inventoryDetail(c, http.StatusOK, inventoryDetailPageData{
		pageData: pageData{
			Title: item.Name, CSRF: CSRF(c), User: h.publicUser(c),
		},
		Item: inventoryItemViewOf(item, true),
	})
}

func (h *inventoryHandler) publicUser(c *gin.Context) *domain.User {
	token := sessionToken(c)
	if token == "" || h.auth == nil {
		return nil
	}
	principal, err := h.auth.Authenticate(c.Request.Context(), token)
	if err != nil {
		// A public page treats an invalid or unavailable optional session as
		// anonymous rather than turning the inventory read into a login error.
		return nil
	}
	return &principal.User
}
func inventoryFilterFrom(c *gin.Context) (domain.InventoryFilter, map[string]string) {
	filter := domain.InventoryFilter{
		Search: strings.TrimSpace(c.Query("q")),
		Class:  strings.TrimSpace(c.Query("class")),
		Race:   strings.TrimSpace(c.Query("race")),
		Sort:   domain.InventorySort(c.Query("sort")),
		Page:   1, PageSize: service.PublicInventoryDefaultPageSize,
	}
	if raw := strings.TrimSpace(c.Query("itemType")); raw != "" {
		filter.ItemType = domain.ItemType(raw)
	}
	fields := make(map[string]string)
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > service.PublicInventoryMaxPage {
			fields["page"] = fmt.Sprintf("page must be between 1 and %d", service.PublicInventoryMaxPage)
		} else {
			filter.Page = value
		}
	}
	if raw := strings.TrimSpace(c.Query("pageSize")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > service.PublicInventoryMaxPageSize {
			fields["pageSize"] = fmt.Sprintf("pageSize must be between 1 and %d", service.PublicInventoryMaxPageSize)
		} else {
			filter.PageSize = value
		}
	}
	return filter, fields
}

func (h *inventoryHandler) parsePublicInventoryID(c *gin.Context, htmlResponse bool) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		if htmlResponse {
			h.views.errorPage(c, http.StatusNotFound, "The requested inventory record does not exist.")
		} else {
			writeError(c, http.StatusNotFound, "not_found", "inventory item not found")
		}
		return 0, false
	}
	return id, true
}

func (h *inventoryHandler) respondInventoryError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		respondError(c, h.logger, err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable",
			"the inventory request timed out; retry the request")
	case errors.Is(err, domain.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable",
			"the guild inventory is unavailable")
	case errors.Is(err, domain.ErrNotFound):
		writeError(c, http.StatusNotFound, "not_found", "inventory item not found")
	default:
		respondError(c, h.logger, err)
	}
}

func (h *inventoryHandler) respondInventoryPageError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		h.views.errorPage(c, http.StatusUnprocessableEntity, inventoryValidationMessage(validation.Fields))
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled),
		errors.Is(err, domain.ErrUnavailable):
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The guild inventory is temporarily unavailable. Please try again after the database connection is restored.")
	default:
		h.logger.Error("public inventory page failed", "request_id", GetRequestID(c), "error", err)
		h.views.errorPage(c, http.StatusInternalServerError, "The inventory could not be displayed.")
	}
}

func inventoryValidationMessage(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "One or more filters are invalid."
	}
	return fields[keys[0]]
}

func publicInventoryItemResponseOf(item domain.PublicInventoryItem) publicInventoryItemResponse {
	return publicInventoryItemResponse{
		ID: item.ID, EQUID: item.EQUID, Name: item.Name, ItemType: item.ItemType,
		Quantity: item.Quantity, ClassString: item.ClassString, RaceString: item.RaceString,
		MetadataMissing: item.MetadataMissing,
		DateAdded:       item.DateAdded.Format("2006-01-02"), DateOpen: item.DateOpen.Format("2006-01-02"),
		DateBazaar: item.DateBazaar.Format("2006-01-02"), Availability: item.Availability,
		EQResourceURL: item.EQResourceURL,
	}
}

type inventoryFilterView struct {
	Search   string
	Class    string
	Race     string
	ItemType string
	Sort     string
}

type inventorySelectOption struct {
	Value    string
	Label    string
	Selected bool
}

type inventorySortOption struct {
	Value    string
	Label    string
	Selected bool
}

type inventoryPageLink struct {
	Page    int
	URL     string
	Current bool
}

type inventoryItemView struct {
	ID                int64
	EQUID             uint64
	Name              string
	ItemType          domain.ItemType
	Quantity          uint32
	ClassText         string
	RaceText          string
	ClassUnknown      bool
	RaceUnknown       bool
	MetadataNote      string
	DateAddedISO      string
	DateAddedText     string
	DateOpenISO       string
	DateOpenText      string
	DateBazaarISO     string
	DateBazaarText    string
	Availability      domain.AvailabilityState
	AvailabilityLabel string
	EQResourceURL     *string
}

func inventoryItemViewOf(item domain.PublicInventoryItem, detail bool) inventoryItemView {
	classText, classUnknown := inventoryRestrictionText(item.ClassString, detail, "classes")
	raceText, raceUnknown := inventoryRestrictionText(item.RaceString, detail, "races")
	note := ""
	if item.MetadataMissing {
		note = "Metadata unavailable; restrictions may be unknown."
	}
	view := inventoryItemView{
		ID: item.ID, EQUID: item.EQUID, Name: item.Name, ItemType: item.ItemType,
		Quantity: item.Quantity, ClassText: classText, RaceText: raceText,
		ClassUnknown: classUnknown, RaceUnknown: raceUnknown, MetadataNote: note,
		DateAddedISO: item.DateAdded.Format("2006-01-02"), DateAddedText: item.DateAdded.Format("Jan 2, 2006"),
		DateOpenISO: item.DateOpen.Format("2006-01-02"), DateOpenText: item.DateOpen.Format("Jan 2, 2006"),
		DateBazaarISO: item.DateBazaar.Format("2006-01-02"), DateBazaarText: item.DateBazaar.Format("Jan 2, 2006"),
		Availability: item.Availability, EQResourceURL: item.EQResourceURL,
	}
	// PD5-37: the visible label names the audience state (Mains/Persona/
	// Bazaar); the JSON availability enum is unchanged.
	view.AvailabilityLabel = availabilityLabel(item.Availability)
	return view
}

func inventoryRestrictionText(value string, detail bool, noun string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown", true
	}
	if strings.EqualFold(value, "ALL") {
		if detail {
			return "All " + noun, false
		}
		return "All", false
	}
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ", "), false
}

func inventoryListPageDataOf(result domain.PublicInventoryPage, user *domain.User) inventoryListPageData {
	items := make([]inventoryItemView, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, inventoryItemViewOf(item, false))
	}
	filter := result.Filter
	hasFilters := filter.Search != "" || filter.Class != "" || filter.Race != "" || filter.ItemType != ""
	data := inventoryListPageData{
		pageData: pageData{Title: "Guild inventory", User: user},
		Ready:    true, Items: items,
		Filter: inventoryFilterView{
			Search: filter.Search, Class: filter.Class, Race: filter.Race,
			ItemType: string(filter.ItemType), Sort: string(result.Sort),
		},
		Total: result.Total, Page: result.Page, PageSize: result.PageSize,
		TotalPages: result.TotalPages, HasFilters: hasFilters,
		EmptyInventory: result.Total == 0 && !hasFilters,
		EmptyFiltered:  result.Total == 0 && hasFilters,
		BeyondLast:     result.Total > 0 && result.Page > result.TotalPages,
	}
	data.ClassOptions = inventoryOptions("All classes", service.ClassValues(), data.Filter.Class)
	data.RaceOptions = inventoryOptions("All races", service.RaceValues(), data.Filter.Race)
	data.ItemTypeOptions = []inventorySelectOption{
		{Value: "", Label: "All types", Selected: data.Filter.ItemType == ""},
		{Value: string(domain.ItemTypeItem), Label: "Items", Selected: data.Filter.ItemType == string(domain.ItemTypeItem)},
		{Value: string(domain.ItemTypeSpell), Label: "Spells", Selected: data.Filter.ItemType == string(domain.ItemTypeSpell)},
	}
	sortLabels := map[domain.InventorySort]string{
		domain.InventorySortName: "Name (natural)", domain.InventorySortNameDesc: "Name (reverse natural)",
		domain.InventorySortDateAdded: "Date added (oldest)", domain.InventorySortDateAddedDesc: "Date added (newest)",
		domain.InventorySortDateOpen: "Open date (earliest)", domain.InventorySortDateOpenDesc: "Open date (latest)",
		domain.InventorySortDateBazaar: "Bazaar date (earliest)", domain.InventorySortDateBazaarDesc: "Bazaar date (latest)",
	}
	for _, sort := range domain.InventorySorts {
		data.SortOptions = append(data.SortOptions, inventorySortOption{
			Value: string(sort), Label: sortLabels[sort], Selected: sort == result.Sort,
		})
	}

	if result.TotalPages > 0 {
		if result.Page > 1 {
			previous := result.Page - 1
			if previous > result.TotalPages {
				previous = result.TotalPages
			}
			data.PrevURL = inventoryListURL(filter, previous)
		}
		if result.Page < result.TotalPages {
			data.NextURL = inventoryListURL(filter, result.Page+1)
		}
		if result.Page != 1 {
			data.FirstURL = inventoryListURL(filter, 1)
		}
		if result.Page != result.TotalPages {
			data.LastURL = inventoryListURL(filter, result.TotalPages)
		}
		data.LastPageURL = inventoryListURL(filter, result.TotalPages)
		data.PageLinks = inventoryPageLinksOf(filter, result.Page, result.TotalPages)
	}
	data.ClearFiltersURL = "/"
	return data
}

func inventoryOptions(defaultLabel string, values []string, selected string) []inventorySelectOption {
	options := make([]inventorySelectOption, 0, len(values)+1)
	options = append(options, inventorySelectOption{Value: "", Label: defaultLabel, Selected: selected == ""})
	for _, value := range values {
		options = append(options, inventorySelectOption{Value: value, Label: value, Selected: value == selected})
	}
	return options
}
func inventoryPageLinksOf(filter domain.InventoryFilter, page, totalPages int) []inventoryPageLink {
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
			Page: candidate, URL: inventoryListURL(filter, candidate), Current: candidate == page,
		})
	}
	return links
}

func inventoryListURL(filter domain.InventoryFilter, page int) string {
	values := url.Values{}
	if filter.Search != "" {
		values.Set("q", filter.Search)
	}
	if filter.Class != "" {
		values.Set("class", filter.Class)
	}
	if filter.Race != "" {
		values.Set("race", filter.Race)
	}
	if filter.ItemType != "" {
		values.Set("itemType", string(filter.ItemType))
	}
	if filter.Sort != "" && filter.Sort != domain.InventorySortName {
		values.Set("sort", string(filter.Sort))
	}
	if filter.PageSize != 0 && filter.PageSize != service.PublicInventoryDefaultPageSize {
		values.Set("pageSize", strconv.Itoa(filter.PageSize))
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/?" + encoded
	}
	return "/"
}
