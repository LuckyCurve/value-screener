// Package config defines the screening criteria a user can tune without
// touching code. Every threshold the screener applies comes from here.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Criteria is the screening rule set, loaded from a JSON config file.
//
// The zero value is not usable: use Default and override fields from the
// file. Fields left out of the file keep their default.
type Criteria struct {
	// Markets to screen, e.g. ["HK", "US", "CN"].
	Markets []string `json:"markets"`

	// PEmax is the upper bound on the trailing PE ratio.
	// Positive-only; a stock trading at a loss (negative PE) is excluded.
	PEmax float64 `json:"pe_max"`

	// ROEMin is the floor a year must clear, as a fraction (0.20 = 20%).
	ROEMin float64 `json:"roe_min"`

	// ROEYears is how far back the ROE test looks, in annual reports.
	ROEYears int `json:"roe_years"`

	// ROESlack is how many of those years may fall under ROEMin.
	ROESlack int `json:"roe_slack"`

	// CashYears is how far back the net-cash test looks.
	CashYears int `json:"cash_years"`

	// PercentileYears is the valuation history window, in years.
	PercentileYears int `json:"percentile_years"`
}

// Default returns the criteria this project ships with.
func Default() Criteria {
	return Criteria{
		Markets:         []string{"HK", "US", "CN"},
		PEmax:           20,
		ROEMin:          0.20,
		ROEYears:        15,
		ROESlack:        0,
		CashYears:       10,
		PercentileYears: 10,
	}
}

// Load reads a criteria file. A missing file yields the default criteria, so
// the app runs before the user writes one.
func Load(path string) (Criteria, error) {
	c := Default()
	if path == "" {
		return c, nil
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	return c, nil
}

// Validate reports criteria that would silently produce empty or wrong
// results. A slack of years but no years to apply it to, for instance, would
// make every stock pass.
func (c Criteria) Validate() error {
	if c.PEmax <= 0 {
		return fmt.Errorf("pe_max must be positive, got %g", c.PEmax)
	}
	if c.ROEMin <= 0 {
		return fmt.Errorf("roe_min must be positive, got %g", c.ROEMin)
	}
	if c.ROEYears < 1 {
		return fmt.Errorf("roe_years must be at least 1, got %d", c.ROEYears)
	}
	if c.ROESlack < 0 || c.ROESlack >= c.ROEYears {
		return fmt.Errorf("roe_slack must be in [0, roe_years), got %d", c.ROESlack)
	}
	if c.CashYears < 1 {
		return fmt.Errorf("cash_years must be at least 1, got %d", c.CashYears)
	}
	if c.PercentileYears < 1 {
		return fmt.Errorf("percentile_years must be at least 1, got %d", c.PercentileYears)
	}
	if len(c.Markets) == 0 {
		return fmt.Errorf("markets must not be empty")
	}
	for _, m := range c.Markets {
		switch m {
		case "HK", "US", "CN":
		default:
			return fmt.Errorf("unknown market %q (want HK, US or CN)", m)
		}
	}
	return nil
}

// MarketsSorted returns the markets in a stable order, for reproducible output.
func (c Criteria) MarketsSorted() []string {
	out := append([]string(nil), c.Markets...)
	sort.Strings(out)
	return out
}

// Marshal renders criteria the way Load reads them, so the app can hand the
// data layer exactly the rules it is screening by.
func Marshal(c Criteria) ([]byte, error) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return append(data, '\n'), nil
}
