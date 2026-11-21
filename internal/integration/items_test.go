package integration

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/repository/mariadb"
	"github.com/Panazule/guildbank/internal/service"
)

// The development source is the 144,712-row table profiled in DP-001. Running
// this test is opt-in because it requires the externally provisioned dataset.
const (
	integrationItemsRows     = int64(144712)
	integrationClassHighBits = 570
	integrationRaceHighBits  = 628
	integrationPageSize      = 1000
)

func TestItemMetadataIntegrationFullDevelopmentTable(t *testing.T) {
	cfg := integrationMariaDBConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store, err := mariadb.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open integration store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("close integration store: %v", err)
		}
	}()

	count, err := store.CountItems(ctx)
	if err != nil {
		t.Fatalf("CountItems: %v", err)
	}
	if count != integrationItemsRows {
		t.Fatalf("Items row count = %d, want the profiled %d; update DP-001 and this test if the source was refreshed", count, integrationItemsRows)
	}

	classDecoder := service.NewClassMaskDecoder()
	raceDecoder := service.NewRaceMaskDecoder()
	var seen int64
	var classHighBits, raceHighBits int
	var after uint64
	var previous uint64
	for {
		page, err := store.ListItems(ctx, after, integrationPageSize)
		if err != nil {
			t.Fatalf("ListItems after %d: %v", after, err)
		}
		if len(page) == 0 {
			break
		}
		for _, item := range page {
			if item.EQUID <= previous {
				t.Fatalf("keyset order broke at %d after %d", item.EQUID, previous)
			}
			previous = item.EQUID
			seen++
			classResult := classDecoder.Decode(item.ClassList)
			raceResult := raceDecoder.Decode(item.RaceList)
			classHighBits += countHighBitWarnings(classResult)
			raceHighBits += countHighBitWarnings(raceResult)
			if item.Name == "" {
				t.Fatalf("item %d has a blank name", item.EQUID)
			}
		}
		after = page[len(page)-1].EQUID
	}
	if seen != count {
		t.Fatalf("scanned %d items, CountItems reported %d", seen, count)
	}
	if classHighBits != integrationClassHighBits {
		t.Fatalf("class masks with undefined high bits = %d, want %d", classHighBits, integrationClassHighBits)
	}
	if raceHighBits != integrationRaceHighBits {
		t.Fatalf("race masks with undefined high bits = %d, want %d", raceHighBits, integrationRaceHighBits)
	}
	t.Logf("scanned %d items; warnings: class=%d race=%d", seen, classHighBits, raceHighBits)

	svc := service.NewItemMetadataService(store)
	for _, fixture := range []struct {
		equid   uint64
		name    string
		classes string
		races   string
	}{
		{25210, "Blade of Carnage", "WAR,PAL,RNG,SHD,BRD", "ALL"},
		{10802, "Flayed Skin Cap", "SHD,NEC", "Human,Erudite"},
	} {
		got, err := svc.Resolve(ctx, domain.ItemReference{EQUID: fixture.equid, Name: "stale imported name"})
		if err != nil {
			t.Fatalf("Resolve(%d): %v", fixture.equid, err)
		}
		if got.Name != fixture.name || got.ClassString != fixture.classes || got.RaceString != fixture.races {
			t.Fatalf("Resolve(%d) = %+v, want %s / %s / %s", fixture.equid, got, fixture.name, fixture.classes, fixture.races)
		}
		if got.MetadataMissing || len(got.Warnings) != 0 {
			t.Fatalf("Resolve(%d) = %+v, want complete metadata without warnings", fixture.equid, got)
		}
	}

	missing, err := svc.Resolve(ctx, domain.ItemReference{EQUID: 999999999, Name: "Imported Unresolved Item"})
	if err != nil {
		t.Fatalf("Resolve(missing): %v", err)
	}
	if !missing.MetadataMissing || missing.Name != "Imported Unresolved Item" ||
		missing.ClassString != "" || missing.RaceString != "" || len(missing.Warnings) != 1 {
		t.Fatalf("Resolve(missing) = %+v", missing)
	}
	if missing.Warnings[0].Code != service.ItemWarningMetadataMissing {
		t.Fatalf("missing warning = %+v", missing.Warnings[0])
	}
}

func countHighBitWarnings(result service.MaskDecodeResult) int {
	n := 0
	for _, warning := range result.Warnings {
		if warning.Code == service.MaskWarningUndefinedHighBits {
			n++
		}
	}
	return n
}

// integrationMariaDBConfig reads the opt-in integration settings. The test is
// skipped in normal builds; when GUILDBANK_INTEGRATION=1 is set, missing
// connection values are a configuration failure rather than a skip.
func integrationMariaDBConfig(t *testing.T) mariadb.Config {
	t.Helper()
	if os.Getenv("GUILDBANK_INTEGRATION") != "1" {
		t.Skip("set GUILDBANK_INTEGRATION=1 and the MariaDB test variables to run the full Items integration test")
	}
	cfg := mariadb.Config{
		Host:     envFirst("GUILDBANK_TEST_MARIADB_HOST", "GUILDBANK_MARIADB_HOST"),
		Database: envFirst("GUILDBANK_TEST_MARIADB_DATABASE", "GUILDBANK_MARIADB_DATABASE"),
		Username: envFirst("GUILDBANK_TEST_MARIADB_USERNAME", "GUILDBANK_MARIADB_USERNAME"),
		Password: envFirst("GUILDBANK_TEST_MARIADB_PASSWORD", "GUILDBANK_MARIADB_PASSWORD"),
		Port:     3306,
	}
	if value := envFirst("GUILDBANK_TEST_MARIADB_PORT", "GUILDBANK_MARIADB_PORT"); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("MariaDB integration port %q: %v", value, err)
		}
		cfg.Port = port
	}
	if cfg.Database == "" {
		cfg.Database = "GuildBank"
	}
	if cfg.Host == "" || cfg.Username == "" {
		t.Fatal("GUILDBANK_INTEGRATION=1 requires a MariaDB host and username")
	}
	return cfg
}

func envFirst(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
