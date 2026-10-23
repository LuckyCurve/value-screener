package engine

import (
	"testing"

	"github.com/LuckyCurve/value-screener/config"
)

// criteria is the default rule set with a short window, so tests stay small.
func criteria() config.Criteria {
	c := config.Default()
	c.ROEYears = 3
	c.CashYears = 3
	c.PEmax = 20
	c.ROEMin = 0.20
	c.ROESlack = 0
	return c
}

func roeOf(values ...float64) ROEReport {
	r := ROEReport{ROE: map[int]float64{}}
	for i, v := range values {
		r.ROE[2020+i] = v
	}
	return r
}

// balanceOf builds net-cash years from the net figure: a negative net means
// debt exceeded cash by that much.
func balanceOf(nets ...float64) BalanceReport {
	b := BalanceReport{Years: map[int]Balance{}}
	for i, net := range nets {
		if net >= 0 {
			b.Years[2020+i] = Balance{Cash: 100 + net, Debt: 100}
		} else {
			b.Years[2020+i] = Balance{Cash: 100 + net, Debt: 100}
		}
	}
	return b
}

func TestStockPassingEveryRule(t *testing.T) {
	v := Evaluate(Stock{Code: "HK.00001", Name: "ok", PE: 12},
		roeOf(0.25, 0.28, 0.30), balanceOf(50, 60, 70), criteria())
	if !v.Passed {
		t.Fatalf("expected pass, failures: %+v", v.Failures)
	}
	if len(v.Notes) != 0 {
		t.Errorf("unexpected notes: %v", v.Notes)
	}
}

func TestPEAboveCeilingFails(t *testing.T) {
	v := Evaluate(Stock{Code: "X", PE: 25},
		roeOf(0.25, 0.28, 0.30), balanceOf(50, 60, 70), criteria())
	if v.Passed {
		t.Fatal("PE 25 above a ceiling of 20 should fail")
	}
	if v.Failures[0].Criterion != "PE" {
		t.Errorf("expected a PE failure, got %+v", v.Failures)
	}
}

func TestNonPositivePEFails(t *testing.T) {
	for _, pe := range []float64{0, -8} {
		v := Evaluate(Stock{Code: "X", PE: pe},
			roeOf(0.25, 0.28, 0.30), balanceOf(50, 60, 70), criteria())
		if v.Passed {
			t.Errorf("PE %g should fail: a loss-maker has no meaningful trailing PE", pe)
		}
	}
}

func TestPEEqualToCeilingPasses(t *testing.T) {
	v := Evaluate(Stock{Code: "X", PE: 20},
		roeOf(0.25, 0.28, 0.30), balanceOf(50, 60, 70), criteria())
	if !v.Passed {
		t.Fatalf("PE exactly at the ceiling should pass, got %+v", v.Failures)
	}
}

func TestROEYearUnderFloorFails(t *testing.T) {
	v := Evaluate(Stock{Code: "X", PE: 10},
		roeOf(0.25, 0.19, 0.30), balanceOf(50, 60, 70), criteria())
	if v.Passed {
		t.Fatal("a year under the ROE floor should fail with no slack")
	}
	if got := v.Failures[0].Years; len(got) != 1 || got[0] != 2021 {
		t.Errorf("expected year 2021 named, got %v", got)
	}
}

func TestROESlackLetsOneYearThrough(t *testing.T) {
	c := criteria()
	c.ROESlack = 1
	v := Evaluate(Stock{Code: "X", PE: 10},
		roeOf(0.25, 0.19, 0.30), balanceOf(50, 60, 70), c)
	if !v.Passed {
		t.Fatalf("one year under floor with slack 1 should pass, got %+v", v.Failures)
	}
	if len(v.Notes) != 1 {
		t.Errorf("the year that used the slack should be noted, got %v", v.Notes)
	}
}

