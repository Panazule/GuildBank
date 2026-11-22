package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

var (
	// ErrFileTooLarge maps to 413.
	ErrFileTooLarge = errors.New("file too large")
	// ErrSelectionEmpty maps to commit-time 422 selection_empty.
	ErrSelectionEmpty = errors.New("selection is empty")
	// ErrSettingsInvalid maps to commit-time 422 settings_invalid.
	ErrSettingsInvalid = errors.New("import date settings are invalid")
)

// ImportStore is the guild-store surface used by ImportService. It is
// implemented by the MariaDB repository and faked in tests.
type ImportStore interface {
	CreateBatch(ctx context.Context, b domain.ImportBatch) (int64, error)
	FailBatch(ctx context.Context, batchID int64, reason *string, sourceRows *int, sha *string) error
	StageBatch(ctx context.Context, batchID int64, rows []domain.ImportRow, sourceRows int, sha string, audit domain.AuditEntry) error
	GetBatch(ctx context.Context, batchID int64) (domain.ImportBatch, error)
	ListBatches(ctx context.Context, limit int, beforeID int64) ([]domain.ImportBatch, error)
	FindDuplicateBatch(ctx context.Context, sha string, excludeID int64) (*int64, error)
	ListLocations(ctx context.Context, batchID int64) ([]domain.ImportLocation, error)
	ListRows(ctx context.Context, batchID int64, filter domain.ImportRowFilter) (domain.ImportRowPage, error)
	RowIDs(ctx context.Context, batchID int64, locationGroup string) ([]int64, error)
	SelectedIDs(ctx context.Context, batchID int64) ([]int64, error)
	AllRows(ctx context.Context, batchID int64) ([]domain.ImportRow, error)
	ReplaceSelection(ctx context.Context, batchID int64, ids []int64) error
	SelectedTotals(ctx context.Context, batchID int64) (domain.ImportTotals, error)
	SpellPreview(ctx context.Context, batchID int64) ([]domain.SpellPreview, error)
	BeginCommit(ctx context.Context, batchID int64) (ImportCommitTx, error)
	RecoverInterrupted(ctx context.Context) (int, error)
	Ready() bool
}

// ImportCommitTx is the store-level transaction facade.
type ImportCommitTx interface {
	Batch() domain.ImportBatch
	SelectedRows() ([]domain.ImportRow, error)
	ActiveSpells(equids []uint64) ([]domain.GuildBankItem, error)
	InsertItems(rows []domain.GuildBankItem) ([]int64, error)
	InsertSpells(rows []domain.GuildBankItem) ([]int64, error)
	AddSpellQuantity(id int64, qty uint32, at time.Time, actorName string, actorID int64) error
	ReplaceRowWarnings(warnings map[int64][]domain.ImportWarning) error
	WriteAudit(entries []domain.AuditEntry) error
	MarkCompleted() error
	Commit() error
	Rollback() error
}

// ImportService owns parsing, staging, selection and atomic commit.
type ImportService struct {
	store    ImportStore
	metadata *ItemMetadataService
	settings *SettingsService
	now      func() time.Time
	logger   *slog.Logger
}

// NewImportService wires the import service. A nil store produces a degraded
// service whose Ready() is false.
func NewImportService(store ImportStore, metadata *ItemMetadataService, settings *SettingsService, now func() time.Time, logger *slog.Logger) *ImportService {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ImportService{store: store, metadata: metadata, settings: settings, now: now, logger: logger}
}

// Ready reports whether the guild store and metadata resolver are available.
func (s *ImportService) Ready() bool {
	return s != nil && s.store != nil && s.store.Ready() && s.metadata != nil
}

func (s *ImportService) requireReady() error {
	if !s.Ready() {
		return domain.ErrUnavailable
	}
	return nil
}

// ListBatches returns recent batches.
func (s *ImportService) ListBatches(ctx context.Context, limit int, beforeID int64) ([]domain.ImportBatch, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		return nil, domain.NewValidationError("limit", "limit must be between 1 and 200")
	}
	if beforeID < 0 {
		return nil, domain.NewValidationError("beforeId", "beforeId must be positive")
	}
	return s.store.ListBatches(ctx, limit, beforeID)
}

// GetBatch reads one batch.
func (s *ImportService) GetBatch(ctx context.Context, batchID int64) (domain.ImportBatch, error) {
	if err := s.requireReady(); err != nil {
		return domain.ImportBatch{}, err
	}
	return s.store.GetBatch(ctx, batchID)
}

