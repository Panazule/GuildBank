package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

type fakeAdmin struct {
	ready        bool
	nextID       int64
	items        map[int64]domain.AdminInventoryItem
	audits       []domain.AuditEntry
	movements    []domain.InventoryMovement
	stackability map[uint64]domain.Stackability
	groups       []string
	failAudit    bool
	committed    int
	rolled       int
	// backupItems/backupAudits/backupMovements make the fake honour
	// AdminWriteTx rollback so an all-or-nothing service path can be asserted
	// without a live database.
	backupItems     map[int64]domain.AdminInventoryItem
	backupAudits    []domain.AuditEntry
	backupMovements []domain.InventoryMovement
	backupRequests  []domain.InventoryRequest
	inTx            bool
	// requests is the request workflow ledger. nextRequestID mirrors the
	// auto-increment primary key so an insert can hand back a stable ID.
	requests      []domain.InventoryRequest
	nextRequestID int64
}

func newFakeAdmin() *fakeAdmin {
	return &fakeAdmin{ready: true, nextID: 1, nextRequestID: 1, items: map[int64]domain.AdminInventoryItem{}}
}

func (f *fakeAdmin) Ready() bool { return f.ready }

func (f *fakeAdmin) ListAdminInventory(ctx context.Context, filter domain.AdminInventoryFilter) (domain.AdminInventoryPage, error) {
	items := make([]domain.AdminInventoryItem, 0, len(f.items))
	for _, item := range f.items {
		items = append(items, item)
	}
	return domain.AdminInventoryPage{
		Records: items, Total: len(items), Page: filter.Page, PageSize: filter.PageSize, Filter: filter,
	}, nil
}

func (f *fakeAdmin) GetAdminInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error) {
	item, ok := f.items[id]
	if !ok {
		return domain.AdminInventoryItem{}, domain.ErrNotFound
	}
	return item, nil
}

func (f *fakeAdmin) ListLocationGroups(ctx context.Context) ([]string, error) {
	return f.groups, nil
}

func (f *fakeAdmin) BeginAdminWrite(ctx context.Context) (AdminWriteTx, error) {
	f.backupItems = make(map[int64]domain.AdminInventoryItem, len(f.items))
	for id, item := range f.items {
		f.backupItems[id] = item
	}
	f.backupAudits = append([]domain.AuditEntry(nil), f.audits...)
	f.backupMovements = append([]domain.InventoryMovement(nil), f.movements...)
	f.backupRequests = append([]domain.InventoryRequest(nil), f.requests...)
	f.inTx = true
	return f, nil
}

func (f *fakeAdmin) SumActiveQuantity(ctx context.Context, filter domain.AdminInventoryFilter) (uint64, error) {
	var total uint64
	for _, item := range f.items {
		if item.DeletedFg {
			continue
		}
		total += uint64(item.Quantity)
	}
	return total, nil
}

func (f *fakeAdmin) ListMovements(ctx context.Context, filter domain.MovementFilter) (domain.MovementPage, error) {
	movements := make([]domain.InventoryMovement, 0, len(f.movements))
	for _, movement := range f.movements {
		if filter.GuildBankItemID > 0 && movement.GuildBankItemID != filter.GuildBankItemID {
			continue
		}
		if filter.EQUID > 0 && movement.EQUID != filter.EQUID {
			continue
		}
		if filter.MovementType != "" && movement.MovementType != filter.MovementType {
			continue
		}
		if filter.Recipient != "" && movement.Recipient != filter.Recipient {
			continue
		}
		if filter.CorrelationID != "" && movement.CorrelationID != filter.CorrelationID {
			continue
		}
		movements = append(movements, movement)
	}
	return domain.MovementPage{
		Movements: movements, Total: len(movements), Page: filter.Page, PageSize: filter.PageSize,
	}, nil
}

func (f *fakeAdmin) GetMovement(ctx context.Context, movementID int64) (domain.InventoryMovement, error) {
	for _, movement := range f.movements {
		if movement.MovementID == movementID {
			return movement, nil
		}
	}
	return domain.InventoryMovement{}, domain.ErrNotFound
}

