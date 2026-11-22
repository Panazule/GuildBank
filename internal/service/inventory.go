package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Panazule/guildbank/internal/domain"
)

const (
	// PublicInventoryDefaultPageSize and PublicInventoryMaxPageSize are the
	// approved public pagination bounds (PD4-05/PD4-13).
	PublicInventoryDefaultPageSize = 50
	PublicInventoryMaxPageSize     = 100
	PublicInventoryMaxPage         = 10000
	PublicInventoryMaxSearchRunes  = 128
)

// InventoryStore is the narrow repository surface used by InventoryService.
// It exposes only public reads and no write, audit or metadata operation.
type InventoryStore interface {
	Ready() bool
	ListPublicInventory(ctx context.Context, filter domain.InventoryFilter) (domain.InventoryRecordPage, error)
	GetPublicInventory(ctx context.Context, id int64) (domain.InventoryRecord, error)
}

// InventorySettingsReader reads the validated EQResource base URL setting.
// Keeping this narrow lets settings failures degrade links without failing a
// valid inventory read.
type InventorySettingsReader interface {
	Values(ctx context.Context) (map[string]string, error)
}

// InventoryService owns public filter validation, availability calculation,
// EQResource construction and mapping to the public projection.
type InventoryService struct {
	store    InventoryStore
	settings InventorySettingsReader
	now      func() time.Time
	logger   *slog.Logger
}

// NewInventoryService wires the public inventory read path. A nil store
// produces a degraded service whose Ready() is false.
func NewInventoryService(store InventoryStore, settings InventorySettingsReader, now func() time.Time, logger *slog.Logger) *InventoryService {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &InventoryService{store: store, settings: settings, now: now, logger: logger}
}

// Ready reports whether the guild store is available.
func (s *InventoryService) Ready() bool {
	return s != nil && s.store != nil && s.store.Ready()
}

func (s *InventoryService) requireReady() error {
	if !s.Ready() {
		return domain.ErrUnavailable
	}
	return nil
}

// NormalizeInventoryFilter trims and validates every public filter field. It
// returns a field-keyed ValidationError so both HTML and JSON paths report the
// same contract.
func NormalizeInventoryFilter(filter domain.InventoryFilter) (domain.InventoryFilter, error) {
	fields := make(map[string]string)

	filter.Search = strings.TrimSpace(filter.Search)
	if utf8.RuneCountInString(filter.Search) > PublicInventoryMaxSearchRunes {
		fields["q"] = fmt.Sprintf("q must be at most %d characters", PublicInventoryMaxSearchRunes)
	}

	class := strings.TrimSpace(filter.Class)
	switch {
	case class == "" || strings.EqualFold(class, "ALL"):
		filter.Class = ""
	default:
		canonical, ok := CanonicalClass(class)
		if !ok {
			fields["class"] = "class must be a defined class abbreviation or ALL"
		}
		filter.Class = canonical
	}

	race := strings.TrimSpace(filter.Race)
	switch {
	case race == "" || strings.EqualFold(race, "ALL"):
		filter.Race = ""
	default:
		canonical, ok := CanonicalRace(race)
		if !ok {
			fields["race"] = "race must be a defined race or ALL"
		}
		filter.Race = canonical
	}

	itemType := strings.TrimSpace(string(filter.ItemType))
	switch {
	case itemType == "" || strings.EqualFold(itemType, "all"):
		filter.ItemType = ""
	case strings.EqualFold(itemType, string(domain.ItemTypeItem)):
		filter.ItemType = domain.ItemTypeItem
	case strings.EqualFold(itemType, string(domain.ItemTypeSpell)):
		filter.ItemType = domain.ItemTypeSpell
	default:
		fields["itemType"] = "itemType must be Item, Spell or all"
	}

	sort := domain.InventorySort(strings.ToLower(strings.TrimSpace(string(filter.Sort))))
	if sort == "" {
		sort = domain.InventorySortName
	}
	if !sort.Valid() {
		fields["sort"] = "sort must be one of the approved values"
	}
	filter.Sort = sort

	if filter.Page < 1 || filter.Page > PublicInventoryMaxPage {
		fields["page"] = fmt.Sprintf("page must be between 1 and %d", PublicInventoryMaxPage)
	}
	if filter.PageSize < 1 || filter.PageSize > PublicInventoryMaxPageSize {
		fields["pageSize"] = fmt.Sprintf("pageSize must be between 1 and %d", PublicInventoryMaxPageSize)
	}

	if len(fields) > 0 {
		return domain.InventoryFilter{}, &domain.ValidationError{Fields: fields}
	}
	return filter, nil
}

