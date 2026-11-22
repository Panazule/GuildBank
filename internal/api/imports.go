package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

type importsHandler struct {
	imports *service.ImportService
	views   *views
	logger  *slog.Logger
}

type batchResponse struct {
	ID               int64              `json:"id"`
	FileName         string             `json:"fileName"`
	SHA256           *string            `json:"sha256"`
	Status           domain.ImportStatus `json:"status"`
	SourceRowCount   *int               `json:"sourceRowCount"`
	AcceptedRowCount *int               `json:"acceptedRowCount"`
	FailureReason    *string            `json:"failureReason"`
	ImportedBy       string             `json:"importedBy"`
	ImportedByUserID *int64             `json:"importedByUserId"`
	ImportedAt       time.Time          `json:"importedAt"`
}

type rowResponse struct {
	ImportID      int64                  `json:"importId"`
	SourceLine    *int                   `json:"sourceLine"`
	ItemType      domain.ItemType        `json:"itemType"`
	EQUID         uint64                 `json:"equid"`
	Name          string                 `json:"name"`
	Quantity      uint32                 `json:"quantity"`
	LocationRaw   string                 `json:"locationRaw"`
	LocationGroup string                 `json:"locationGroup"`
	Selected      bool                   `json:"selected"`
	Warnings      []domain.ImportWarning `json:"warnings"`
}

type locationResponse struct {
	Group         string `json:"group"`
	Sort          string `json:"sort"`
	RowCount      int    `json:"rowCount"`
	SelectedCount int    `json:"selectedCount"`
}

type summaryResponse struct {
	BatchID                  int64              `json:"batchId"`
	Status                   domain.ImportStatus `json:"status"`
	SelectedRows             int                `json:"selectedRows"`
	StagedRows               int                `json:"stagedRows"`
	SelectedLocations        int                `json:"selectedLocations"`
	TotalLocations           int                `json:"totalLocations"`
	ItemRows                 int                `json:"itemRows"`
	ItemUnits                uint64             `json:"itemUnits"`
	SpellRows                int                `json:"spellRows"`
	SpellQuantity            uint64             `json:"spellQuantity"`
	DistinctSpells           int                `json:"distinctSpells"`
	NewSpells                int                `json:"newSpells"`
	MergedSpells             int                `json:"mergedSpells"`
	InventoryRecordsToCreate uint64             `json:"inventoryRecordsToCreate"`
	WarningRows              int                `json:"warningRows"`
	TotalRecordsToImport     uint64             `json:"totalRecordsToImport"`
}

func (h *importsHandler) page(c *gin.Context) {
	ready := h.imports.Ready()
	batches := []domain.ImportBatch{}
	var err error
	if ready {
		batches, err = h.imports.ListBatches(c.Request.Context(), 50, 0)
		if err != nil {
			respondError(c, h.logger, err)
			return
		}
	}
	h.views.imports(c, http.StatusOK, importsPageData{
		pageData: pageData{Title: "Imports", CSRF: CSRF(c), User: currentUserPointer(c)},
		Ready:    ready, Batches: importBatchViews(batches),
	})
}

func (h *importsHandler) detailPage(c *gin.Context) {
	batchID, ok := parseBatchID(c)
	if !ok {
		return
	}
	if !h.imports.Ready() {
		h.views.importDetail(c, http.StatusOK, importDetailPageData{
			pageData: pageData{Title: "Import batch", CSRF: CSRF(c), User: currentUserPointer(c)},
			Ready:    false,
		})
		return
	}
	batch, err := h.imports.GetBatch(c.Request.Context(), batchID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			h.views.errorPage(c, http.StatusNotFound, "The requested import batch does not exist.")
			return
		}
		respondError(c, h.logger, err)
		return
	}
	h.views.importDetail(c, http.StatusOK, importDetailPageData{
		pageData: pageData{Title: "Import batch", CSRF: CSRF(c), User: currentUserPointer(c)},
		Ready:    true, Batch: importBatchViewOf(batch),
		Uploaded: c.Query("uploaded") == "1", FailedUpload: c.Query("failed") == "1",
	})
}