// movementOutstanding mirrors the repository arithmetic for the fake ledger.
func (f *fakeAdmin) movementOutstanding(movementID int64) uint32 {
	var issue *domain.InventoryMovement
	for i := range f.movements {
		if f.movements[i].MovementID == movementID {
			issue = &f.movements[i]
			break
		}
	}
	if issue == nil || issue.MovementType != domain.MovementIssue {
		return 0
	}
	outstanding := int64(issue.Quantity)
	for _, movement := range f.movements {
		if movement.MovementType == domain.MovementReturn && movement.RelatedMovementID != nil &&
			*movement.RelatedMovementID == movementID {
			outstanding -= int64(movement.Quantity)
		}
	}
	if outstanding <= 0 {
		return 0
	}
	return uint32(outstanding)
}

func (f *fakeAdmin) GetMovementOutstanding(ctx context.Context, movementID int64) (uint32, error) {
	for _, movement := range f.movements {
		if movement.MovementID == movementID {
			return f.movementOutstanding(movementID), nil
		}
	}
	return 0, domain.ErrNotFound
}

func (f *fakeAdmin) LockMovementOutstanding(ctx context.Context, movementID int64) (domain.InventoryMovement, uint32, error) {
	for _, movement := range f.movements {
		if movement.MovementID == movementID {
			return movement, f.movementOutstanding(movementID), nil
		}
	}
	return domain.InventoryMovement{}, 0, domain.ErrNotFound
}

func (f *fakeAdmin) EarliestOutstandingIssue(ctx context.Context, lotID int64) (int64, error) {
	best := int64(0)
	for _, movement := range f.movements {
		if movement.MovementType != domain.MovementIssue || movement.GuildBankItemID != lotID {
			continue
		}
		if f.movementOutstanding(movement.MovementID) == 0 {
			continue
		}
		if best == 0 || movement.MovementID < best {
			best = movement.MovementID
		}
	}
	return best, nil
}

func (f *fakeAdmin) LookupStackability(ctx context.Context, equids []uint64) (map[uint64]domain.Stackability, error) {
	result := make(map[uint64]domain.Stackability, len(equids))
	for _, equid := range equids {
		value, ok := f.stackability[equid]
		if !ok {
			value = domain.StackabilityUnknown
		}
		result[equid] = value
	}
	return result, nil
}

func (f *fakeAdmin) ListAudit(ctx context.Context, filter domain.AuditFilter) (domain.AuditPage, error) {
	return domain.AuditPage{Entries: f.audits, Total: len(f.audits), Page: 1, PageSize: 50}, nil
}

func (f *fakeAdmin) GetAudit(ctx context.Context, id int64) (domain.AuditEntry, error) {
	for _, entry := range f.audits {
		if entry.AuditID == id {
			return entry, nil
		}
	}
	return domain.AuditEntry{}, domain.ErrNotFound
}

// sortedRequests returns the fake's request rows newest-first, matching the
// repository's fixed order.
func (f *fakeAdmin) sortedRequests() []domain.InventoryRequest {
	requests := append([]domain.InventoryRequest(nil), f.requests...)
	sort.SliceStable(requests, func(i, j int) bool {
		if !requests[i].CreatedAt.Equal(requests[j].CreatedAt) {
			return requests[i].CreatedAt.After(requests[j].CreatedAt)
		}
		return requests[i].RequestID > requests[j].RequestID
	})
	return requests
}

func (f *fakeAdmin) projectRequest(request domain.InventoryRequest) domain.InventoryRequest {
	item, ok := f.items[request.GuildBankItemID]
	if !ok {
		request.CurrentOnHand = 0
		request.LotActive = false
		return request
	}
	request.CurrentOnHand = item.Quantity
	request.LotActive = !item.DeletedFg
	return request
}

func (f *fakeAdmin) GetRequest(ctx context.Context, requestID int64) (domain.InventoryRequest, error) {
	for _, request := range f.requests {
		if request.RequestID == requestID {
			return f.projectRequest(request), nil
		}
	}
	return domain.InventoryRequest{}, domain.ErrNotFound
}

func (f *fakeAdmin) RequestByIdempotencyKey(ctx context.Context, key string) (domain.InventoryRequest, error) {
	for _, request := range f.requests {
		if key != "" && request.IdempotencyKey == key {
			return f.projectRequest(request), nil
		}
	}
	return domain.InventoryRequest{}, domain.ErrNotFound
}