// ListPublic validates the filter, captures one UTC date and maps the
// repository page to the public projection.
func (s *InventoryService) ListPublic(ctx context.Context, filter domain.InventoryFilter) (domain.PublicInventoryPage, error) {
	if err := s.requireReady(); err != nil {
		return domain.PublicInventoryPage{}, err
	}
	normalized, err := NormalizeInventoryFilter(filter)
	if err != nil {
		return domain.PublicInventoryPage{}, err
	}
	today := utcDate(s.now())
	page, err := s.store.ListPublicInventory(ctx, normalized)
	if err != nil {
		return domain.PublicInventoryPage{}, err
	}
	baseURL := s.eqResourceBaseURL(ctx)
	items := make([]domain.PublicInventoryItem, 0, len(page.Records))
	for _, record := range page.Records {
		items = append(items, publicInventoryItem(record, today, baseURL))
	}
	totalPages := 0
	if page.Total > 0 {
		totalPages = (page.Total + normalized.PageSize - 1) / normalized.PageSize
	}
	return domain.PublicInventoryPage{
		Items:      items,
		Filter:     normalized,
		Page:       normalized.Page,
		PageSize:   normalized.PageSize,
		Total:      page.Total,
		TotalPages: totalPages,
		Sort:       normalized.Sort,
	}, nil
}

// GetPublic reads and maps one active inventory record. Non-positive IDs are
// treated as not found, matching the route contract.
func (s *InventoryService) GetPublic(ctx context.Context, id int64) (domain.PublicInventoryItem, error) {
	if err := s.requireReady(); err != nil {
		return domain.PublicInventoryItem{}, err
	}
	if id <= 0 {
		return domain.PublicInventoryItem{}, domain.ErrNotFound
	}
	record, err := s.store.GetPublicInventory(ctx, id)
	if err != nil {
		return domain.PublicInventoryItem{}, err
	}
	return publicInventoryItem(record, utcDate(s.now()), s.eqResourceBaseURL(ctx)), nil
}

func (s *InventoryService) eqResourceBaseURL(ctx context.Context) string {
	if s.settings == nil {
		return ""
	}
	values, err := s.settings.Values(ctx)
	if err != nil {
		s.logger.Warn("public inventory settings unavailable; EQResource links suppressed",
			"error", err)
		return ""
	}
	return values["EQResourceBaseUrl"]
}

func publicInventoryItem(record domain.InventoryRecord, today time.Time, baseURL string) domain.PublicInventoryItem {
	return domain.PublicInventoryItem{
		ID:              record.GuildBankItemID,
		EQUID:           record.EQUID,
		Name:            record.Name,
		ItemType:        record.ItemType,
		Quantity:        record.Quantity,
		ClassString:     record.ClassString,
		RaceString:      record.RaceString,
		MetadataMissing: record.MetadataMissing,
		DateAdded:       utcDate(record.DateAdded),
		DateOpen:        utcDate(record.DateOpen),
		DateBazaar:      utcDate(record.DateBazaar),
		Availability:    Availability(today, record.DateOpen, record.DateBazaar),
		EQResourceURL:   BuildEQResourceURL(baseURL, record.EQUID),
	}
}

// Availability implements PD4-11 with inclusive DateOpen/DateBazaar boundaries
// and a single UTC calendar date. An inverted window is never reported open.
func Availability(today, dateOpen, dateBazaar time.Time) domain.AvailabilityState {
	today = utcDate(today)
	dateOpen = utcDate(dateOpen)
	dateBazaar = utcDate(dateBazaar)
	if today.Before(dateOpen) {
		return domain.AvailabilityUpcoming
	}
	if today.After(dateBazaar) {
		return domain.AvailabilityBazaar
	}
	return domain.AvailabilityOpen
}

// BuildEQResourceURL revalidates the stored setting at point of use. It returns
// nil for any invalid setting or a zero EQUID and never performs a network
// request.
func BuildEQResourceURL(base string, equid uint64) *string {
	if equid == 0 {
		return nil
	}
	if !validEQResourceURL(base) {
		return nil
	}
	u, err := url.Parse(base)
	if err != nil || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return nil
	}
	if u.Scheme != "https" || !strings.EqualFold(u.Host, "items.eqresource.com") ||
		u.Path != "/items.php" || u.RawQuery != "id=" {
		return nil
	}
	u.RawQuery = "id=" + strconv.FormatUint(equid, 10)
	value := u.String()
	return &value
}

func utcDate(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

// IsInventoryUnavailable reports the documented public-store failure states.
func IsInventoryUnavailable(err error) bool {
	return errors.Is(err, domain.ErrUnavailable)
}