func (h *importsHandler) list(c *gin.Context) {
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			writeError(c, http.StatusUnprocessableEntity, "validation_failed", "limit must be between 1 and 200")
			return
		}
		limit = value
	}
	var beforeID int64
	if raw := c.Query("beforeId"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			writeError(c, http.StatusUnprocessableEntity, "validation_failed", "beforeId must be positive")
			return
		}
		beforeID = value
	}
	batches, err := h.imports.ListBatches(c.Request.Context(), limit, beforeID)
	if err != nil {
		h.respondImportError(c, err)
		return
	}
	var next *int64
	if len(batches) == limit {
		value := batches[len(batches)-1].ImportBatchID
		next = &value
	}
	out := make([]batchResponse, 0, len(batches))
	for _, batch := range batches {
		out = append(out, batchResponseOf(batch))
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"batches": out, "nextBeforeId": next}})
}

func (h *importsHandler) get(c *gin.Context) {
	batchID, ok := parseBatchID(c)
	if !ok {
		return
	}
	batch, err := h.imports.GetBatch(c.Request.Context(), batchID)
	if err != nil {
		h.respondImportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"batch": batchResponseOf(batch)}})
}

func (h *importsHandler) upload(c *gin.Context) {
	if !h.imports.Ready() {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "the guild database is unavailable")
		return
	}
	defer func() {
		if c.Request.MultipartForm != nil {
			_ = c.Request.MultipartForm.RemoveAll()
		}
	}()
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			writeError(c, http.StatusRequestEntityTooLarge, "file_too_large", "the uploaded file exceeds 10 MiB")
			return
		}
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"the multipart upload could not be read", map[string]string{"file": "a single file part named file is required"})
		return
	}
	files := c.Request.MultipartForm.File["file"]
	if len(files) != 1 {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"exactly one file part named file is required", map[string]string{"file": "a single file is required"})
		return
	}
	file, err := files[0].Open()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "the uploaded file could not be read")
		return
	}
	defer file.Close()
	principal, _ := CurrentSession(c)
	actor := domain.Actor{UserID: principal.User.ID, UserName: principal.User.UserName, IsAdmin: principal.User.IsAdmin()}
	result, err := h.imports.Upload(c.Request.Context(), actor, files[0].Filename, file,
		GetRequestID(c), c.ClientIP())
	if err != nil {
		h.respondUploadError(c, err)
		return
	}
	c.Header("X-Import-Batch-ID", strconv.FormatInt(result.Batch.ImportBatchID, 10))
	if result.DuplicateOf != nil {
		c.Header("X-Import-Duplicate-Of", strconv.FormatInt(*result.DuplicateOf, 10))
	}
	if wantsHTML(c) {
		target := fmt.Sprintf("/admin/imports/%d?uploaded=1", result.Batch.ImportBatchID)
		if result.Batch.Status == domain.ImportStatusFailed {
			target = fmt.Sprintf("/admin/imports/%d?failed=1", result.Batch.ImportBatchID)
		}
		c.Redirect(http.StatusSeeOther, target)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{
		"batch": batchResponseOf(result.Batch), "structuralRows": result.StructuralRows,
		"discardedRows": result.DiscardedRows, "blankRows": result.BlankRows,
		"duplicateOf": result.DuplicateOf, "warningCounts": result.WarningCounts,
	}})
}

func (h *importsHandler) respondUploadError(c *gin.Context, err error) {
	var failure *domain.ImportFailure
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		h.respondImportError(c, err)
	case errors.Is(err, service.ErrFileTooLarge):
		writeError(c, http.StatusRequestEntityTooLarge, "file_too_large", "the uploaded file exceeds 10 MiB")
	case errors.As(err, &failure):
		c.Header("X-Import-Batch-ID", strconv.FormatInt(failure.BatchID, 10))
		code := "invalid_import_file"
		var parseErr *service.ParseError
		if errors.As(failure.Err, &parseErr) && parseErr.Code == "metadata_unavailable" {
			code = "dependency_unavailable"
		}
		if code == "dependency_unavailable" {
			writeError(c, http.StatusServiceUnavailable, code, "Items metadata is unavailable")
			return
		}
		writeErrorFields(c, http.StatusUnprocessableEntity, code,
			"the import file was rejected", map[string]string{"file": failure.Err.Error()})
	case errors.Is(err, domain.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "the guild database is unavailable")
	default:
		h.respondImportError(c, err)
	}
}

