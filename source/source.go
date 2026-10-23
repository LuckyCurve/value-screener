// Package source is where figures come from and how they are remembered.
//
// A Source is one channel — today Futu's OpenD, tomorrow another broker or a
// filing archive. The cache sits above it, in this package, because what is
// worth remembering is a fact about the data, not about the channel: a
// closed fiscal year never changes, whoever fetched it, and a price changes
// every tick, whoever fetched it.
package source

import (
	"context"

	"github.com/LuckyCurve/value-screener/engine"
)

// Candidate is one stock the first pass turned up, with its live figures.
type Candidate struct {
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	PE        float64 `json:"pe"`
	PB        float64 `json:"pb"`
	MarketCap float64 `json:"market_cap"`
}

// Annual is one stock's reports: the years that never change again, and
// today's valuation, which does.
type Annual struct {
	Code      string            `json:"code"`
	Name      string            `json:"name"`
	ROE       map[int]float64   `json:"roe,omitempty"`
	Balance   map[int]Balance   `json:"balance,omitempty"`
	Valuation *engine.Valuation `json:"valuation,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// Balance is one year's cash and interest-bearing debt.
type Balance struct {
	Cash float64 `json:"cash"`
	Debt float64 `json:"debt"`
}

// Progress reports how far a fetch has got, for the window.
//
// It may be nil: a caller with nothing to show, such as a retry of one
// stock, passes none, and report is then a no-op.
type Progress func(index, total int, message string)

// report calls p if there is anyone listening.
func (p Progress) report(index, total int, message string) {
	if p != nil {
		p(index, total, message)
	}
}

// Source is one channel of figures.
type Source interface {
	// Candidates returns the stocks whose live figures clear the prefilter.
	// Its results are never cached: PE moves with the price.
	Candidates(ctx context.Context, q Query, progress Progress) ([]Candidate, error)
	// Figures returns the annual reports and today's valuation for codes.
	Figures(ctx context.Context, q Query, codes []string, progress Progress) ([]Annual, error)
}

// Query is what the caller is screening for. A channel may use it to narrow
// what it fetches; the engine makes the decisions.
type Query struct {
	Markets         []string `json:"markets"`
	PEmax           float64  `json:"pe_max"`
	ROEMin          float64  `json:"roe_min"`
	ROEYears        int      `json:"roe_years"`
	ROESlack        int      `json:"roe_slack"`
	CashYears       int      `json:"cash_years"`
	PercentileYears int      `json:"percentile_years"`
}

// CachedYears is how many closed years the cache must hold before a fetch
// can be skipped. Short by even one year means fetching, because the missing
// year is usually why it is short — a newly listed stock, say.
func CachedYears(years map[int]float64, need int) bool { return len(years) >= need }
