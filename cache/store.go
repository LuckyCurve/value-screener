// Package cache implements the screener's store: a SQLite file of the
// fiscal years that will never change again.
package cache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/LuckyCurve/value-screener/source"
	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

const schema = `
CREATE TABLE IF NOT EXISTS roe (
    code TEXT NOT NULL,
    year INTEGER NOT NULL,
    value REAL NOT NULL,
    PRIMARY KEY (code, year)
);
CREATE TABLE IF NOT EXISTS balance (
    code TEXT NOT NULL,
    year INTEGER NOT NULL,
    cash REAL NOT NULL,
    debt REAL NOT NULL,
    PRIMARY KEY (code, year)
);
CREATE TABLE IF NOT EXISTS failures (
    code TEXT NOT NULL PRIMARY KEY,
    reason TEXT NOT NULL,
    at TEXT NOT NULL
);`

// schemaVersion is stored in SQLite's user_version. A version below it means
// the file was written under a convention since found wrong, and the facts it
// holds cannot be told apart from the right ones: they are dropped and fetched
// again.
const schemaVersion = 1

// migrations brings an older cache up to schemaVersion. Version 1: balance
// rows used to be written with Hong Kong debt negated, which made the net-cash
// test pass every Hong Kong stock; there is no way to repair a stored sign
// without the statement it came from.
var migrations = map[int]string{
	0: `DELETE FROM balance;`,
}

// Cache is a SQLite database of closed fiscal years.
type Cache struct {
	mu sync.Mutex
	db *sql.DB
}

// Open opens the cache at path, creating it if needed. A cache that cannot
// be opened is returned nil: a screen without its cache is slower, not wrong.
func Open(path string) *Cache {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil
	}
	return &Cache{db: db}
}

// migrate applies every step between the file's version and this one.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for v := version; v < schemaVersion; v++ {
		step, ok := migrations[v]
		if !ok {
			return fmt.Errorf("no migration from cache version %d", v)
		}
		if _, err := db.Exec(step); err != nil {
			return err
		}
	}
	if version != schemaVersion {
		_, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
		return err
	}
	return nil
}

// OpenMemory opens a cache that lives as long as the process, for tests.
func OpenMemory() *Cache {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil
	}
	return &Cache{db: db}
}

// Close closes the database. A nil cache, which Open returns when it could
// not open one, closes safely: the run that made it goes on without a cache
// and could not call this anyway.
func (c *Cache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// ROE returns the cached ROE by year. A stock never seen yields an empty
// map, not an error.
func (c *Cache) ROE(code string) (map[int]float64, error) {
	out := map[int]float64{}
	if c == nil {
		return out, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.db.Query(`SELECT year, value FROM roe WHERE code = ?`, code)
	if err != nil {
		return nil, fmt.Errorf("read roe cache: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var y int
		var v float64
		if err := rows.Scan(&y, &v); err != nil {
			return nil, err
		}
		out[y] = v
	}
	return out, rows.Err()
}

// Balance returns the cached balance sheets.
func (c *Cache) Balance(code string) (map[int]source.Balance, error) {
	out := map[int]source.Balance{}
	if c == nil {
		return out, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.db.Query(`SELECT year, cash, debt FROM balance WHERE code = ?`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var y int
		var b source.Balance
		if err := rows.Scan(&y, &b.Cash, &b.Debt); err != nil {
			return nil, err
		}
		out[y] = b
	}
	return out, rows.Err()
}

// SaveROE writes every year of one stock's ROE in a transaction.
func (c *Cache) SaveROE(code string, years map[int]float64) error {
	if c == nil || len(years) == 0 {
		return nil
	}
	tx, stmt, err := c.begin(`INSERT OR REPLACE INTO roe (code, year, value) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	defer stmt.Close()
	for y, v := range years {
		if _, err := stmt.Exec(code, y, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SaveBalance writes every year of one stock's balance sheets.
func (c *Cache) SaveBalance(code string, years map[int]source.Balance) error {
	if c == nil || len(years) == 0 {
		return nil
	}
	tx, stmt, err := c.begin(`INSERT OR REPLACE INTO balance (code, year, cash, debt) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	defer stmt.Close()
	for y, b := range years {
		if _, err := stmt.Exec(code, y, b.Cash, b.Debt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (c *Cache) begin(query string) (*sql.Tx, *sql.Stmt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	stmt, err := tx.Prepare(query)
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}
	return tx, stmt, nil
}

// Failure returns the reason a code could not be fetched before, with the
// time it was recorded. A blank reason means it was never tried.
func (c *Cache) Failure(code string) (string, string, error) {
	if c == nil {
		return "", "", nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var reason, at string
	err := c.db.QueryRow(`SELECT reason, at FROM failures WHERE code = ?`, code).
		Scan(&reason, &at)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return reason, at, err
}

// PutFailure remembers that a code could not be fetched, so a later run does
// not spend a call on it again unless the user asks for a retry.
func (c *Cache) PutFailure(code, reason, at string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.Exec(`INSERT OR REPLACE INTO failures (code, reason, at) VALUES (?, ?, ?)`,
		code, reason, at)
	return err
}

// ClearFailure forgets a code's recorded failure, so the next run fetches it.
func (c *Cache) ClearFailure(code string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.Exec(`DELETE FROM failures WHERE code = ?`, code)
	return err
}

// Counts reports how many stocks and years are cached, for the window to
// show what its cache is worth.
func (c *Cache) Counts() (stocks, years int) {
	if c == nil {
		return 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.db.QueryRow(`SELECT COUNT(DISTINCT code) FROM roe`).Scan(&stocks)
	_ = c.db.QueryRow(`SELECT COUNT(*) FROM roe`).Scan(&years)
	return stocks, years
}
