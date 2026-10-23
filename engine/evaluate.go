package engine

import (
	"fmt"
	"sort"

	"github.com/LuckyCurve/value-screener/config"
)

// ROESeries returns the most recent n years of ROE, oldest first. Years with
// no figure are reported separately: a missing year is not a passing year.
func ROESeries(r ROEReport, n int) (years []int, values []float64, missing []int) {
	all := make([]int, 0, len(r.ROE))
	for y := range r.ROE {
		all = append(all, y)
	}
	sort.Ints(all)
	if len(all) > n {
		all = all[len(all)-n:]
	}
	for _, y := range all {
		years = append(years, y)
		values = append(values, r.ROE[y])
	}
	return years, values, missing
}

// CheckROE applies the ROE rule. It requires n years of history, allows at
// most slack years under the floor, and treats a year with no figure as a
// miss.
func CheckROE(r ROEReport, c config.Criteria) ([]Failure, []string) {
	years, values, _ := ROESeries(r, c.ROEYears)

	if len(years) < c.ROEYears {
		earliest := 0
		if len(years) > 0 {
			earliest = years[0]
		}
		return []Failure{{
			Criterion: CritROE,
			Reason: fmt.Sprintf("only %d annual reports, need %d (earliest %d)",
				len(years), c.ROEYears, earliest),
		}}, nil
	}

	var under []int
	for i, y := range years {
		if values[i] < c.ROEMin {
			under = append(under, y)
		}
	}
	if len(under) > c.ROESlack {
		return []Failure{{
			Criterion: CritROE,
			Reason: fmt.Sprintf("%d years below %.0f%% (slack %d)",
				len(under), c.ROEMin*100, c.ROESlack),
			Years: under,
		}}, nil
	}

	notes := make([]string, 0, len(under))
	for _, y := range under {
		notes = append(notes, fmt.Sprintf("%d fell under the ROE floor and used the slack", y))
	}
	return nil, notes
}

// CheckNetCash applies the net-cash rule: cash must exceed interest-bearing
// debt in every year of the window.
func CheckNetCash(b BalanceReport, c config.Criteria) ([]Failure, []string) {
	all := make([]int, 0, len(b.Years))
	for y := range b.Years {
		all = append(all, y)
	}
	sort.Ints(all)
	if len(all) > c.CashYears {
		all = all[len(all)-c.CashYears:]
	}

	if len(all) < c.CashYears {
		earliest := 0
		if len(all) > 0 {
			earliest = all[0]
		}
		return []Failure{{
			Criterion: CritNetCash,
			Reason: fmt.Sprintf("only %d balance sheets, need %d (earliest %d)",
				len(all), c.CashYears, earliest),
		}}, nil
	}

	var underwater []int
	for _, y := range all {
		bal := b.Years[y]
		if bal.Cash-bal.Debt <= 0 {
			underwater = append(underwater, y)
		}
	}
	if len(underwater) > 0 {
		return []Failure{{
			Criterion: CritNetCash,
			Reason:    fmt.Sprintf("%d years with debt above cash", len(underwater)),
			Years:     underwater,
		}}, nil
	}
	return nil, nil
}

// Evaluate applies every criterion to one stock.
func Evaluate(s Stock, roe ROEReport, bal BalanceReport, c config.Criteria) Verdict {
	v := Verdict{Stock: s, ROE: roe, Balance: bal}

	if s.PE <= 0 {
		v.Failures = append(v.Failures, Failure{
			Criterion: "PE",
			Reason:    "no positive trailing PE (loss-making or unavailable)",
		})
	} else if s.PE > c.PEmax {
		v.Failures = append(v.Failures, Failure{
			Criterion: "PE",
			Reason:    fmt.Sprintf("PE %.1f above %.1f", s.PE, c.PEmax),
			Detail:    s.PE,
		})
	}

	roeFail, roeNotes := CheckROE(roe, c)
	v.Failures = append(v.Failures, roeFail...)
	v.Notes = append(v.Notes, roeNotes...)

	cashFail, cashNotes := CheckNetCash(bal, c)
	v.Failures = append(v.Failures, cashFail...)
	v.Notes = append(v.Notes, cashNotes...)

	v.Passed = len(v.Failures) == 0
	return v
}