func (f *fakeAdmin) ListRequests(ctx context.Context, filter domain.RequestFilter) (domain.RequestPage, error) {
	matches := make([]domain.InventoryRequest, 0, len(f.requests))
	for _, request := range f.sortedRequests() {
		if filter.Status != "" && request.Status != filter.Status {
			continue
		}
		if filter.RequesterKind != "" && request.RequesterKind != filter.RequesterKind {
			continue
		}
		if filter.RequesterUserID > 0 &&
			(request.RequesterUserID == nil || *request.RequesterUserID != filter.RequesterUserID) {
			continue
		}
		if filter.GuildBankItemID > 0 && request.GuildBankItemID != filter.GuildBankItemID {
			continue
		}
		if filter.CharacterName != "" && request.CharacterName != filter.CharacterName {
			continue
		}
		if filter.ItemName != "" && request.ItemName != filter.ItemName {
			continue
		}
		if !filter.From.IsZero() && request.CreatedAt.Before(filter.From) {
			continue
		}
		if !filter.To.IsZero() && request.CreatedAt.After(filter.To) {
			continue
		}
		matches = append(matches, f.projectRequest(request))
	}
	page := domain.RequestPage{
		Requests: matches, Total: len(matches), Page: filter.Page,
		PageSize: filter.PageSize, Filter: filter,
	}
	if page.PageSize > 0 {
		page.TotalPages = (len(matches) + page.PageSize - 1) / page.PageSize
	}
	return page, nil
}

func (f *fakeAdmin) CountPendingRequests(ctx context.Context, requesterUserID int64) (int, error) {
	if requesterUserID <= 0 {
		return 0, &domain.ValidationError{Fields: map[string]string{
			"requesterUserId": "requesterUserId must be a positive integer",
		}}
	}
	count := 0
	for _, request := range f.requests {
		if request.Status == domain.RequestPending && request.RequesterUserID != nil &&
			*request.RequesterUserID == requesterUserID {
			count++
		}
	}
	return count, nil
}

func (f *fakeAdmin) PendingRequestForCharacter(ctx context.Context, guildBankItemID int64, characterName string) (int64, error) {
	characterName = domain.NormalizeCharacterName(characterName)
	if guildBankItemID <= 0 {
		return 0, domain.ErrNotFound
	}
	for _, request := range f.requests {
		if request.Status == domain.RequestPending && request.GuildBankItemID == guildBankItemID &&
			request.CharacterName == characterName {
			return request.RequestID, nil
		}
	}
	return 0, nil
}

func (f *fakeAdmin) PendingCharactersForLot(ctx context.Context, guildBankItemID, requesterUserID int64) ([]string, error) {
	names := make([]string, 0, 2)
	for _, request := range f.requests {
		if request.Status != domain.RequestPending || request.GuildBankItemID != guildBankItemID {
			continue
		}
		if request.RequesterUserID == nil || *request.RequesterUserID != requesterUserID {
			continue
		}
		names = append(names, request.CharacterName)
	}
	return names, nil
}

func (f *fakeAdmin) PendingRequestsForLot(ctx context.Context, guildBankItemID int64, limit int) ([]domain.InventoryRequest, error) {
	requests := make([]domain.InventoryRequest, 0, 2)
	for _, request := range f.requests {
		if request.Status != domain.RequestPending || request.GuildBankItemID != guildBankItemID {
			continue
		}
		requests = append(requests, f.projectRequest(request))
	}
	sort.SliceStable(requests, func(i, j int) bool {
		if !requests[i].CreatedAt.Equal(requests[j].CreatedAt) {
			return requests[i].CreatedAt.Before(requests[j].CreatedAt)
		}
		return requests[i].RequestID < requests[j].RequestID
	})
	if limit > 0 && len(requests) > limit {
		requests = requests[:limit]
	}
	return requests, nil
}

func (f *fakeAdmin) LockRequest(ctx context.Context, requestID int64) (domain.InventoryRequest, error) {
	for _, request := range f.requests {
		if request.RequestID == requestID {
			return request, nil
		}
	}
	return domain.InventoryRequest{}, domain.ErrNotFound
}

func (f *fakeAdmin) LockInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error) {
	item, ok := f.items[id]
	if !ok {
		return domain.AdminInventoryItem{}, domain.ErrNotFound
	}
	return item, nil
}

func (f *fakeAdmin) FindActiveSpells(ctx context.Context, equid uint64) ([]domain.AdminInventoryItem, error) {
	matches := make([]domain.AdminInventoryItem, 0, 1)
	ids := make([]int64, 0, len(f.items))
	for id := range f.items {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		item := f.items[id]
		if item.DeletedFg || item.ItemType != domain.ItemTypeSpell || item.EQUID != equid {
			continue
		}
		matches = append(matches, item)
	}
	return matches, nil
}