func (h *importsHandler) locations(c *gin.Context) {
	batchID, ok := parseBatchID(c)
	if !ok {
		return
	}
	locations, err := h.imports.Locations(c.Request.Context(), batchID)
	if err != nil {
		h.respondImportError(c, err)
		return
	}
	out := make([]locationResponse, 0, len(locations))
	for _, location := range locations {
		out = append(out, locationResponse{
			Group: location.LocationGroup, Sort: location.LocationSort,
			RowCount: location.RowCount, SelectedCount: location.SelectedCount,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"locations": out}})
}

func (h *importsHandler) rows(c *gin.Context) {
	batchID, ok := parseBatchID(c)
	if !ok {
		return
	}
	page, ok := parsePageQuery(c, "page", 1, 1, 0)
	if !ok {
		return
	}
	pageSize, ok := parsePageQuery(c, "pageSize", 50, 1, 200)
	if !ok {
		return
	}
	selected := c.DefaultQuery("selected", "all")
	filter := domain.ImportRowFilter{
		LocationGroup: strings.TrimSpace(c.Query("locationGroup")),
		Selected:      selected, Search: strings.TrimSpace(c.Query("q")),
		Page: page, PageSize: pageSize,
	}
	result, err := h.imports.Rows(c.Request.Context(), batchID, filter)
	if err != nil {
		h.respondImportError(c, err)
		return
	}
	rows := make([]rowResponse, 0, len(result.Rows))
	for _, row := range result.Rows {
		warnings := row.Warnings
		if warnings == nil {
			warnings = []domain.ImportWarning{}
		}
		rows = append(rows, rowResponse{
			ImportID: row.ImportID, SourceLine: row.SourceLine, ItemType: row.ItemType,
			EQUID: row.EQUID, Name: row.Name, Quantity: row.Quantity,
			LocationRaw: row.LocationRaw, LocationGroup: row.LocationGroup,
			Selected: row.SelectedFg, Warnings: warnings,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"rows": rows, "page": result.Page, "pageSize": result.PageSize, "total": result.Total,
	}})
}

func (h *importsHandler) rowIDs(c *gin.Context) {
	batchID, ok := parseBatchID(c)
	if !ok {
		return
	}
	locationGroup := strings.TrimSpace(c.Query("locationGroup"))
	ids, err := h.imports.RowIDs(c.Request.Context(), batchID, locationGroup)
	if err != nil {
		h.respondImportError(c, err)
		return
	}
	if ids == nil {
		ids = []int64{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"importIds": ids, "total": len(ids)}})
}

func (h *importsHandler) selection(c *gin.Context) {
	batchID, ok := parseBatchID(c)
	if !ok {
		return
	}
	ids, err := h.imports.Selection(c.Request.Context(), batchID)
	if err != nil {
		h.respondImportError(c, err)
		return
	}
	if ids == nil {
		ids = []int64{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"batchId": batchID, "selectedImportIds": ids}})
}

type selectionPayload struct {
	SelectedImportIDs *[]int64 `json:"selectedImportIds"`
}

func (h *importsHandler) putSelection(c *gin.Context) {
	batchID, ok := parseBatchID(c)
	if !ok {
		return
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 2<<20))
	decoder.DisallowUnknownFields()
	var payload selectionPayload
	if err := decoder.Decode(&payload); err != nil || payload.SelectedImportIDs == nil {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"selectedImportIds is required", map[string]string{"selectedImportIds": "a complete replacement array is required"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(c, http.StatusUnprocessableEntity, "validation_failed", "the request body must contain one JSON object")
		return
	}
	summary, err := h.imports.SetSelection(c.Request.Context(), batchID, *payload.SelectedImportIDs)
	if err != nil {
		h.respondImportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"batchId": batchID, "selectedRows": summary.SelectedRows,
		"summary": summaryResponseOf(summary),
	}})
}

