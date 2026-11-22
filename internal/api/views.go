package api

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// views renders the embedded server-side templates. No template is loaded
// from disk at runtime, so a static binary is self-contained.
type views struct {
	templates *template.Template
}

func newViews(fs embed.FS) (*views, error) {
	t, err := template.ParseFS(fs, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("api: parse templates: %w", err)
	}
	return &views{templates: t}, nil
}

func (v *views) render(c *gin.Context, status int, name string, data any) {
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := v.templates.ExecuteTemplate(c.Writer, name, data); err != nil {
		// The header is already written; log through Gin's error channel.
		_ = c.Error(err)
	}
}

type pageData struct {
	Title       string
	CSRF        string
	User        *domain.User
	Error       string
	Notice      string
	ErrorFields map[string]string
}

type loginPageData struct {
	pageData
	Next string
}

type setupPageData struct {
	pageData
	Form service.SetupInput
}

type settingView struct {
	Key        string
	Label      string
	Value      string
	Type       domain.SettingType
	Secret     bool
	Configured bool
	Help       string
}

type settingsPageData struct {
	pageData
	Settings []settingView
	Form     map[string]string
}

type inventoryListPageData struct {
	pageData
	Ready           bool
	Items           []inventoryItemView
	Filter          inventoryFilterView
	ClassOptions    []inventorySelectOption
	RaceOptions     []inventorySelectOption
	ItemTypeOptions []inventorySelectOption
	SortOptions     []inventorySortOption
	Total           int
	Page            int
	PageSize        int
	TotalPages      int
	HasFilters      bool
	EmptyInventory  bool
	EmptyFiltered   bool
	BeyondLast      bool
	ClearFiltersURL string
	PrevURL         string
	NextURL         string
	FirstURL        string
	LastURL         string
	LastPageURL     string
	PageLinks       []inventoryPageLink
}

type inventoryDetailPageData struct {
	pageData
	Item inventoryItemView
}

// adminItemView is the shared administrative presentation of one row. The list,
// detail, edit and remove pages all use it so the identity fields rendered in
// the admin area cannot drift.
type adminItemView struct {
	ID                int64
	EQUID             uint64
	Name              string
	ItemType          string
	Quantity          uint32
	ClassText         string
	RaceText          string
	MetadataMissing   bool
	SourceLocation    string
	LocationGroup     string
	GivenTo           string
	Deleted           bool
	DateAddedISO      string
	DateAddedText     string
	DateOpenISO       string
	DateOpenText      string
	DateBazaarISO     string
	DateBazaarText    string
	Availability      domain.AvailabilityState
	AvailabilityLabel string
	ModAtValue        string
	ModAtText         string
	ModBy             string
}

type adminFilterView struct {
	Search        string
	ID            string
	EQUID         string
	GivenTo       string
	Metadata      string
	LocationGroup string
	ItemType      string
	Status        string
	Sort          string
	PageSize      string
}

type adminLocationOption struct {
	Value    string
	Selected bool
}

type adminListPageData struct {
	pageData
	Ready          bool
	Items          []adminItemView
	Filter         adminFilterView
	LocationGroups []adminLocationOption
	ItemTypeOpts   []inventorySelectOption
	MetadataOpts   []inventorySelectOption
	StatusOpts     []inventorySelectOption
	SortOpts       []inventorySortOption
	PageSizeOpts   []inventorySelectOption
	Total          int
	Page           int
	PageSize       int
	TotalPages     int
	Empty          bool
	BeyondLast     bool
	Notice         string
	ClearURL       string
	PrevURL        string
	NextURL        string
	FirstURL       string
	LastURL        string
	LastPageURL    string
	PageLinks      []inventoryPageLink
}

type adminFormValues struct {
	ItemType         string
	EQUID            string
	Quantity         string
	SourceLocation   string
	GivenTo          string
	Name             string
	DateOpen         string
	DateBazaar       string
	ExpectedModAt    string
	QuantityReadOnly bool
}

type adminFormPageData struct {
	pageData
	Ready    bool
	Edit     bool
	Item     adminItemView
	Form     adminFormValues
	Conflict bool
}

type auditEntryView struct {
	AuditID       int64
	EventDate     string
	EventDateText string
	UserName      string
	EventType     string
	TableName     string
	RecordID      string
	CorrelationID string
	SourceIP      string
	Notes         string
	OldValues     string
	NewValues     string
}