func (f *fakeAdmin) InsertInventory(ctx context.Context, rows []domain.GuildBankItem) ([]int64, error) {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		for {
			if _, taken := f.items[f.nextID]; !taken {
				break
			}
			f.nextID++
		}
		id := f.nextID
		f.nextID++
		f.items[id] = adminItemFromRow(row, id)
		ids = append(ids, id)
	}
	return ids, nil
}

func (f *fakeAdmin) AddSpellQuantity(ctx context.Context, id int64, qty uint32, at time.Time, actor domain.Actor) error {
	item := f.items[id]
	item.Quantity += qty
	item.ModAt = at
	item.ModBy = actor.UserName
	f.items[id] = item
	return nil
}

func (f *fakeAdmin) UpdateInventory(ctx context.Context, in domain.AdminUpdateRow) error {
	item := f.items[in.ID]
	if in.Quantity != nil {
		item.Quantity = *in.Quantity
	}
	item.SourceLocation = in.SourceLocation
	item.LocationGroup = in.LocationGroup
	item.LocationSort = in.LocationSort
	item.GivenTo = in.GivenTo
	if in.DateOpen != nil {
		item.DateOpen = *in.DateOpen
	}
	if in.DateBazaar != nil {
		item.DateBazaar = *in.DateBazaar
	}
	item.ModAt = in.ModAt
	item.ModBy = in.Actor.UserName
	f.items[in.ID] = item
	return nil
}

func (f *fakeAdmin) SetDeleted(ctx context.Context, id int64, deleted bool, at time.Time, actor domain.Actor) error {
	item := f.items[id]
	item.DeletedFg = deleted
	item.ModAt = at
	item.ModBy = actor.UserName
	f.items[id] = item
	return nil
}

func (f *fakeAdmin) InsertMovement(ctx context.Context, movement domain.InventoryMovement) (int64, error) {
	if err := movement.Validate(); err != nil {
		return 0, err
	}
	if movement.IdempotencyKey != "" {
		for _, existing := range f.movements {
			if existing.IdempotencyKey == movement.IdempotencyKey {
				return 0, domain.ErrConflict
			}
		}
	}
	movement.MovementID = int64(len(f.movements) + 1)
	f.movements = append(f.movements, movement)
	return movement.MovementID, nil
}

// InsertRequest mirrors the repository's duplicate rules: one Pending request
// per character and lot, and one row per idempotency key.
func (f *fakeAdmin) InsertRequest(ctx context.Context, in domain.CreateRequestInput) (int64, error) {
	if err := in.Validate(); err != nil {
		return 0, err
	}
	characterName := domain.NormalizeCharacterName(in.CharacterName)
	for _, existing := range f.requests {
		if in.IdempotencyKey != "" && existing.IdempotencyKey == in.IdempotencyKey {
			return 0, domain.ErrConflict
		}
		if existing.Status == domain.RequestPending && existing.GuildBankItemID == in.GuildBankItemID &&
			existing.CharacterName == characterName {
			return 0, domain.ErrConflict
		}
	}
	request := domain.InventoryRequest{
		RequestID: f.nextRequestID, GuildBankItemID: in.GuildBankItemID, EQUID: in.EQUID,
		ItemName: in.ItemName, RequestedQuantity: in.Quantity, OnHandAtCreate: in.OnHandAtCreate,
		CharacterName: characterName, RequesterKind: in.Kind, ContactDiscord: in.ContactDiscord,
		Note: in.Note, Status: domain.RequestPending, CorrelationID: in.CorrelationID,
		IdempotencyKey: in.IdempotencyKey, CreatedAt: in.CreatedAt,
		CreatedByName: domain.RequestPublicActorName,
	}
	if in.Kind == domain.RequesterMember {
		userID := in.Actor.UserID
		request.RequesterUserID = &userID
		request.RequesterUserName = in.Actor.UserName
		request.CreatedByUserID = &userID
		request.CreatedByName = in.Actor.UserName
	}
	f.nextRequestID++
	f.requests = append(f.requests, request)
	return request.RequestID, nil
}

