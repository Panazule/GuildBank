package integration

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/repository/mariadb"
	"github.com/Panazule/guildbank/internal/service"
)

const phase4PerfDatabase = "GuildBankPhase4Perf"

// TestPhase4PublicInventoryPerformance is opt-in. It refuses to run against
// any database except the disposable Phase 4 performance schema.
func TestPhase4PublicInventoryPerformance(t *testing.T) {
	if os.Getenv("GUILDBANK_PHASE4_PERF") != "1" {
		t.Skip("set GUILDBANK_PHASE4_PERF=1 and provision the disposable GuildBankPhase4Perf schema")
	}
	cfg := phase4PerfConfig(t)
	if cfg.Database != phase4PerfDatabase {
		t.Fatalf("refusing to seed %q; use the disposable %s schema", cfg.Database, phase4PerfDatabase)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	store, err := mariadb.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open perf store: %v", err)
	}
	defer store.Close()
	version, err := store.Migrate(ctx)
	if err != nil {
		t.Fatalf("migrate perf store: %v", err)
	}
	t.Logf("schema version: %s", version)
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM GuildBankItems WHERE Name LIKE 'Phase4 %'`); err != nil {
		t.Fatalf("clear previous fixture: %v", err)
	}
	defer func() {
		if _, err := store.DB().ExecContext(context.Background(), `DELETE FROM GuildBankItems WHERE Name LIKE 'Phase4 %'`); err != nil {
			t.Errorf("cleanup fixture: %v", err)
		}
	}()

	rows := 250000
	if raw := os.Getenv("GUILDBANK_PHASE4_PERF_ROWS"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			t.Fatalf("invalid GUILDBANK_PHASE4_PERF_ROWS %q", raw)
		}
		rows = value
	}
	t.Logf("fixture rows: %d", rows)
	insertPhase4Fixtures(t, ctx, store)
	seedPhase4Inventory(t, ctx, store, rows)
	if _, err := store.DB().ExecContext(ctx, `ANALYZE TABLE GuildBankItems`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	assertPhase4NaturalOrder(t, ctx, store)

	detailID := firstPhase4ActiveID(t, ctx, store)
	base := domain.InventoryFilter{Page: 1, PageSize: 50, Sort: domain.InventorySortName}
	results := []perfResult{
		measurePerf(t, ctx, store, "default page 1", base, 20),
		measurePerf(t, ctx, store, "default page 5", withPage(base, 5), 20),
		measurePerf(t, ctx, store, "deep page 200", withPage(base, 200), 10),
		measurePerf(t, ctx, store, "search broad", withSearch(base, "Phase4 Perf 1"), 10),
		measurePerf(t, ctx, store, "search no match", withSearch(base, "no-such-item"), 10),
		measurePerf(t, ctx, store, "class WAR", withClass(base, "WAR"), 10),
		measurePerf(t, ctx, store, "race Human", withRace(base, "Human"), 10),
		measurePerf(t, ctx, store, "race Wood Elf", withRace(base, "Wood Elf"), 10),
		measurePerf(t, ctx, store, "combined", combinedFilter(base), 10),
		measurePerf(t, ctx, store, "date open asc", withSort(base, domain.InventorySortDateOpen), 10),
		measurePerf(t, ctx, store, "date open desc", withSort(base, domain.InventorySortDateOpenDesc), 10),
		measurePerf(t, ctx, store, "date bazaar desc", withSort(base, domain.InventorySortDateBazaarDesc), 10),
	}
	for _, result := range results {
		t.Logf("%-20s p50=%v p95=%v max=%v rows=%d", result.Name,
			result.P50.Round(time.Microsecond), result.P95.Round(time.Microsecond),
			result.Max.Round(time.Microsecond), result.Rows)
	}
	for _, result := range results {
		limit := 250 * time.Millisecond
		if rows > 250000 {
			limit = time.Second
		}
		if result.Name != "default page 1" && result.Name != "default page 5" && result.Name != "deep page 200" {
			limit = 750 * time.Millisecond
			if rows > 250000 {
				limit = 2 * time.Second
			}
		}
		if result.P95 > limit {
			t.Fatalf("%s p95 %v exceeds %v", result.Name, result.P95, limit)
		}
	}
	start := time.Now()
	if _, err := store.GetPublicInventory(ctx, detailID); err != nil {
		t.Fatalf("detail: %v", err)
	}
	if elapsed := time.Since(start); elapsed > detailLimit(rows) {
		t.Fatalf("detail took %v, exceeds %v", elapsed, detailLimit(rows))
	}

	for _, statement := range []string{
		`SELECT GuildBankItemID FROM GuildBankItems WHERE DeletedFg=0 ORDER BY NameSort ASC, GuildBankItemID ASC LIMIT 50`,
		`SELECT GuildBankItemID FROM GuildBankItems WHERE DeletedFg=0 AND ItemType='Item' ORDER BY NameSort ASC, GuildBankItemID ASC LIMIT 50`,
		`SELECT GuildBankItemID FROM GuildBankItems WHERE DeletedFg=0 AND (ClassString='ALL' OR FIND_IN_SET('WAR',ClassString)>0) ORDER BY NameSort ASC, GuildBankItemID ASC LIMIT 50`,
	} {
		rows, err := store.DB().QueryContext(ctx, "EXPLAIN "+statement)
		if err != nil {
			t.Fatalf("explain: %v", err)
		}
		columns, _ := rows.Columns()
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		for rows.Next() {
			if err := rows.Scan(pointers...); err != nil {
				t.Fatalf("scan explain: %v", err)
			}
			t.Logf("EXPLAIN %v", values)
		}
		_ = rows.Close()
	}
}

type perfResult struct {
	Name string
	P50  time.Duration
	P95  time.Duration
	Max  time.Duration
	Rows int
}

func measurePerf(t *testing.T, ctx context.Context, store *mariadb.Store, name string, filter domain.InventoryFilter, iterations int) perfResult {
	t.Helper()
	durations := make([]time.Duration, 0, iterations)
	var rows int
	for i := 0; i < iterations; i++ {
		start := time.Now()
		page, err := store.ListPublicInventory(ctx, filter)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		durations = append(durations, elapsed)
		rows = len(page.Records)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return perfResult{
		Name: name, P50: durations[len(durations)*50/100],
		P95: durations[len(durations)*95/100], Max: durations[len(durations)-1], Rows: rows,
	}
}
func detailLimit(rows int) time.Duration {
	if rows > 250000 {
		return 100 * time.Millisecond
	}
	return 50 * time.Millisecond
}

func phase4PerfConfig(t *testing.T) mariadb.Config {
	t.Helper()
	port, err := strconv.Atoi(os.Getenv("GUILDBANK_TEST_MARIADB_PORT"))
	if err != nil || port == 0 {
		port = 3306
	}
	return mariadb.Config{
		Host: os.Getenv("GUILDBANK_TEST_MARIADB_HOST"), Port: port,
		Database: os.Getenv("GUILDBANK_TEST_MARIADB_DATABASE"),
		Username: os.Getenv("GUILDBANK_TEST_MARIADB_USERNAME"),
		Password: os.Getenv("GUILDBANK_TEST_MARIADB_PASSWORD"),
	}
}

func seedPhase4Inventory(t *testing.T, ctx context.Context, store *mariadb.Store, rows int) {
	t.Helper()
	query := fmt.Sprintf(`INSERT INTO GuildBankItems
		(ItemType, EQUID, MetadataMissingFg, Name, ClassString, RaceString,
		 DateAdded, DateOpen, DateBazaar, Quantity, SourceLocation, GivenTo,
		 DeletedFg, ModAt, ModBy, ModByUserID, NameSort)
		SELECT IF(seq %% 50 = 0, 'Spell', 'Item'), seq, IF(seq %% 97 = 0, 1, 0),
		       CONCAT('Phase4 Perf ', LPAD(seq, 6, '0')),
		       CASE seq %% 5 WHEN 0 THEN 'ALL' WHEN 1 THEN 'WAR'
		            WHEN 2 THEN 'WAR,CLR' WHEN 3 THEN 'CLR,PAL' ELSE '' END,
		       CASE seq %% 4 WHEN 0 THEN 'ALL' WHEN 1 THEN 'Human'
		            WHEN 2 THEN 'Human,Erudite' ELSE 'Wood Elf' END,
		       DATE_ADD('2026-01-01', INTERVAL seq %% 365 DAY),
		       DATE_ADD(DATE_ADD('2026-01-01', INTERVAL seq %% 365 DAY), INTERVAL 14 DAY),
		       DATE_ADD(DATE_ADD('2026-01-01', INTERVAL seq %% 365 DAY), INTERVAL 30 DAY),
		       IF(seq %% 50 = 0, 2, 1), NULL, NULL, 0, UTC_TIMESTAMP(6), 'perf', NULL,
		       CONCAT('phase4', CHAR(1), 'perf', CHAR(1), LPAD(seq, 10, '0'))
		FROM seq_1_to_%d`, rows)
	if _, err := store.DB().ExecContext(ctx, query); err != nil {
		t.Fatalf("seed %d active rows: %v", rows, err)
	}
	deleted := fmt.Sprintf(`INSERT INTO GuildBankItems
		(ItemType, EQUID, MetadataMissingFg, Name, ClassString, RaceString,
		 DateAdded, DateOpen, DateBazaar, Quantity, SourceLocation, GivenTo,
		 DeletedFg, ModAt, ModBy, ModByUserID, NameSort)
		SELECT 'Item', seq, 0, CONCAT('Phase4 Deleted ', LPAD(seq, 6, '0')),
		       'WAR', 'Human', '2026-01-01', '2026-01-15', '2026-01-31', 1,
		       NULL, NULL, 1, UTC_TIMESTAMP(6), 'perf', NULL,
		       CONCAT('phase4', CHAR(1), 'deleted', CHAR(1), LPAD(seq, 10, '0'))
		FROM seq_1_to_%d`, rows/10)
	if _, err := store.DB().ExecContext(ctx, deleted); err != nil {
		t.Fatalf("seed deleted rows: %v", err)
	}
}

func insertPhase4Fixtures(t *testing.T, ctx context.Context, store *mariadb.Store) {
	t.Helper()
	for _, name := range []string{"Phase4 Order 1", "Phase4 Order 2", "Phase4 Order 10", "Phase4 Order 11"} {
		_, err := store.DB().ExecContext(ctx, `INSERT INTO GuildBankItems
			(ItemType, EQUID, MetadataMissingFg, Name, ClassString, RaceString,
			 DateAdded, DateOpen, DateBazaar, Quantity, SourceLocation, GivenTo,
			 DeletedFg, ModAt, ModBy, ModByUserID, NameSort)
			VALUES ('Item', 900001, 0, ?, 'WAR', 'Human', '2026-01-01', '2026-01-15',
			        '2026-01-31', 1, NULL, NULL, 0, UTC_TIMESTAMP(6), 'perf', NULL, ?)`,
			name, service.NaturalSortKey(name))
		if err != nil {
			t.Fatalf("insert order fixture %s: %v", name, err)
		}
	}
}

func assertPhase4NaturalOrder(t *testing.T, ctx context.Context, store *mariadb.Store) {
	t.Helper()
	page, err := store.ListPublicInventory(ctx, domain.InventoryFilter{
		Search: "Phase4 Order", Page: 1, PageSize: 10, Sort: domain.InventorySortName,
	})
	if err != nil {
		t.Fatalf("order query: %v", err)
	}
	want := []string{"Phase4 Order 1", "Phase4 Order 2", "Phase4 Order 10", "Phase4 Order 11"}
	if len(page.Records) != len(want) {
		t.Fatalf("order rows = %d, want %d", len(page.Records), len(want))
	}
	for i := range want {
		if page.Records[i].Name != want[i] {
			t.Fatalf("natural order[%d] = %q, want %q", i, page.Records[i].Name, want[i])
		}
	}
}

func firstPhase4ActiveID(t *testing.T, ctx context.Context, store *mariadb.Store) int64 {
	t.Helper()
	var id int64
	if err := store.DB().QueryRowContext(ctx, `SELECT GuildBankItemID FROM GuildBankItems
		WHERE DeletedFg=0 AND Name LIKE 'Phase4 Perf %' ORDER BY GuildBankItemID LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("detail id: %v", err)
	}
	return id
}

func withPage(filter domain.InventoryFilter, page int) domain.InventoryFilter {
	filter.Page = page
	return filter
}
func withSearch(filter domain.InventoryFilter, search string) domain.InventoryFilter {
	filter.Search = search
	return filter
}
func withClass(filter domain.InventoryFilter, class string) domain.InventoryFilter {
	filter.Class = class
	return filter
}
func withRace(filter domain.InventoryFilter, race string) domain.InventoryFilter {
	filter.Race = race
	return filter
}
func withSort(filter domain.InventoryFilter, sort domain.InventorySort) domain.InventoryFilter {
	filter.Sort = sort
	return filter
}
func combinedFilter(filter domain.InventoryFilter) domain.InventoryFilter {
	filter.Search, filter.Class, filter.Race = "Phase4 Perf", "WAR", "Human"
	filter.ItemType = domain.ItemTypeItem
	return filter
}