func (h *importsHandler) summary(c *gin.Context) {
	batchID, ok := parseBatchID(c)
	if !ok {
		return
	}
	summary, err := h.imports.Summary(c.Request.Context(), batchID)
	if err != nil {
		h.respondImportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": summaryResponseOf(summary)})
}

func (h *importsHandler) commit(c *gin.Context) {
	batchID, ok := parseBatchID(c)
	if !ok {
		return
	}
	principal, _ := CurrentSession(c)
	actor := domain.Actor{UserID: principal.User.ID, UserName: principal.User.UserName, IsAdmin: principal.User.IsAdmin()}
	result, err := h.imports.Commit(c.Request.Context(), batchID, actor, GetRequestID(c), c.ClientIP())
	if err != nil {
		h.respondImportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"batchId": result.BatchID, "status": result.Status,
		"itemRecordsCreated": result.ItemRecordsCreated,
		"spellRecordsCreated": result.SpellRecordsCreated,
		"spellRecordsMerged": result.SpellRecordsMerged,
		"spellQuantityAdded": result.SpellQuantityAdded,
		"totalRecordsAffected": result.TotalRecordsAffected,
		"duplicateSpellTargets": result.DuplicateSpellTargets,
		"warnings": result.Warnings, "durationMs": result.DurationMS,
	}})
}

func parseBatchID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("batchID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "not_found", "import batch not found")
		return 0, false
	}
	return id, true
}

func parsePageQuery(c *gin.Context, key string, fallback, minimum, maximum int) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || (maximum > 0 && value > maximum) {
		message := key + " is invalid"
		if maximum > 0 {
			message = fmt.Sprintf("%s must be between %d and %d", key, minimum, maximum)
		}
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed", message,
			map[string]string{key: message})
		return 0, false
	}
	return value, true
}

func (h *importsHandler) respondImportError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	var retry *domain.RetryableError
	switch {
	case errors.As(err, &validation):
		respondError(c, h.logger, err)
	case errors.Is(err, service.ErrSelectionEmpty):
		writeErrorFields(c, http.StatusUnprocessableEntity, "selection_empty",
			"select at least one staged row before committing", map[string]string{"selection": "nothing is selected"})
	case errors.Is(err, service.ErrSettingsInvalid):
		writeError(c, http.StatusUnprocessableEntity, "settings_invalid", "import date settings are invalid")
	case errors.Is(err, domain.ErrConflict):
		writeError(c, http.StatusConflict, "batch_not_editable", "this batch has already been committed or is no longer editable")
	case errors.Is(err, domain.ErrNotFound):
		writeError(c, http.StatusNotFound, "not_found", "import batch not found")
	case errors.Is(err, domain.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "a required dependency is unavailable")
	case errors.As(err, &retry):
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "the guild database is busy; retry the request")
	default:
		respondError(c, h.logger, err)
	}
}

func batchResponseOf(batch domain.ImportBatch) batchResponse {
	return batchResponse{
		ID: batch.ImportBatchID, FileName: batch.FileName, SHA256: batch.FileSha256,
		Status: batch.Status, SourceRowCount: batch.SourceRowCount,
		AcceptedRowCount: batch.AcceptedRowCount, FailureReason: batch.FailureReason,
		ImportedBy: batch.ImportedBy, ImportedByUserID: batch.ImportedByUserID,
		ImportedAt: batch.ImportedAt.UTC(),
	}
}

func summaryResponseOf(summary domain.ImportSummary) summaryResponse {
	return summaryResponse{
		BatchID: summary.BatchID, Status: summary.Status, SelectedRows: summary.SelectedRows,
		StagedRows: summary.StagedRows, SelectedLocations: summary.SelectedLocations,
		TotalLocations: summary.TotalLocations, ItemRows: summary.ItemRows,
		ItemUnits: summary.ItemUnits, SpellRows: summary.SpellRows,
		SpellQuantity: summary.SpellQuantity, DistinctSpells: summary.DistinctSpells,
		NewSpells: summary.NewSpells, MergedSpells: summary.MergedSpells,
		InventoryRecordsToCreate: summary.InventoryRecordsToCreate,
		WarningRows: summary.WarningRows, TotalRecordsToImport: summary.TotalRecordsToImport,
	}
}