// Locations returns selector entries derived only from LocationGroup.
func (s *ImportService) Locations(ctx context.Context, batchID int64) ([]domain.ImportLocation, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	return s.store.ListLocations(ctx, batchID)
}

// Rows validates and pages staged rows.
func (s *ImportService) Rows(ctx context.Context, batchID int64, filter domain.ImportRowFilter) (domain.ImportRowPage, error) {
	if err := s.requireReady(); err != nil {
		return domain.ImportRowPage{}, err
	}
	if filter.Page < 1 {
		return domain.ImportRowPage{}, domain.NewValidationError("page", "page must be positive")
	}
	if filter.PageSize < 1 || filter.PageSize > 200 {
		return domain.ImportRowPage{}, domain.NewValidationError("pageSize", "pageSize must be between 1 and 200")
	}
	switch filter.Selected {
	case "", "all", "selected", "unselected":
	default:
		return domain.ImportRowPage{}, domain.NewValidationError("selected", "selected must be all, selected or unselected")
	}
	if len([]rune(filter.Search)) > 128 {
		return domain.ImportRowPage{}, domain.NewValidationError("q", "q must be at most 128 characters")
	}
	return s.store.ListRows(ctx, batchID, filter)
}

// RowIDs returns every staged ID, optionally for one LocationGroup.
func (s *ImportService) RowIDs(ctx context.Context, batchID int64, locationGroup string) ([]int64, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	return s.store.RowIDs(ctx, batchID, locationGroup)
}

// Selection returns the persisted selection set.
func (s *ImportService) Selection(ctx context.Context, batchID int64) ([]int64, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	return s.store.SelectedIDs(ctx, batchID)
}

// SetSelection replaces the persisted set and returns the canonical summary.
func (s *ImportService) SetSelection(ctx context.Context, batchID int64, ids []int64) (domain.ImportSummary, error) {
	if err := s.requireReady(); err != nil {
		return domain.ImportSummary{}, err
	}
	if len(ids) > 50_000 {
		return domain.ImportSummary{}, domain.NewValidationError("selectedImportIds", "at most 50,000 IDs may be selected")
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return domain.ImportSummary{}, domain.NewValidationError("selectedImportIds", "IDs must be positive")
		}
		if seen[id] {
			return domain.ImportSummary{}, domain.NewValidationError("selectedImportIds", "duplicate IDs are not allowed")
		}
		seen[id] = true
	}
	if err := s.store.ReplaceSelection(ctx, batchID, ids); err != nil {
		return domain.ImportSummary{}, err
	}
	return s.Summary(ctx, batchID)
}

// Summary composes the unit-aware selected totals, locations and preview.
func (s *ImportService) Summary(ctx context.Context, batchID int64) (domain.ImportSummary, error) {
	if err := s.requireReady(); err != nil {
		return domain.ImportSummary{}, err
	}
	batch, err := s.store.GetBatch(ctx, batchID)
	if err != nil {
		return domain.ImportSummary{}, err
	}
	totals, err := s.store.SelectedTotals(ctx, batchID)
	if err != nil {
		return domain.ImportSummary{}, err
	}
	locations, err := s.store.ListLocations(ctx, batchID)
	if err != nil {
		return domain.ImportSummary{}, err
	}
	previews, err := s.store.SpellPreview(ctx, batchID)
	if err != nil {
		return domain.ImportSummary{}, err
	}
	summary := domain.ImportSummary{
		BatchID: batchID, Status: batch.Status, SelectedRows: totals.SelectedRows,
		StagedRows: intValue(batch.AcceptedRowCount), ItemRows: totals.ItemRows,
		ItemUnits: totals.ItemUnits, SpellRows: totals.SpellRows,
		SpellQuantity: totals.SpellQuantity, DistinctSpells: totals.DistinctSpells,
		WarningRows: totals.WarningRows, TotalLocations: len(locations),
	}
	for _, location := range locations {
		if location.SelectedCount > 0 {
			summary.SelectedLocations++
		}
	}
	for _, preview := range previews {
		if preview.MergeTargetID == nil {
			summary.NewSpells++
		} else {
			summary.MergedSpells++
		}
	}
	summary.InventoryRecordsToCreate = summary.ItemUnits + uint64(summary.NewSpells)
	summary.TotalRecordsToImport = summary.ItemUnits + uint64(summary.DistinctSpells)
	return summary, nil
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
