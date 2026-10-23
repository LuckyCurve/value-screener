package engine

import (
	"fmt"
	"sort"

	"github.com/LuckyCurve/value-screener/config"
)

// Result is one screening run: the criteria that produced it, what passed and
// what did not.
type Result struct {
	Criteria config.Criteria `json:"criteria"`
	Passed   []Verdict       `json:"passed"`
	Rejected []Verdict       `json:"rejected"`
}

// SortPassed orders passing stocks cheapest first by valuation percentile,
// falling back to PE for stocks with no percentile. Ties break on code so the
// report is stable across runs.
func SortPassed(vs []Verdict) {
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if (a.Valuation != nil) != (b.Valuation != nil) {
			return a.Valuation != nil
		}
		if a.Valuation != nil && a.Valuation.Percentile != b.Valuation.Percentile {
			return a.Valuation.Percentile < b.Valuation.Percentile
		}
		if a.Stock.PE != b.Stock.PE {
			return a.Stock.PE < b.Stock.PE
		}
		return a.Stock.Code < b.Stock.Code
	})
}

// BuildResult splits verdicts into passed and rejected, and sorts both.
func BuildResult(c config.Criteria, verdicts []Verdict) Result {
	r := Result{Criteria: c}
	for _, v := range verdicts {
		if v.Passed {
			r.Passed = append(r.Passed, v)
		} else {
			r.Rejected = append(r.Rejected, v)
		}
	}
	SortPassed(r.Passed)
	sort.SliceStable(r.Rejected, func(i, j int) bool {
		return r.Rejected[i].Stock.Code < r.Rejected[j].Stock.Code
	})
	return r
}

// ROERange returns the lowest and highest judged ROE, for summarising a stock
// in the report.
func ROERange(r ROEReport, n int) (min, max float64, ok bool) {
	_, values, _ := ROESeries(r, n)
	if len(values) == 0 {
		return 0, 0, false
	}
	min, max = values[0], values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max, true
}

// NetCashLowest returns the smallest cash-minus-debt over the judged window.
func NetCashLowest(b BalanceReport, n int) (float64, bool) {
	all := make([]int, 0, len(b.Years))
	for y := range b.Years {
		all = append(all, y)
	}
	sort.Ints(all)
	if len(all) > n {
		all = all[len(all)-n:]
	}
	if len(all) == 0 {
		return 0, false
	}
	lowest := b.Years[all[0]].Cash - b.Years[all[0]].Debt
	for _, y := range all[1:] {
		bal := b.Years[y]
		if net := bal.Cash - bal.Debt; net < lowest {
			lowest = net
		}
	}
	return lowest, true
}

// Summary is the one-line reason a rejected stock was rejected.
func (v Verdict) Summary() string {
	if v.Passed {
		return "passed"
	}
	if len(v.Failures) == 0 {
		return "rejected"
	}
	f := v.Failures[0]
	if len(f.Years) > 0 {
		return fmt.Sprintf("%s: %s (%v)", f.Criterion, f.Reason, f.Years)
	}
	return fmt.Sprintf("%s: %s", f.Criterion, f.Reason)
}