type adminDetailPageData struct {
	pageData
	Item    adminItemView
	History []auditEntryView
	Notice  string
}

type adminRemovePageData struct {
	pageData
	Item adminItemView
}

type auditFilterView struct {
	EventType     string
	User          string
	Table         string
	RecordID      string
	CorrelationID string
	From          string
	To            string
}

type adminAuditPageData struct {
	pageData
	Entries       []auditEntryView
	Filter        auditFilterView
	EventTypeOpts []inventorySelectOption
	Total         int
	Page          int
	PageSize      int
	TotalPages    int
	Empty         bool
	PrevURL       string
	NextURL       string
	FirstURL      string
	LastURL       string
	PageLinks     []inventoryPageLink
}

type errorPageData struct {
	pageData
	Status int
}

type importBatchView struct {
	ID               int64
	FileName         string
	SHA256           string
	SHA256Prefix     string
	Status           domain.ImportStatus
	SourceRowCount   int
	AcceptedRowCount int
	FailureReason    string
	ImportedBy       string
	ImportedAt       string
	ReadOnly         bool
}

type importsPageData struct {
	pageData
	Ready   bool
	Batches []importBatchView
}

type importDetailPageData struct {
	pageData
	Ready        bool
	Batch        importBatchView
	Uploaded     bool
	FailedUpload bool
}

func (v *views) login(c *gin.Context, status int, data loginPageData) {
	v.render(c, status, "login.html", data)
}

func (v *views) setup(c *gin.Context, status int, data setupPageData) {
	v.render(c, status, "setup.html", data)
}

func (v *views) settings(c *gin.Context, status int, data settingsPageData) {
	v.render(c, status, "settings.html", data)
}

func (v *views) inventoryList(c *gin.Context, status int, data inventoryListPageData) {
	v.render(c, status, "inventory_list.html", data)
}

func (v *views) inventoryDetail(c *gin.Context, status int, data inventoryDetailPageData) {
	v.render(c, status, "inventory_detail.html", data)
}

func (v *views) adminInventoryList(c *gin.Context, status int, data adminListPageData) {
	v.render(c, status, "admin_inventory.html", data)
}

func (v *views) adminInventoryForm(c *gin.Context, status int, data adminFormPageData) {
	v.render(c, status, "admin_inventory_form.html", data)
}

func (v *views) adminInventoryDetail(c *gin.Context, status int, data adminDetailPageData) {
	v.render(c, status, "admin_inventory_detail.html", data)
}

func (v *views) adminRemove(c *gin.Context, status int, data adminRemovePageData) {
	v.render(c, status, "admin_remove.html", data)
}

func (v *views) adminAudit(c *gin.Context, status int, data adminAuditPageData) {
	v.render(c, status, "admin_audit.html", data)
}

func (v *views) errorPage(c *gin.Context, status int, message string) {
	v.render(c, status, "error.html", errorPageData{
		pageData: pageData{
			Title: http.StatusText(status),
			CSRF:  CSRF(c),
			Error: message,
		},
		Status: status,
	})
}

func (v *views) imports(c *gin.Context, status int, data importsPageData) {
	v.render(c, status, "imports.html", data)
}

func (v *views) importDetail(c *gin.Context, status int, data importDetailPageData) {
	v.render(c, status, "import_detail.html", data)
}

func importBatchViews(batches []domain.ImportBatch) []importBatchView {
	out := make([]importBatchView, 0, len(batches))
	for _, batch := range batches {
		out = append(out, importBatchViewOf(batch))
	}
	return out
}

func importBatchViewOf(batch domain.ImportBatch) importBatchView {
	sha := ""
	if batch.FileSha256 != nil {
		sha = *batch.FileSha256
	}
	prefix := sha
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	view := importBatchView{
		ID: batch.ImportBatchID, FileName: batch.FileName, SHA256: sha,
		SHA256Prefix: prefix, Status: batch.Status,
		SourceRowCount: intValue(batch.SourceRowCount), AcceptedRowCount: intValue(batch.AcceptedRowCount),
		ImportedBy: batch.ImportedBy, ImportedAt: batch.ImportedAt.UTC().Format("2006-01-02 15:04 UTC"),
		ReadOnly: batch.Status != domain.ImportStatusUploaded,
	}
	if batch.FailureReason != nil {
		view.FailureReason = *batch.FailureReason
	}
	return view
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
