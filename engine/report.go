package engine

import (
	"fmt"
	"sort"

	"github.com/LuckyCurve/value-screener/config"
)

// Result is one screening run: the criteria that produced it, what passed,
// what did not, and what the channel could not answer for at all.
//
// Missing is kept apart from Rejected because it is a different kind of fact:
// a rejected stock was measured against the rules and fell short, while a
// missing one was never measured. Retrying is worth offering for the second
// and not the first.
type Result struct {
	Criteria config.Criteria `json:"criteria"`
	Passed   []Verdict       `json:"passed"`
	Rejected []Verdict       `json:"rejected"`
	Missing  []Verdict       `json:"missing,omitempty"`
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

// BuildResult splits verdicts into passed, rejected and missing, and sorts
// them.
func BuildResult(c config.Criteria, verdicts []Verdict) Result {
	r := Result{Criteria: c}
	for _, v := range verdicts {
		switch {
		case v.Unavailable != "":
			r.Missing = append(r.Missing, v)
		case v.Passed:
			r.Passed = append(r.Passed, v)
		default:
			r.Rejected = append(r.Rejected, v)
		}
	}
	SortPassed(r.Passed)
	sort.SliceStable(r.Rejected, func(i, j int) bool {
		return r.Rejected[i].Stock.Code < r.Rejected[j].Stock.Code
	})
	sort.SliceStable(r.Missing, func(i, j int) bool {
		return r.Missing[i].Stock.Code < r.Missing[j].Stock.Code
	})
	return r
}

// Add puts one more verdict in the group it belongs to, keeping the groups
// sorted. A run in flight uses it: each stock shows up as soon as it is
// judged, rather than at the end.
func (r *Result) Add(v Verdict) {
	switch {
	case v.Unavailable != "":
		r.Missing = append(r.Missing, v)
		sort.SliceStable(r.Missing, func(i, j int) bool {
			return r.Missing[i].Stock.Code < r.Missing[j].Stock.Code
		})
	case v.Passed:
		r.Passed = append(r.Passed, v)
		SortPassed(r.Passed)
	default:
		r.Rejected = append(r.Rejected, v)
		sort.SliceStable(r.Rejected, func(i, j int) bool {
			return r.Rejected[i].Stock.Code < r.Rejected[j].Stock.Code
		})
	}
}

// Replace swaps one stock's verdict for its code, puts it in the group its
// new verdict belongs to, and returns whether the code was found.
func (r *Result) Replace(v Verdict) bool {
	code := v.Stock.Code
	found := false
	all := make([]Verdict, 0, len(r.Passed)+len(r.Rejected)+len(r.Missing)+1)
	for _, vs := range [][]Verdict{r.Passed, r.Rejected, r.Missing} {
		for _, old := range vs {
			if old.Stock.Code == code {
				found = true
				continue
			}
			all = append(all, old)
		}
	}
	if !found {
		return false
	}
	*r = BuildResult(r.Criteria, append(all, v))
	return true
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
	if v.Unavailable != "" {
		return "no data: " + v.Unavailable
	}
	if len(v.Failures) == 0 {
		return "rejected"
	}
	f := v.Failures[0]
	line := f.Criterion + ": " + f.Reason
	if len(f.Years) > 0 {
		line += " in " + years(f.Years)
	}
	// A second broken rule is worth knowing about without opening the row.
	if len(v.Failures) > 1 {
		line += " (+1 more)"
	}
	return line
}

// years names the years a failure covered, newest first, the way a person
// reads them: "2024, 2023 and 2022", not a bracketed list.
//
// Past a few, the count carries the point and the list does not: a stock
// that missed fifteen years should not print fifteen of them.
func years(ys []int) string {
	if len(ys) > 4 {
		return fmt.Sprintf("%d years, %d to %d", len(ys), ys[len(ys)-1], ys[0])
	}
	out := make([]string, len(ys))
	for i, y := range ys {
		out[i] = fmt.Sprint(y)
	}
	switch len(out) {
	case 0:
		return ""
	case 1:
		return out[0]
	case 2:
		return out[0] + " and " + out[1]
	}
	return fmt.Sprintf("%s and %s", join(out[:len(out)-1]), out[len(out)-1])
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
