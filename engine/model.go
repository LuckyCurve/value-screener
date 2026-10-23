// Package engine runs the screening rules over data reported by the Python
// data layer, and is the only place the pass/fail decisions are made.
package engine

// Criterion names, used in reports and tests so a failure points at the rule
// that rejected a stock.
const (
	CritROE     = "ROE"
	CritNetCash = "net cash"
)

// Stock is one first-pass candidate, as returned by the data layer.
type Stock struct {
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	PE        float64 `json:"pe"`
	PB        float64 `json:"pb"`
	MarketCap float64 `json:"market_cap"`
}

// ROEReport carries one stock's annual ROE, keyed by fiscal year.
// Values are fractions: 0.25 means 25%.
type ROEReport struct {
	ROE  map[int]float64 `json:"roe"`
	Note string          `json:"note,omitempty"`
}

// Balance is one fiscal year's cash and interest-bearing debt, in the
// report's own currency unit.
type Balance struct {
	Cash float64 `json:"cash"`
	Debt float64 `json:"debt"` // positive magnitude
}

// BalanceReport carries one stock's annual balance sheets, keyed by year.
type BalanceReport struct {
	Years map[int]Balance `json:"years"`
	Note  string          `json:"note,omitempty"`
}

// Valuation is one stock's pe history percentile.
type Valuation struct {
	Percentile float64 `json:"percentile"` // 0..100
	Average    float64 `json:"average"`
	Current    float64 `json:"current"`
	Forward    float64 `json:"forward"`
}

// Verdict is the outcome of applying the criteria to one stock.
type Verdict struct {
	Stock     Stock
	ROE       ROEReport
	Balance   BalanceReport
	Valuation *Valuation

	Passed bool
	// Failures names each criterion the stock missed, with the years that
	// caused it. Empty when Passed.
	Failures []Failure
	// Notes surfaces soft warnings: a year let through by the slack, a
	// denominator so small the ratio is meaningless.
	Notes []string
}

// Failure is one missed criterion.
type Failure struct {
	Criterion string  `json:"criterion"`
	Reason    string  `json:"reason"`
	Years     []int   `json:"years,omitempty"`
	Detail    float64 `json:"detail,omitempty"`
}
