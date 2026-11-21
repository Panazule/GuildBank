package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Panazule/guildbank/internal/domain"
)

const (
	// ItemWarningMetadataMissing reports an EQUID absent from Items at
	// resolution time. It is non-fatal: the inventory row is retained with the
	// imported identity and blank decoded strings (D-08, AM-01/AM-02).
	ItemWarningMetadataMissing = "metadata_missing"
)

// ItemMetadata is the service-layer result used to populate GuildBankItems.
// MetadataMissing maps directly to GuildBankItems.MetadataMissingFg.
type ItemMetadata struct {
	EQUID           uint64
	Name            string
	ClassString     string
	RaceString      string
	MetadataMissing bool
	Warnings        []ItemWarning
}

// ItemMetadataReader is the narrow read-only source contract required by the
// metadata service. It is implemented by the MariaDB item repository.
type ItemMetadataReader interface {
	GetItemsByEQUIDs(ctx context.Context, equids []uint64) (map[uint64]domain.Item, error)
}

// ItemMetadataService resolves imported item references against the read-only
// Items source and generates the decoded fields stored in GuildBankItems.
type ItemMetadataService struct {
	items   ItemMetadataReader
	classes *ClassMaskDecoder
	races   *RaceMaskDecoder
}

// NewItemMetadataService builds the metadata generation service.
func NewItemMetadataService(items ItemMetadataReader) *ItemMetadataService {
	return &ItemMetadataService{
		items:   items,
		classes: NewClassMaskDecoder(),
		races:   NewRaceMaskDecoder(),
	}
}

// Resolve generates metadata for one imported reference.
func (s *ItemMetadataService) Resolve(ctx context.Context, ref domain.ItemReference) (ItemMetadata, error) {
	results, err := s.ResolveBatch(ctx, []domain.ItemReference{ref})
	if err != nil {
		return ItemMetadata{}, err
	}
	return results[0], nil
}

// ResolveBatch generates metadata in input order. EQUIDs are validated before
// any source read, so a malformed identifier set produces no partial result.
// Identifiers are fetched in one batch; absent rows become MetadataMissing
// results rather than errors when the imported name fallback is present
// (D-08, AM-01/AM-02).
func (s *ItemMetadataService) ResolveBatch(ctx context.Context, refs []domain.ItemReference) ([]ItemMetadata, error) {
	if len(refs) == 0 {
		return []ItemMetadata{}, nil
	}
	for i, ref := range refs {
		if ref.EQUID == 0 {
			return nil, domain.NewValidationError("EQUID", fmt.Sprintf("reference %d must have a positive item identifier", i))
		}
	}

	ids := make([]uint64, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.EQUID)
	}
	items, err := s.items.GetItemsByEQUIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	results := make([]ItemMetadata, 0, len(refs))
	for _, ref := range refs {
		item, ok := items[ref.EQUID]
		if !ok {
			if strings.TrimSpace(ref.Name) == "" {
				return nil, domain.NewValidationError("Name", fmt.Sprintf("item %d is absent from Items and has no imported name fallback", ref.EQUID))
			}
			results = append(results, s.missing(ref))
			continue
		}
		results = append(results, s.generate(item))
	}
	return results, nil
}

func (s *ItemMetadataService) generate(item domain.Item) ItemMetadata {
	classes := s.classes.Decode(item.ClassList)
	races := s.races.Decode(item.RaceList)
	warnings := make([]ItemWarning, 0, len(classes.Warnings)+len(races.Warnings))
	warnings = append(warnings, classes.Warnings...)
	warnings = append(warnings, races.Warnings...)
	for i := range warnings {
		warnings[i].EQUID = item.EQUID
	}
	return ItemMetadata{
		EQUID:       item.EQUID,
		Name:        item.Name,
		ClassString: classes.Value,
		RaceString:  races.Value,
		Warnings:    warnings,
	}
}

func (s *ItemMetadataService) missing(ref domain.ItemReference) ItemMetadata {
	return ItemMetadata{
		EQUID:           ref.EQUID,
		Name:            ref.Name,
		MetadataMissing: true,
		Warnings: []ItemWarning{{
			Code:   ItemWarningMetadataMissing,
			Field:  "EQUID",
			EQUID:  ref.EQUID,
			Detail: fmt.Sprintf("item %d is absent from Items; imported name retained and class/race strings left blank", ref.EQUID),
		}},
	}
}