func TestROESlackStillFailsOnSecondMiss(t *testing.T) {
	c := criteria()
	c.ROESlack = 1
	v := Evaluate(Stock{Code: "X", PE: 10},
		roeOf(0.19, 0.18, 0.30), balanceOf(50, 60, 70), c)
	if v.Passed {
		t.Fatal("two misses against slack 1 should fail")
	}
}

func TestROEShortHistoryFails(t *testing.T) {
	v := Evaluate(Stock{Code: "X", PE: 10},
		roeOf(0.25, 0.28), balanceOf(50, 60, 70), criteria())
	if v.Passed {
		t.Fatal("two years of history against a three-year window should fail")
	}
	if v.Failures[0].Criterion != CritROE {
		t.Errorf("expected ROE failure, got %+v", v.Failures)
	}
}

func TestROEOnlyLooksAtMostRecentYears(t *testing.T) {
	// The old year is under the floor but sits outside the three-year window.
	r := ROEReport{ROE: map[int]float64{2018: 0.05, 2020: 0.25, 2021: 0.28, 2022: 0.30}}
	v := Evaluate(Stock{Code: "X", PE: 10}, r, balanceOf(50, 60, 70), criteria())
	if !v.Passed {
		t.Fatalf("a year outside the window should not be judged, got %+v", v.Failures)
	}
}

func TestNetCashUnderwaterYearFails(t *testing.T) {
	v := Evaluate(Stock{Code: "X", PE: 10},
		roeOf(0.25, 0.28, 0.30), balanceOf(50, -10, 70), criteria())
	if v.Passed {
		t.Fatal("debt above cash in one year should fail")
	}
	if v.Failures[0].Criterion != CritNetCash {
		t.Errorf("expected net cash failure, got %+v", v.Failures)
	}
}

func TestNetCashZeroMarginFails(t *testing.T) {
	v := Evaluate(Stock{Code: "X", PE: 10},
		roeOf(0.25, 0.28, 0.30), balanceOf(50, 0, 70), criteria())
	if v.Passed {
		t.Fatal("cash exactly equal to debt is not net cash")
	}
}

func TestNetCashShortHistoryFails(t *testing.T) {
	v := Evaluate(Stock{Code: "X", PE: 10},
		roeOf(0.25, 0.28, 0.30), balanceOf(50, 60), criteria())
	if v.Passed {
		t.Fatal("two balance sheets against a three-year window should fail")
	}
}

func TestBothCriteriaFailuresAreReported(t *testing.T) {
	v := Evaluate(Stock{Code: "X", PE: 10},
		roeOf(0.05, 0.05, 0.05), balanceOf(-1, -2, -3), criteria())
	if len(v.Failures) != 2 {
		t.Fatalf("expected both failures reported, got %+v", v.Failures)
	}
}

func TestValuationCarriesThroughWithoutJudging(t *testing.T) {
	// Percentile is reported and sorted on, but is not a pass/fail rule.
	v := Evaluate(Stock{Code: "X", PE: 10},
		roeOf(0.25, 0.28, 0.30), balanceOf(50, 60, 70), criteria())
	v.Valuation = &Valuation{Percentile: 95, Current: 19, Average: 10}
	if !v.Passed {
		t.Fatal("a high percentile must not fail a stock on its own")
	}
}

func TestUnavailableStockIsRejectedWithItsReason(t *testing.T) {
	v := EvaluateData(Stock{Code: "US.TCEHY", Name: "Tencent(ADR)", PE: 13},
		ROEReport{}, BalanceReport{}, config.Default(), "暂不提供美股 OTC 市场行情 TCEHY")
	if v.Passed {
		t.Fatal("a stock with no figures must not pass")
	}
	if v.Unavailable == "" {
		t.Fatal("the reason should be kept")
	}
	if len(v.Failures) != 0 {
		t.Errorf("it missed no rule; it had no figures: %+v", v.Failures)
	}
	if got := v.Summary(); got != "no data: 暂不提供美股 OTC 市场行情 TCEHY" {
		t.Errorf("summary %q should name the reason", got)
	}
}
