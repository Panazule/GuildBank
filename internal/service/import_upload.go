package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// Upload reads, parses and stages one file. The batch is created before parsing
// so a parse failure remains durable and reviewable.
func (s *ImportService) Upload(ctx context.Context, actor domain.Actor, fileName string, body io.Reader, correlationID, sourceIP string) (domain.UploadResult, error) {
	if err := s.requireReady(); err != nil {
		return domain.UploadResult{}, err
	}
	if err := validateActor(actor); err != nil {
		return domain.UploadResult{}, err
	}
	name, err := sanitizeImportFileName(fileName)
	if err != nil {
		return domain.UploadResult{}, err
	}
	if body == nil {
		return domain.UploadResult{}, domain.NewValidationError("file", "a file is required")
	}
	data, err := io.ReadAll(io.LimitReader(body, ImportMaxBytes+1))
	if err != nil {
		return domain.UploadResult{}, fmt.Errorf("import: read upload: %w", err)
	}
	if len(data) > ImportMaxBytes {
		return domain.UploadResult{}, ErrFileTooLarge
	}
	shaBytes := sha256.Sum256(data)
	sha := hex.EncodeToString(shaBytes[:])
	now := s.now().UTC()
	batchID, err := s.store.CreateBatch(ctx, domain.ImportBatch{
		FileName: name, ImportedBy: actor.UserName, ImportedByUserID: &actor.UserID,
		ImportedAt: now, Status: domain.ImportStatusProcessing,
	})
	if err != nil {
		return domain.UploadResult{}, err
	}
	parsed, parseErr := ParseImport(data)
	if parseErr != nil {
		reason := parseErr.Error()
		var sourceRows *int
		if pe, ok := parseErr.(*ParseError); ok && pe.Line > 0 {
			value := pe.Line
			sourceRows = &value
		}
		_ = s.store.FailBatch(ctx, batchID, &reason, sourceRows, &sha)
		return domain.UploadResult{Batch: domain.ImportBatch{ImportBatchID: batchID, Status: domain.ImportStatusFailed}},
			&domain.ImportFailure{BatchID: batchID, Err: parseErr}
	}

	type classification struct {
		kind domain.ItemType
		line int
	}
	byEQUID := make(map[uint64]classification, len(parsed.Rows))
	for _, row := range parsed.Rows {
		kind := ClassifyItem(row.Name)
		if previous, ok := byEQUID[row.EQUID]; ok && previous.kind != kind {
			conflict := &ParseError{
				Code: "classification_conflict", Line: row.SourceLine,
				Message: fmt.Sprintf("EQUID %d is classified as both %s (line %d) and %s (line %d)",
					row.EQUID, previous.kind, previous.line, kind, row.SourceLine),
			}
			reason := conflict.Error()
			sourceRows := parsed.SourceLines
			_ = s.store.FailBatch(ctx, batchID, &reason, &sourceRows, &sha)
			return domain.UploadResult{Batch: domain.ImportBatch{ImportBatchID: batchID, Status: domain.ImportStatusFailed}},
				&domain.ImportFailure{BatchID: batchID, Err: conflict}
		}
		byEQUID[row.EQUID] = classification{kind: kind, line: row.SourceLine}
	}

	references := make([]domain.ItemReference, 0, len(byEQUID))
	seenEQUID := make(map[uint64]bool, len(byEQUID))
	for _, row := range parsed.Rows {
		if seenEQUID[row.EQUID] {
			continue
		}
		seenEQUID[row.EQUID] = true
		references = append(references, domain.ItemReference{EQUID: row.EQUID, Name: row.Name})
	}
	metadata, err := s.metadata.ResolveBatch(ctx, references)
	if err != nil {
		reason := "metadata_unavailable — Items metadata could not be read"
		sourceRows := parsed.SourceLines
		_ = s.store.FailBatch(ctx, batchID, &reason, &sourceRows, &sha)
		return domain.UploadResult{Batch: domain.ImportBatch{ImportBatchID: batchID, Status: domain.ImportStatusFailed}},
			&domain.ImportFailure{BatchID: batchID, Err: &ParseError{Code: "metadata_unavailable", Message: "Items metadata could not be read"}}
	}
	metadataByEQUID := make(map[uint64]ItemMetadata, len(metadata))
	for _, item := range metadata {
		metadataByEQUID[item.EQUID] = item
	}

	rows := make([]domain.ImportRow, 0, len(parsed.Rows))
	warningCounts := make(map[string]int)
	for _, parsedRow := range parsed.Rows {
		location := NormalizeLocation(parsedRow.Location)
		item := metadataByEQUID[parsedRow.EQUID]
		warnings := make([]domain.ImportWarning, 0, len(item.Warnings)+len(location.Warnings))
		for _, warning := range item.Warnings {
			warnings = append(warnings, domain.ImportWarning{
				Code: warning.Code, Field: warning.Field, EQUID: warning.EQUID,
				Mask: warning.Mask, HighBits: warning.HighBits, Detail: warning.Detail,
			})
		}
		warnings = append(warnings, location.Warnings...)
		for _, warning := range warnings {
			warningCounts[warning.Code]++
		}
		rows = append(rows, domain.ImportRow{
			ImportBatchID: batchID, SourceLine: intPtr(parsedRow.SourceLine),
			ItemType: byEQUID[parsedRow.EQUID].kind, EQUID: parsedRow.EQUID,
			Name: parsedRow.Name, Quantity: parsedRow.Quantity, LocationRaw: parsedRow.Location,
			LocationGroup: location.Group, LocationSort: TruncateRunes(location.Sort, 80),
			NameSort:   TruncateRunes(NaturalSortKey(parsedRow.Name), 160),
			SelectedFg: true, Warnings: warnings,
		})
	}

	auditNew, _ := json.Marshal(map[string]any{
		"fileName": name, "sourceRows": parsed.SourceLines,
		"acceptedRows": len(rows), "sha256": sha,
	})
	audit := domain.AuditEntry{
		EventDate: now, UserID: actor.UserID, UserName: actor.UserName,
		EventType: "IMPORT_CREATED", TableName: "ImportBatch",
		RecordID: fmt.Sprintf("%d", batchID), NewValues: auditNew,
		CorrelationID: correlationID, SourceIP: sourceIP,
	}
	if err := s.store.StageBatch(ctx, batchID, rows, parsed.SourceLines, sha, audit); err != nil {
		reason := "staging_failed — imported rows could not be staged"
		sourceRows := parsed.SourceLines
		_ = s.store.FailBatch(ctx, batchID, &reason, &sourceRows, &sha)
		return domain.UploadResult{Batch: domain.ImportBatch{ImportBatchID: batchID, Status: domain.ImportStatusFailed}},
			&domain.ImportFailure{BatchID: batchID, Err: err}
	}
	batch, err := s.store.GetBatch(ctx, batchID)
	if err != nil {
		return domain.UploadResult{}, err
	}
	duplicate, duplicateErr := s.store.FindDuplicateBatch(ctx, sha, batchID)
	if duplicateErr != nil {
		s.logger.Warn("duplicate import check failed", "batch_id", batchID, "error", duplicateErr)
	}
	if duplicate != nil {
		warningCounts["duplicate_file"] = 1
	}
	s.logger.Info("import batch uploaded", "batch_id", batchID,
		"user_id", actor.UserID, "bytes", len(data), "source_rows", parsed.SourceLines,
		"accepted_rows", len(rows), "sha_prefix", sha[:12])
	return domain.UploadResult{
		Batch: batch, StructuralRows: parsed.StructuralRows,
		DiscardedRows: parsed.DiscardedRows, BlankRows: parsed.BlankRows,
		DuplicateOf: duplicate, WarningCounts: warningCounts,
	}, nil
}

func validateActor(actor domain.Actor) error {
	if actor.UserID <= 0 || strings.TrimSpace(actor.UserName) == "" || !actor.IsAdmin {
		return domain.ErrForbidden
	}
	return nil
}

func sanitizeImportFileName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(strings.TrimSpace(name))
	name = strings.ToValidUTF8(name, "")
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) || r == 0 {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", domain.NewValidationError("file", "a file name is required")
	}
	return TruncateRunes(name, 255), nil
}

func intPtr(value int) *int { return &value }

func importNow(value time.Time) time.Time { return value.UTC() }
