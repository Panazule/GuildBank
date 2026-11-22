package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/repository/sqlite"
	"github.com/Panazule/guildbank/internal/service"
)

type fakeImportStore struct {
	batches map[int64]domain.ImportBatch
	rows    map[int64][]domain.ImportRow
	next    int64
	audits  []domain.AuditEntry
}

func newFakeImportStore() *fakeImportStore {
	return &fakeImportStore{batches: map[int64]domain.ImportBatch{}, rows: map[int64][]domain.ImportRow{}, next: 1}
}

func (f *fakeImportStore) CreateBatch(_ context.Context, batch domain.ImportBatch) (int64, error) {
	id := f.next
	f.next++
	batch.ImportBatchID = id
	batch.Status = domain.ImportStatusProcessing
	f.batches[id] = batch
	return id, nil
}
func (f *fakeImportStore) FailBatch(_ context.Context, id int64, reason *string, sourceRows *int, sha *string) error {
	batch := f.batches[id]
	batch.Status = domain.ImportStatusFailed
	batch.FailureReason = reason
	batch.SourceRowCount = sourceRows
	batch.FileSha256 = sha
	f.batches[id] = batch
	return nil
}
func (f *fakeImportStore) StageBatch(_ context.Context, id int64, rows []domain.ImportRow, sourceRows int, sha string, audit domain.AuditEntry) error {
	for i := range rows {
		rows[i].ImportID = int64(i + 1)
		rows[i].ImportBatchID = id
	}
	f.rows[id] = append([]domain.ImportRow(nil), rows...)
	batch := f.batches[id]
	batch.Status = domain.ImportStatusUploaded
	batch.SourceRowCount = &sourceRows
	accepted := len(rows)
	batch.AcceptedRowCount = &accepted
	batch.FileSha256 = &sha
	f.batches[id] = batch
	f.audits = append(f.audits, audit)
	return nil
}
func (f *fakeImportStore) GetBatch(_ context.Context, id int64) (domain.ImportBatch, error) {
	batch, ok := f.batches[id]
	if !ok {
		return domain.ImportBatch{}, domain.ErrNotFound
	}
	return batch, nil
}
func (f *fakeImportStore) ListBatches(_ context.Context, limit int, beforeID int64) ([]domain.ImportBatch, error) {
	ids := make([]int64, 0, len(f.batches))
	for id := range f.batches {
		if beforeID == 0 || id < beforeID {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]domain.ImportBatch, 0, len(ids))
	for _, id := range ids {
		out = append(out, f.batches[id])
	}
	return out, nil
}
func (f *fakeImportStore) FindDuplicateBatch(context.Context, string, int64) (*int64, error) {
	return nil, nil
}
func (f *fakeImportStore) ListLocations(_ context.Context, id int64) ([]domain.ImportLocation, error) {
	byGroup := map[string]*domain.ImportLocation{}
	for _, row := range f.rows[id] {
		location := byGroup[row.LocationGroup]
		if location == nil {
			location = &domain.ImportLocation{LocationGroup: row.LocationGroup, LocationSort: row.LocationSort}
			byGroup[row.LocationGroup] = location
		}
		location.RowCount++
		if row.SelectedFg {
			location.SelectedCount++
		}
	}
	out := make([]domain.ImportLocation, 0, len(byGroup))
	for _, location := range byGroup {
		out = append(out, *location)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LocationGroup < out[j].LocationGroup })
	return out, nil
}
func (f *fakeImportStore) ListRows(_ context.Context, id int64, filter domain.ImportRowFilter) (domain.ImportRowPage, error) {
	all := make([]domain.ImportRow, 0)
	for _, row := range f.rows[id] {
		if filter.LocationGroup != "" && row.LocationGroup != filter.LocationGroup {
			continue
		}
		if filter.Selected == "selected" && !row.SelectedFg {
			continue
		}
		if filter.Selected == "unselected" && row.SelectedFg {
			continue
		}
		if filter.Search != "" && !strings.Contains(strings.ToLower(row.Name), strings.ToLower(filter.Search)) {
			continue
		}
		all = append(all, row)
	}
	start := (filter.Page - 1) * filter.PageSize
	if start > len(all) {
		start = len(all)
	}
	end := min(start+filter.PageSize, len(all))
	return domain.ImportRowPage{Rows: append([]domain.ImportRow(nil), all[start:end]...), Total: len(all), Page: filter.Page, PageSize: filter.PageSize}, nil
}
func (f *fakeImportStore) RowIDs(_ context.Context, id int64, group string) ([]int64, error) {
	ids := make([]int64, 0)
	for _, row := range f.rows[id] {
		if group == "" || row.LocationGroup == group {
			ids = append(ids, row.ImportID)
		}
	}
	return ids, nil
}
func (f *fakeImportStore) SelectedIDs(_ context.Context, id int64) ([]int64, error) {
	ids := make([]int64, 0)
	for _, row := range f.rows[id] {
		if row.SelectedFg {
			ids = append(ids, row.ImportID)
		}
	}
	return ids, nil
}
func (f *fakeImportStore) AllRows(_ context.Context, id int64) ([]domain.ImportRow, error) {
	return append([]domain.ImportRow(nil), f.rows[id]...), nil
}
func (f *fakeImportStore) ReplaceSelection(_ context.Context, id int64, ids []int64) error {
	batch, ok := f.batches[id]
	if !ok {
		return domain.ErrNotFound
	}
	if batch.Status != domain.ImportStatusUploaded {
		return domain.ErrConflict
	}
	wanted := make(map[int64]bool, len(ids))
	for _, importID := range ids {
		wanted[importID] = true
	}
	for i := range f.rows[id] {
		f.rows[id][i].SelectedFg = wanted[f.rows[id][i].ImportID]
	}
	return nil
}
func (f *fakeImportStore) SelectedTotals(_ context.Context, id int64) (domain.ImportTotals, error) {
	var totals domain.ImportTotals
	for _, row := range f.rows[id] {
		if !row.SelectedFg {
			continue
		}
		totals.SelectedRows++
		if row.ItemType == domain.ItemTypeSpell {
			totals.SpellRows++
			totals.SpellQuantity += uint64(row.Quantity)
		} else {
			totals.ItemRows++
			totals.ItemUnits += uint64(row.Quantity)
		}
	}
	return totals, nil
}
func (f *fakeImportStore) SpellPreview(context.Context, int64) ([]domain.SpellPreview, error) {
	return nil, nil
}
func (f *fakeImportStore) BeginCommit(context.Context, int64) (service.ImportCommitTx, error) {
	return nil, domain.ErrUnavailable
}
func (f *fakeImportStore) RecoverInterrupted(context.Context) (int, error) { return 0, nil }
func (f *fakeImportStore) Ready() bool                                     { return true }

