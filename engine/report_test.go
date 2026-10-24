package engine

import (
	"testing"

	"github.com/LuckyCurve/value-screener/config"
)

func DefaultC() config.Criteria { return config.Default() }

func verdict(code string, pe float64, pctl *float64) Verdict {
	v := Verdict{Stock: Stock{Code: code, PE: pe}, Passed: true}
	if pctl != nil {
		v.Valuation = &Valuation{Percentile: *pctl}
	}
	return v
}

func f(v float64) *float64 { return &v }

func TestSortPassedByPercentile(t *testing.T) {
	vs := []Verdict{
		verdict("A", 10, f(50)),
		verdict("B", 18, f(5)),
		verdict("C", 12, f(20)),
	}
	SortPassed(vs)
	want := []string{"B", "C", "A"}
	for i, w := range want {
		if vs[i].Stock.Code != w {
			t.Fatalf("position %d: want %s, got %s", i, w, vs[i].Stock.Code)
		}
	}
}

func TestSortPassedFallsBackToPEWithoutPercentile(t *testing.T) {
	vs := []Verdict{
		verdict("A", 15, nil),
		verdict("B", 9, nil),
		verdict("C", 12, f(80)),
	}
	SortPassed(vs)
	// C has a percentile so it leads; A and B follow, cheaper PE first.
	want := []string{"C", "B", "A"}
	for i, w := range want {
		if vs[i].Stock.Code != w {
			t.Fatalf("position %d: want %s, got %s", i, w, vs[i].Stock.Code)
		}
	}
}

func TestSortPassedIsStableOnTies(t *testing.T) {
	vs := []Verdict{
		verdict("Z", 10, f(5)),
		verdict("A", 10, f(5)),
	}
	SortPassed(vs)
	if vs[0].Stock.Code != "A" {
		t.Errorf("ties should break on code, got %s first", vs[0].Stock.Code)
	}
}

func TestBuildResultSplitsAndSorts(t *testing.T) {
	pass := verdict("P", 10, f(5))
	fail := verdict("Q", 10, nil)
	fail.Passed = false
	fail.Failures = []Failure{{Criterion: "PE", Reason: "too high"}}

	r := BuildResult(DefaultC(), []Verdict{fail, pass})
	if len(r.Passed) != 1 || r.Passed[0].Stock.Code != "P" {
		t.Errorf("passed split wrong: %+v", r.Passed)
	}
	if len(r.Rejected) != 1 || r.Rejected[0].Stock.Code != "Q" {
		t.Errorf("rejected split wrong: %+v", r.Rejected)
	}
}

func TestROERangeIgnoresYearsOutsideWindow(t *testing.T) {
	r := ROEReport{ROE: map[int]float64{2018: 0.90, 2020: 0.25, 2021: 0.28, 2022: 0.30}}
	min, max, ok := ROERange(r, 3)
	if !ok {
		t.Fatal("expected a range")
	}
	if min != 0.25 || max != 0.30 {
		t.Errorf("want 0.25..0.30, got %v..%v", min, max)
	}
}

func TestNetCashLowest(t *testing.T) {
	b := BalanceReport{Years: map[int]Balance{
		2020: {Cash: 100, Debt: 10},
		2021: {Cash: 100, Debt: 90},
		2022: {Cash: 100, Debt: 20},
	}}
	got, ok := NetCashLowest(b, 3)
	if !ok || got != 10 {
		t.Errorf("want 10, got %v (ok=%v)", got, ok)
	}
}

func TestSummaryNamesTheCriterion(t *testing.T) {
	v := Verdict{Stock: Stock{Code: "X"}, Failures: []Failure{
		{Criterion: CritROE, Reason: "1 years below 20%", Years: []int{2021}},
	}}
	got := v.Summary()
	if got == "" || got == "rejected" {
		t.Fatalf("summary should name the cause, got %q", got)
	}
}

// The years read the way a person says them: no brackets, no slice syntax.
func TestSummaryReadsTheYearsLikeASentence(t *testing.T) {
	cases := []struct {
		years []int
		want  string
	}{
		{[]int{2021}, "net cash: debt above cash in 2021"},
		{[]int{2023, 2022}, "net cash: debt above cash in 2023 and 2022"},
		{[]int{2024, 2023, 2022}, "net cash: debt above cash in 2024, 2023 and 2022"},
		// Past a few, the count says it and the list is noise.
		{[]int{2024, 2023, 2022, 2021, 2020, 2019},
			"net cash: debt above cash in 6 years, 2019 to 2024"},
	}
	for _, tc := range cases {
		v := Verdict{Failures: []Failure{{Criterion: CritNetCash,
			Reason: "debt above cash", Years: tc.years}}}
		if got := v.Summary(); got != tc.want {
			t.Errorf("summary %q, want %q", got, tc.want)
		}
	}
}

func TestAMissingStockIsItsOwnGroup(t *testing.T) {
	v := Verdict{Stock: Stock{Code: "US.TCEHY"}, Unavailable: "OTC not covered"}
	r := BuildResult(criteria(), []Verdict{
		v,
		{Stock: Stock{Code: "HK.00700"}, Passed: true},
	})
	if len(r.Missing) != 1 || len(r.Passed) != 1 || len(r.Rejected) != 0 {
		t.Fatalf("got passed %d rejected %d missing %d",
			len(r.Passed), len(r.Rejected), len(r.Missing))
	}
	if r.Missing[0].Stock.Code != "US.TCEHY" {
		t.Errorf("got %+v", r.Missing[0])
	}
}

func TestReplaceMovesAStockToItsNewGroup(t *testing.T) {
	r := BuildResult(criteria(), []Verdict{
		{Stock: Stock{Code: "US.TCEHY"}, Unavailable: "OTC not covered"},
		{Stock: Stock{Code: "HK.00700"}, Passed: true},
	})
	ok := r.Replace(Verdict{Stock: Stock{Code: "US.TCEHY"}, Passed: true})
	if !ok {
		t.Fatal("the code was not found")
	}
	if len(r.Missing) != 0 {
		t.Errorf("it should have left the missing group: %+v", r.Missing)
	}
	if len(r.Passed) != 2 {
		t.Errorf("it should have joined the passed group: %+v", r.Passed)
	}
}

func TestReplaceIgnoresAnUnknownCode(t *testing.T) {
	r := BuildResult(criteria(), []Verdict{{Stock: Stock{Code: "HK.00700"}, Passed: true}})
	if r.Replace(Verdict{Stock: Stock{Code: "HK.09988"}, Passed: true}) {
		t.Error("an unknown code should not be replaced")
	}
	if len(r.Passed) != 1 {
		t.Errorf("nothing should have been added: %+v", r.Passed)
	}
}