// CompleteRequest applies the guarded Pending -> Completed transition and
// refuses a quantity the schema would refuse (1..RequestedQuantity).
func (f *fakeAdmin) CompleteRequest(ctx context.Context, p domain.CompleteRequestParams) error {
	for i := range f.requests {
		request := &f.requests[i]
		if request.RequestID != p.RequestID {
			continue
		}
		if request.Status != domain.RequestPending {
			return domain.ErrConflict
		}
		if p.IssuedQuantity == 0 || p.IssuedQuantity > request.RequestedQuantity || p.MovementID <= 0 {
			return &domain.ValidationError{Fields: map[string]string{
				"issuedQuantity": "issuedQuantity must be between 1 and the requested quantity",
			}}
		}
		issued := p.IssuedQuantity
		movementID := p.MovementID
		request.Status = domain.RequestCompleted
		request.IssuedQuantity = &issued
		request.CompletedMovementID = &movementID
		request.DecisionNote = p.DecisionNote
		request.DecidedAt = &p.DecidedAt
		request.DecidedByUserID = &p.Actor.UserID
		request.DecidedByName = p.Actor.UserName
		return nil
	}
	return domain.ErrNotFound
}

// DecideRequest applies the guarded Pending -> Denied|Cancelled transition.
func (f *fakeAdmin) DecideRequest(ctx context.Context, p domain.DecideRequestParams) error {
	if p.Status != domain.RequestDenied && p.Status != domain.RequestCancelled {
		return &domain.ValidationError{Fields: map[string]string{
			"status": "status must be Denied or Cancelled",
		}}
	}
	for i := range f.requests {
		request := &f.requests[i]
		if request.RequestID != p.RequestID {
			continue
		}
		if request.Status != domain.RequestPending {
			return domain.ErrConflict
		}
		request.Status = p.Status
		request.DecisionNote = p.DecisionNote
		request.DecidedAt = &p.DecidedAt
		request.DecidedByUserID = &p.Actor.UserID
		request.DecidedByName = p.Actor.UserName
		return nil
	}
	return domain.ErrNotFound
}

func (f *fakeAdmin) InsertAudit(ctx context.Context, entry domain.AuditEntry) error {
	if f.failAudit {
		return errors.New("audit insert failed")
	}
	entry.AuditID = int64(len(f.audits) + 1)
	f.audits = append(f.audits, entry)
	return nil
}

func (f *fakeAdmin) Commit() error {
	f.committed++
	f.inTx = false
	f.backupItems = nil
	f.backupAudits = nil
	f.backupMovements = nil
	f.backupRequests = nil
	return nil
}

func (f *fakeAdmin) Rollback() error {
	f.rolled++
	if f.inTx {
		if f.backupItems != nil {
			f.items = f.backupItems
		}
		f.audits = f.backupAudits
		// inTx guarantees BeginAdminWrite took a snapshot, so restore the
		// movement slice even when the snapshot is nil (no prior movements);
		// guarding on nil here would leave the failed attempt's movement in place.
		f.movements = f.backupMovements
		f.requests = f.backupRequests
	}
	f.inTx = false
	f.backupItems = nil
	f.backupAudits = nil
	f.backupMovements = nil
	f.backupRequests = nil
	return nil
}

type fakeSettings map[string]string

func (f fakeSettings) Values(ctx context.Context) (map[string]string, error) {
	return map[string]string(f), nil
}

type fakeMetadata map[uint64]ItemMetadata

func (f fakeMetadata) Resolve(ctx context.Context, ref domain.ItemReference) (ItemMetadata, error) {
	meta, ok := f[ref.EQUID]
	if !ok {
		return ItemMetadata{}, domain.NewValidationError("EQUID", "missing metadata fixture")
	}
	return meta, nil
}

func testAdminService(store *fakeAdmin, metadata fakeMetadata, now time.Time) *AdminInventoryService {
	if metadata == nil {
		metadata = fakeMetadata{
			100: {EQUID: 100, Name: "Sword", ClassString: "WAR", RaceString: "ALL"},
			200: {EQUID: 200, Name: "Clarity", ClassString: "CLR", RaceString: "ALL"},
		}
	}
	return NewAdminInventoryService(store,
		fakeSettings{"GuildOpenDays": "14", "BazaarDays": "30"},
		metadata, nil, func() time.Time { return now }, nil)
}

func testActor() domain.Actor {
	return domain.Actor{UserID: 1, UserName: "admin", IsAdmin: true}
}
