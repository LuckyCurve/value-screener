package cache

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/LuckyCurve/value-screener/source"
)

// Balances written before the sign fix cannot be repaired without the
// statements they came from, so an old file's balance table is dropped and
// refetched. The ROE years were never affected and stay.
func TestAnOldCacheDropsItsBalancesNotItsROE(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO roe (code, year, value) VALUES ('HK.00700', 2024, 0.24)`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO balance (code, year, cash, debt) VALUES ('HK.00700', 2024, 10, -3)`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	c := Open(path)
	defer c.Close()
	if c == nil {
		t.Fatal("an older cache should open")
	}
	if roe, _ := c.ROE("HK.00700"); roe[2024] != 0.24 {
		t.Errorf("ROE years survive the migration, got %+v", roe)
	}
	if bal, _ := c.Balance("HK.00700"); len(bal) != 0 {
		t.Errorf("a differently-signed debt must not survive, got %+v", bal)
	}
}

func TestROERoundTrips(t *testing.T) {
	c := OpenMemory()
	defer c.Close()
	if err := c.SaveROE("HK.00700", map[int]float64{2024: 0.24, 2023: 0.21}); err != nil {
		t.Fatal(err)
	}
	got, err := c.ROE("HK.00700")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[2024] != 0.24 || got[2023] != 0.21 {
		t.Errorf("got %+v", got)
	}
}

func TestROEMissingCodeIsEmptyNotAnError(t *testing.T) {
	c := OpenMemory()
	defer c.Close()
	got, err := c.ROE("US.NOPE")
	if err != nil {
		t.Fatalf("a stock never seen should not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestSavingAgainReplacesTheYear(t *testing.T) {
	c := OpenMemory()
	defer c.Close()
	_ = c.SaveROE("HK.00700", map[int]float64{2024: 0.24})
	_ = c.SaveROE("HK.00700", map[int]float64{2024: 0.25})
	got, _ := c.ROE("HK.00700")
	if got[2024] != 0.25 || len(got) != 1 {
		t.Errorf("a restated year should replace the old one, got %+v", got)
	}
}

func TestBalanceRoundTrips(t *testing.T) {
	c := OpenMemory()
	defer c.Close()
	err := c.SaveBalance("HK.00700", map[int]source.Balance{
		2024: {Cash: 4.2e10, Debt: 1.1e10},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Balance("HK.00700")
	if err != nil {
		t.Fatal(err)
	}
	if got[2024].Cash != 4.2e10 || got[2024].Debt != 1.1e10 {
		t.Errorf("got %+v", got)
	}
}

func TestStocksAreKeptApart(t *testing.T) {
	c := OpenMemory()
	defer c.Close()
	_ = c.SaveROE("HK.00700", map[int]float64{2024: 0.24})
	_ = c.SaveROE("US.PEP", map[int]float64{2024: 0.52})
	a, _ := c.ROE("HK.00700")
	b, _ := c.ROE("US.PEP")
	if a[2024] != 0.24 || b[2024] != 0.52 {
		t.Errorf("one stock's years leaked into another: %+v %+v", a, b)
	}
}

func TestFailureIsRememberedAndForgotten(t *testing.T) {
	c := OpenMemory()
	defer c.Close()
	if err := c.PutFailure("US.TCEHY", "暂不提供美股 OTC 市场行情", "2026-10-23"); err != nil {
		t.Fatal(err)
	}
	reason, at, err := c.Failure("US.TCEHY")
	if err != nil {
		t.Fatal(err)
	}
	if reason == "" || at != "2026-10-23" {
		t.Errorf("got %q at %q", reason, at)
	}
	if err := c.ClearFailure("US.TCEHY"); err != nil {
		t.Fatal(err)
	}
	reason, _, _ = c.Failure("US.TCEHY")
	if reason != "" {
		t.Errorf("cleared failure still reads as %q", reason)
	}
}

func TestUnknownFailureIsBlank(t *testing.T) {
	c := OpenMemory()
	defer c.Close()
	reason, at, err := c.Failure("US.NOPE")
	if err != nil || reason != "" || at != "" {
		t.Errorf("got %q %q %v", reason, at, err)
	}
}

func TestCounts(t *testing.T) {
	c := OpenMemory()
	defer c.Close()
	_ = c.SaveROE("HK.00700", map[int]float64{2024: 0.24, 2023: 0.21})
	_ = c.SaveROE("US.PEP", map[int]float64{2024: 0.52})
	stocks, years := c.Counts()
	if stocks != 2 || years != 3 {
		t.Errorf("got %d stocks %d years", stocks, years)
	}
}

// A cache may never hold a price: the screen re-reads PE and the percentile
// every run. This test states that as a property of the schema, so a future
// column cannot quietly break it.
func TestSchemaHasNoPriceColumns(t *testing.T) {
	c := OpenMemory()
	defer c.Close()
	rows, err := c.db.Query(`SELECT name FROM pragma_table_info('roe')
		UNION SELECT name FROM pragma_table_info('balance')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	allowed := map[string]bool{
		"code": true, "year": true, "value": true, "cash": true, "debt": true,
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if !allowed[name] {
			t.Errorf("column %q does not belong in a cache of closed years", name)
		}
	}
}

func TestNilCacheIsHarmless(t *testing.T) {
	var c *Cache
	got, err := c.ROE("HK.00700")
	if err != nil || len(got) != 0 {
		t.Errorf("got %+v %v", got, err)
	}
	if err := c.SaveROE("HK.00700", map[int]float64{2024: 0.24}); err != nil {
		t.Errorf("a disabled cache should ignore writes: %v", err)
	}
	reason, _, err := c.Failure("HK.00700")
	if err != nil || reason != "" {
		t.Errorf("got %q %v", reason, err)
	}
}