type fakeMetadataReader struct{}

func (fakeMetadataReader) GetItemsByEQUIDs(_ context.Context, equids []uint64) (map[uint64]domain.Item, error) {
	out := map[uint64]domain.Item{}
	for _, id := range equids {
		if id == 1 {
			out[id] = domain.Item{EQUID: 1, Name: "Canonical Item", ClassList: 1, RaceList: 1}
		}
	}
	return out, nil
}

func newImportHarness(t *testing.T, store *fakeImportStore) *harness {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "guildbank.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	if _, err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("sqlite.Migrate: %v", err)
	}
	settings := service.NewSettingsService(db)
	setup := service.NewSetupService(db, db, setupDBStub{})
	auth := service.NewAuthService(db, db, settings)
	imports := service.NewImportService(store, service.NewItemMetadataService(fakeMetadataReader{}), settings, time.Now, nil)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	router, err := NewRouter(Deps{
		Logger: logger, Health: fakeHealth{result: service.Readiness{Ready: true}},
		Setup: setup, Auth: auth, Settings: settings, Imports: imports, Version: "test",
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	server := httptest.NewServer(router)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	t.Cleanup(func() { server.Close(); _ = db.Close() })
	return &harness{t: t, server: server, client: client, store: db}
}

func (h *harness) uploadMultipart(path, csrf string, fileBody []byte) (*http.Response, []byte) {
	h.t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if csrf != "" {
		_ = writer.WriteField("csrf_token", csrf)
	}
	part, err := writer.CreateFormFile("file", "InventoryExport.txt")
	if err != nil {
		h.t.Fatal(err)
	}
	_, _ = part.Write(fileBody)
	_ = writer.Close()
	req, _ := http.NewRequest(http.MethodPost, h.server.URL+path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, data
}

func TestImportUploadStagesFromMultipartFormCSRF(t *testing.T) {
	store := newFakeImportStore()
	h := newImportHarness(t, store)
	h.completeSetup()
	csrf := h.csrf()
	content := []byte("Location\tName\tID\tCount\tSlots\n" +
		"Hoard 2-Slot1\tCanonical Item\t1\t2\t6\n" +
		"Hoard 3\tSpell: Minor Heal\t2\t1\t6\n")
	resp, body := h.uploadMultipart("/api/v1/imports", csrf, content)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload = %d: %s", resp.StatusCode, body)
	}
	if len(store.rows[1]) != 2 {
		t.Fatalf("staged rows = %d, want 2", len(store.rows[1]))
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "IMPORT_CREATED" {
		t.Fatalf("audits = %+v", store.audits)
	}
	var payload struct {
		Data struct {
			Batch batchResponse `json:"batch"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("upload JSON: %v", err)
	}
	if payload.Data.Batch.ID != 1 || payload.Data.Batch.Status != domain.ImportStatusUploaded {
		t.Fatalf("batch = %+v", payload.Data.Batch)
	}
}

func TestImportUploadRejectsMissingMultipartCSRF(t *testing.T) {
	store := newFakeImportStore()
	h := newImportHarness(t, store)
	h.completeSetup()
	resp, body := h.uploadMultipart("/api/v1/imports", "", []byte("Location\tName\tID\n"))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("upload without CSRF = %d: %s", resp.StatusCode, body)
	}
	if len(store.batches) != 0 {
		t.Fatalf("rejected upload created %d batches", len(store.batches))
	}
}

func TestImportSelectionAndPages(t *testing.T) {
	store := newFakeImportStore()
	store.batches[1] = domain.ImportBatch{ImportBatchID: 1, FileName: "InventoryExport.txt",
		ImportedBy: "admin", ImportedAt: time.Now().UTC(), Status: domain.ImportStatusUploaded}
	store.rows[1] = []domain.ImportRow{
		{ImportID: 1, ImportBatchID: 1, ItemType: domain.ItemTypeItem, EQUID: 1,
			Name: "Item", Quantity: 1, LocationGroup: "General 1",
			LocationSort: service.NaturalSortKey("General 1"), SelectedFg: true},
		{ImportID: 2, ImportBatchID: 1, ItemType: domain.ItemTypeSpell, EQUID: 2,
			Name: "Spell: X", Quantity: 2, LocationGroup: "Hoard",
			LocationSort: service.NaturalSortKey("Hoard"), SelectedFg: true},
	}
	h := newImportHarness(t, store)
	h.completeSetup()
	csrf := h.csrf()

	resp, body := h.request(http.MethodGet, "/admin/imports", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Recent batches") {
		t.Fatalf("imports page = %d: %s", resp.StatusCode, body)
	}
	resp, body = h.request(http.MethodGet, "/admin/imports/1", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `data-batch-id="1"`) {
		t.Fatalf("detail page = %d: %s", resp.StatusCode, body)
	}

	payload := []byte(`{"selectedImportIds":[2]}`)
	resp, body = h.request(http.MethodPut, "/api/v1/imports/1/selection", csrf, payload, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("selection PUT = %d: %s", resp.StatusCode, body)
	}
	if !store.rows[1][1].SelectedFg || store.rows[1][0].SelectedFg {
		t.Fatalf("selection not replaced: %+v", store.rows[1])
	}
	resp, body = h.request(http.MethodGet, "/api/v1/imports/1/selection", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"selectedImportIds":[2]`) {
		t.Fatalf("selection GET = %d: %s", resp.StatusCode, body)
	}
	resp, body = h.request(http.MethodGet, "/api/v1/imports/1/locations", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"group":"Hoard"`) {
		t.Fatalf("locations = %d: %s", resp.StatusCode, body)
	}
}

func TestImportRoutesRequireAuthentication(t *testing.T) {
	h := newImportHarness(t, newFakeImportStore())
	h.completeSetup()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/imports", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unauthenticated imports = %d: %s", resp.StatusCode, body)
	}
}
