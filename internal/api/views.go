package api

import (
	"bytes"
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

// render buffers the template output so a rendering failure cannot emit a
// status with a partial or empty body. Errors become a logged 500 envelope,
// which is diagnosable instead of a blank page.
func (v *views) render(c *gin.Context, status int, name string, data any) {
	var buf bytes.Buffer
	if err := v.templates.ExecuteTemplate(&buf, name, data); err != nil {
		_ = c.Error(err)
		writeError(c, http.StatusInternalServerError, "internal_error", "the page could not be rendered")
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	_, _ = c.Writer.Write(buf.Bytes())
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
	// Discord shows the alternative sign-in button. It is true only when an
	// Administrator enabled Discord sign-in with a complete configuration.
	Discord bool
	// SelfRegistration shows the Create account link. It is true only when the
	// settings switch is on and Discord is fully configured.
	SelfRegistration bool
	// DiscordNotice is fixed text for a callback outcome, never provider text.
	DiscordNotice string
}

// registerPageData backs the public registration landing page. It renders even
// when the flow is unavailable so the link never 404s.
type registerPageData struct {
	pageData
	Available bool
	// GuildConfigured states which approval outcome applies before consent:
	// with no guild ID every registration waits for an officer.
	GuildConfigured bool
}

// registerCompletePageData is the username form. The Discord identity comes from
// the signed ticket, never from a form field.
type registerCompletePageData struct {
	pageData
	Form            registrationForm
	DiscordName     string
	Member          bool
	GuildConfigured bool
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
	ID              int64
	EQUID           uint64
	Name            string
	ItemType        string
	Quantity        uint32
	ClassText       string
	RaceText        string
	MetadataMissing bool
	// UnresolvedIdentity marks the documented placeholder EQUID (4294967295):
	// the row has no resolved item identity and its stored name is authoritative.
	UnresolvedIdentity bool
	SourceLocation     string
	LocationGroup      string
	LocationGroupLabel string
	GivenTo            string
	Deleted            bool
	// Restorable reports whether the admin list may offer the Restore action:
	// the row is administratively removed (DeletedFg = true with no recipient)
	// and was not absorbed by the item-consolidation command. It is presentation
	// only; the service re-checks the same rules under lock.
	Restorable        bool
	DateAddedISO      string
	DateAddedText     string
	DateOpenISO       string
	DateOpenText      string
	DateBazaarISO     string
	DateBazaarText    string
	Availability      domain.AvailabilityState
	AvailabilityLabel string
	StatusLabel       string
	StatusClass       string
	// IssuedURL links an issued row to the recipient's issued queue. It is empty
	// for active and administratively removed rows.
	IssuedURL  string
	ModAtValue string
	ModAtText  string
	ModBy      string
	// MovementBacked marks a movement-derived issued row. It is true only for
	// the Issued projection and the issued half of the All tab; lot-derived
	// rows leave it false so the existing action and selection branches apply.
	MovementBacked bool
	MovementID     int64
	// RowKey is unique across both sources ("l:<LotID>" vs "m:<MovementID>")
	// and anchors the row in the shared page.
	RowKey         string
	IssuedQuantity uint32
	IssuedAtISO    string
	IssuedAtText   string
	IssuedBy       string
	// ReturnURL is the movement-targeted return link. It is empty for
	// lot-derived rows, whose Return (if any) uses the lot route.
	ReturnURL string
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
	Label    string
	Selected bool
}

// adminStatusTab is one lifecycle-state shortcut on the admin list. The URL is
// the same filtered view with the tab's status applied and the page reset to 1.
type adminStatusTab struct {
	Value   string
	Label   string
	URL     string
	Current bool
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
	StatusTabs     []adminStatusTab
	SortOpts       []inventorySortOption
	PageSizeOpts   []inventorySelectOption
	// IssuedHint is set when the officer filtered Active by recipient: active
	// rows never carry a recipient, so the page explains the empty result and
	// links to the same recipient's issued queue.
	IssuedHint    bool
	IssuedHintURL string
	Total         int
	Page          int
	PageSize      int
	TotalPages    int
	Empty         bool
	BeyondLast    bool
	Notice        string
	ClearURL      string
	// SelfURL is the current list URL with every active filter, carried through
	// the row actions so an officer returns to the same filtered view.
	SelfURL     string
	PrevURL     string
	NextURL     string
	FirstURL    string
	LastURL     string
	LastPageURL string
	PageLinks   []inventoryPageLink
}

type adminFormValues struct {
	ItemType       string
	EQUID          string
	Quantity       string
	SourceLocation string
	GivenTo        string
	Name           string
	DateOpen       string
	DateBazaar     string
	ExpectedModAt  string
}

type adminFormPageData struct {
	pageData
	Ready bool
	Edit  bool
	Item  adminItemView
	Form  adminFormValues
	// Candidates is populated when an entered item name matched more than one
	// metadata row. The officer selects one; the server never guesses.
	Candidates []itemCandidateResponse
	Conflict   bool
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
	// ReturnTo is the sanitized inventory-list URL the officer came from, so the
	// page and its actions can return to the same filtered view.
	ReturnTo string
	// NameWarning is the non-blocking advisory shown when a created name
	// differed from the item metadata name.
	NameWarning string
}

type adminRemovePageData struct {
	pageData
	Item adminItemView
	// ReturnTo is the sanitized inventory-list URL the officer came from.
	ReturnTo string
}

// adminRestorePageData backs the Restore confirmation page. It mirrors the
// Remove page so the officer sees the record and the reversible effect before
// the POST reaches the existing restore workflow.
type adminRestorePageData struct {
	pageData
	Item adminItemView
	// ReturnTo is the sanitized inventory-list URL the officer came from.
	ReturnTo string
}

// adminMovementView is the presentation of one ledger row. It deliberately
// omits the idempotency key and source address: the movement history is a
// business record, not a transport log.
type adminMovementView struct {
	MovementID        int64
	Type              string
	Quantity          uint32
	Recipient         string
	ActorUserName     string
	OccurredAtValue   string
	OccurredAtText    string
	FromLocation      string
	ToLocation        string
	RemainingQuantity uint32
	Note              string
	CorrelationID     string
	RelatedItemID     string
}

// adminMovementFormValues carries the re-posted officer form values so a
// validation failure does not lose what the officer typed.
type adminMovementFormValues struct {
	Quantity       string
	Recipient      string
	Note           string
	ExpectedModAt  string
	IdempotencyKey string
}

// adminMovementFormPageData backs the Issue and Return forms. Mode states in
// plain text what the submit will do, so the officer sees the effect before
// acting. PartialSupported reports the quantity-based issue rule (an active lot
// with more than one unit on hand supports partial issue); it is false on the
// return form.
type adminMovementFormPageData struct {
	pageData
	Ready            bool
	Item             adminItemView
	Form             adminMovementFormValues
	Mode             string
	PartialSupported bool
	// FormAction is the POST target. It is the lot route for a lot-level form
	// and the movement route for a movement-derived issued row.
	FormAction string
	// ReturnTo is the sanitized inventory-list URL the officer came from.
	ReturnTo string
}

type adminMovementFilterView struct {
	MovementType  string
	Recipient     string
	CorrelationID string
	PageSize      string
}

// adminHistoryBannerView is the state-driven action banner on the movement
// history page. It offers exactly one canonical action for the lot's current
// state (Issue, Return or Restore).
type adminHistoryBannerView struct {
	Text        string
	ActionLabel string
	// ActionURL is the target of the banner action. When ActionForm is true the
	// action is a POST form (restore); otherwise it is a GET link.
	ActionURL string
	// ReturnTo is the sanitized inventory-list URL carried through the banner
	// action so the officer lands back on the same filtered view.
	ReturnTo   string
	ActionForm bool
}

type adminMovementHistoryPageData struct {
	pageData
	Ready     bool
	Item      adminItemView
	Movements []adminMovementView
	Filter    adminMovementFilterView
	TypeOpts  []inventorySelectOption
	Banner    adminHistoryBannerView
	// ReturnTo is the sanitized inventory-list URL the officer came from.
	ReturnTo   string
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	Empty      bool
	PrevURL    string
	NextURL    string
	FirstURL   string
	LastURL    string
	PageLinks  []inventoryPageLink
}

// adminBulkRemovePageData is the page-local bulk-removal preview (AI-01).
// Targets are the opaque "id|modAt" tokens re-posted by the apply form.
type adminBulkRemovePageData struct {
	pageData
	Items   []adminItemView
	Targets []string
	// ReturnTo is the sanitized inventory-list URL the officer came from.
	ReturnTo string
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

// userRowView is the shared presentation of one account. The list and detail
// pages use it so the status wording cannot drift between them.
type userRowView struct {
	ID        int64
	UserName  string
	Role      domain.Role
	RoleLabel string
	Disabled  bool
	Pending   bool
	// StatusLabel is the derived status text (D-SR design §2.3). It is never
	// stored: approval and the administrative penalty stay two axes.
	StatusLabel        string
	Origin             string
	DiscordUserName    string
	GuildVerifiedText  string
	MustChangePassword bool
	LastLoginText      string
	CreatedText        string
	ModBy              string
	IsSelf             bool
	// Locked marks the last enabled Administrator: the UI disables role, status
	// and delete controls, and the store refuses the same changes.
	Locked     bool
	CanDisable bool
	CanDelete  bool
	// CanApprove marks a pending account whose approval the detail page may
	// offer. CanWithdrawApproval marks an approved account whose approval may be
	// withdrawn: never the operator's own account and never the last enabled
	// administrator.
	CanApprove          bool
	CanWithdrawApproval bool
}

// roleSelectOption is one entry of the role picker.
type roleSelectOption struct {
	Value    string
	Label    string
	Selected bool
}

type userFormValues struct {
	UserName        string
	AccessLevel     string
	Password        string
	ConfirmPassword string
	ConfirmUserName string
}

type usersPageData struct {
	pageData
	Users         []userRowView
	Filter        string
	Status        string
	StatusOptions []inventorySelectOption
	Total         int
	EnabledAdmins int
	// PendingCount drives the approval queue banner and the "Review" link.
	PendingCount int
}

type userCreatePageData struct {
	pageData
	Form  userFormValues
	Roles []roleSelectOption
}

type userDetailPageData struct {
	pageData
	Row           userRowView
	Roles         []roleSelectOption
	EnabledAdmins int
	Form          userFormValues
	// Characters is the account's profile list. Administrators may remove a
	// name here (D4) but can never move it to another account.
	Characters []characterView
}

type accountPasswordPageData struct {
	pageData
	UserName string
	// Forced reports the MustChangePassword state, so the page can explain why
	// the account cannot reach anything else yet.
	Forced bool
	// Setting is true when the account has no password yet. The page then shows
	// Set Password and omits the current-password field (D-SR-15/19).
	Setting bool
	// Action is the form target: the set or the change endpoint (D-SR-19).
	Action string
}

func (v *views) adminUsers(c *gin.Context, status int, data usersPageData) {
	v.render(c, status, "admin_users.html", data)
}

func (v *views) adminUserCreate(c *gin.Context, status int, data userCreatePageData) {
	v.render(c, status, "admin_user_new.html", data)
}

func (v *views) adminUserDetail(c *gin.Context, status int, data userDetailPageData) {
	v.render(c, status, "admin_user_detail.html", data)
}

func (v *views) accountPassword(c *gin.Context, status int, data accountPasswordPageData) {
	v.render(c, status, "account_password.html", data)
}

// characterView is the shared presentation of one profile character name.
type characterView struct {
	ID          int64
	Name        string
	CreatedText string
}

type profilePageData struct {
	pageData
	UserName      string
	RoleLabel     string
	Characters    []characterView
	MaxCharacters int
	Discord       discordSectionView
	// HasPassword selects between Set Password and Change Password on the
	// dedicated Password section (D-SR-20). It is a presentation flag only.
	HasPassword bool
}

// discordSectionView is the profile-page state of the Discord card. Available is
// false whenever an administrator has not enabled linking, and the card then
// only explains that.
type discordSectionView struct {
	Available  bool
	Linked     bool
	UserName   string
	LinkedText string
	GuildID    string
	// CanUnlink is false for a passwordless account: Discord is its only way in,
	// so the unlink control stays hidden and the service refuses it anyway
	// (D-SR-18).
	CanUnlink bool
}

func (v *views) accountProfile(c *gin.Context, status int, data profilePageData) {
	v.render(c, status, "account_profile.html", data)
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
	IgnoredRowCount  int
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

func (v *views) register(c *gin.Context, status int, data registerPageData) {
	v.render(c, status, "register.html", data)
}

func (v *views) registerComplete(c *gin.Context, status int, data registerCompletePageData) {
	v.render(c, status, "register_complete.html", data)
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

func (v *views) adminRestore(c *gin.Context, status int, data adminRestorePageData) {
	v.render(c, status, "admin_restore.html", data)
}

func (v *views) adminBulkRemove(c *gin.Context, status int, data adminBulkRemovePageData) {
	v.render(c, status, "admin_bulk_remove.html", data)
}

func (v *views) adminMovementForm(c *gin.Context, status int, name string, data adminMovementFormPageData) {
	v.render(c, status, name, data)
}

func (v *views) adminMovementHistory(c *gin.Context, status int, data adminMovementHistoryPageData) {
	v.render(c, status, "admin_inventory_history.html", data)
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
		IgnoredRowCount: intValue(batch.IgnoredRowCount),
		ImportedBy:      batch.ImportedBy, ImportedAt: batch.ImportedAt.UTC().Format("2006-01-02 15:04 UTC"),
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
