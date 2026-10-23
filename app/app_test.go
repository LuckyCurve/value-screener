package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LuckyCurve/value-screener/config"
	"github.com/LuckyCurve/value-screener/engine"
	"github.com/egoist/mygo/ui"
)

// sample is a finished run with one passing stock and one rejected one.
func sample() engine.Result {
	return engine.Result{
		Criteria: config.Default(),
		Passed: []engine.Verdict{{
			Stock:     engine.Stock{Code: "HK.00700", Name: "Tencent", PE: 18.4},
			ROE:       engine.ROEReport{ROE: map[int]float64{2024: 0.24, 2023: 0.22}},
			Balance:   engine.BalanceReport{Years: map[int]engine.Balance{2024: {Cash: 4.2e10, Debt: 1.1e10}}},
			Valuation: &engine.Valuation{Percentile: 7.3, Average: 30.2},
			Passed:    true,
		}},
		Rejected: []engine.Verdict{{
			Stock:    engine.Stock{Code: "HK.00005", Name: "HSBC", PE: 9.1},
			Failures: []engine.Failure{{Criterion: engine.CritNetCash, Reason: "debt above cash in 3 years", Years: []int{2022, 2023, 2024}}},
		}},
	}
}

func newTestApp() *App {
	a := New()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		return sample(), nil
	}
	return a
}

// runAndWait clicks Run and waits for the run goroutine to finish.
func runAndWait(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("Run screen"); err != nil {
		t.Fatalf("click Run: %v", err)
	}
	tt.Frame()
	for i := 0; i < 100; i++ {
		if !tt.FramePending() {
			break
		}
		tt.Advance(10 * time.Millisecond)
		tt.Frame()
	}
}

func TestViewShowsEverySetting(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1200, 760)
	for _, label := range []string{
		"PE ceiling", "ROE floor (%)", "Years of ROE", "Years below floor",
		"Years of net cash", "PE history (years)", "Hong Kong", "A-shares",
	} {
		if !tt.HasText(label) {
			t.Errorf("the form should show %q, texts %q", label, tt.Texts())
		}
	}
}

func TestViewOpensOnTheShippedCriteria(t *testing.T) {
	a := newTestApp()
	if a.PE != 20 || a.ROEMin != 20 || a.ROEYears != 15 || a.CashYrs != 10 {
		t.Fatalf("defaults are %v/%v/%v/%v", a.PE, a.ROEMin, a.ROEYears, a.CashYrs)
	}
	tt := ui.NewTester(a.View, 1200, 760)
	if !tt.HasText("Run screen") {
		t.Errorf("no run button, texts %q", tt.Texts())
	}
}

func TestRunPassesFormValuesToScreen(t *testing.T) {
	a := newTestApp()
	got := make(chan config.Criteria, 1)
	a.Screen = func(_ context.Context, c config.Criteria, _ func(int, int, string)) (engine.Result, error) {
		got <- c
		return sample(), nil
	}
	a.PE = 12
	a.ROEMin = 25
	a.ROEYears = 10
	a.Slack = 1
	a.CashYrs = 8
	a.Percenti = 5
	a.US = false

	tt := ui.NewTester(a.View, 1200, 760)
	runAndWait(t, tt)

	select {
	case c := <-got:
		if c.PEmax != 12 || c.ROEMin != 0.25 || c.ROEYears != 10 ||
			c.ROESlack != 1 || c.CashYears != 8 || c.PercentileYears != 5 {
			t.Errorf("screen saw %+v", c)
		}
		if len(c.Markets) != 2 {
			t.Errorf("markets %v, want HK and CN only", c.Markets)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("screen never ran")
	}
}

func TestFinishedRunShowsPassedStock(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1200, 760)
	runAndWait(t, tt)

	for _, want := range []string{"Tencent", "HK.00700", "1 passed, 1 rejected"} {
		if !tt.HasText(want) {
			t.Errorf("want %q on screen, texts %q", want, tt.Texts())
		}
	}
}

func TestPassedTableShowsTheFigures(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1200, 760)
	runAndWait(t, tt)

	for _, want := range []string{"7.3%", "22.0%", "18.4"} {
		if !tt.HasText(want) {
			t.Errorf("want %q in the table, texts %q", want, tt.Texts())
		}
	}
}

func TestRejectedTabShowsWhyItFailed(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1200, 760)
	runAndWait(t, tt)

	if err := tt.Click("Rejected (1)"); err != nil {
		t.Fatalf("click Rejected: %v", err)
	}
	if !tt.HasText("HSBC") {
		t.Errorf("want the rejected stock, texts %q", tt.Texts())
	}
	if !tt.HasText("debt above cash") {
		t.Errorf("want the reason, texts %q", tt.Texts())
	}
	if tt.HasText("Tencent") {
		t.Errorf("the passed stock should not show under Rejected, texts %q", tt.Texts())
	}
}

func TestRunReportsProgressWhileScreening(t *testing.T) {
	a := newTestApp()
	release := make(chan struct{})
	started := make(chan struct{})
	a.Screen = func(_ context.Context, _ config.Criteria, progress func(int, int, string)) (engine.Result, error) {
		progress(3, 40, "HK.00700")
		close(started)
		<-release
		return sample(), nil
	}
	tt := ui.NewTester(a.View, 1200, 760)
	if err := tt.Click("Run screen"); err != nil {
		t.Fatal(err)
	}
	<-started
	tt.Frame()
	if !tt.HasText("3 of 40") {
		t.Errorf("want progress, texts %q", tt.Texts())
	}
	if !tt.HasText("Cancel") {
		t.Errorf("want a cancel button, texts %q", tt.Texts())
	}
	close(release)
}

func TestCancelStopsRun(t *testing.T) {
	a := newTestApp()
	cancelled := make(chan struct{})
	a.Screen = func(ctx context.Context, _ config.Criteria, _ func(int, int, string)) (engine.Result, error) {
		<-ctx.Done()
		close(cancelled)
		return engine.Result{}, ctx.Err()
	}
	tt := ui.NewTester(a.View, 1200, 760)
	if err := tt.Click("Run screen"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Cancel"); err != nil {
		t.Fatalf("click Cancel: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not reach the run")
	}
}

func TestCancelIsNotAFailure(t *testing.T) {
	a := newTestApp()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		return engine.Result{}, context.Canceled
	}
	a.Start()
	a.Wait()
	if a.Status() == statusFailed {
		t.Errorf("a cancelled run should not read as failed: %q", a.Message())
	}
}

func TestNoMarketIsRefusedBeforeRunning(t *testing.T) {
	a := newTestApp()
	called := make(chan struct{}, 1)
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		called <- struct{}{}
		return sample(), nil
	}
	a.HK, a.US, a.CN = false, false, false

	tt := ui.NewTester(a.View, 1200, 760)
	if err := tt.Click("Run screen"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	select {
	case <-called:
		t.Fatal("screen should not run without a market")
	case <-time.After(100 * time.Millisecond):
	}
	if !tt.HasText("markets") {
		t.Errorf("want an explanation, texts %q", tt.Texts())
	}
}

func TestSlackWiderThanWindowIsRefused(t *testing.T) {
	a := newTestApp()
	a.ROEYears = 10
	a.Slack = 10
	tt := ui.NewTester(a.View, 1200, 760)
	if err := tt.Click("Run screen"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("roe_slack") {
		t.Errorf("want the reason, texts %q", tt.Texts())
	}
}

func TestScreenErrorIsShown(t *testing.T) {
	a := newTestApp()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		return engine.Result{}, errors.New("OpenD is not running on 127.0.0.1:11111")
	}
	tt := ui.NewTester(a.View, 1200, 760)
	runAndWait(t, tt)
	if !tt.HasText("OpenD is not running") {
		t.Errorf("want the error on screen, texts %q", tt.Texts())
	}
}

func TestSecondRunIsRefusedWhileOneIsGoing(t *testing.T) {
	a := newTestApp()
	release := make(chan struct{})
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		<-release
		return sample(), nil
	}
	if !a.Start() {
		t.Fatal("the first run should start")
	}
	if a.Start() {
		t.Error("a second run should be refused")
	}
	close(release)
	a.Wait()
}

func TestNoResultBeforeARun(t *testing.T) {
	a := newTestApp()
	if a.Result() != nil {
		t.Error("a fresh app has no result")
	}
	tt := ui.NewTester(a.View, 1200, 760)
	if tt.HasText("passed,") {
		t.Errorf("no summary before a run, texts %q", tt.Texts())
	}
}

func TestClickingAStockShowsItsYears(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1280, 760)
	runAndWait(t, tt)

	if err := tt.Click("Tencent"); err != nil {
		t.Fatalf("click the row: %v", err)
	}
	// The year-by-year figures, which the table alone does not show.
	if !tt.HasText("2024   ROE") {
		t.Errorf("want the yearly detail, texts %q", tt.Texts())
	}
	if !tt.HasText("2023") {
		t.Errorf("want every year in the window, texts %q", tt.Texts())
	}
}

func TestDetailNamesTheYearsTheSlackAllowed(t *testing.T) {
	a := newTestApp()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		r := sample()
		r.Criteria.ROESlack = 1
		r.Passed[0].Notes = []string{"2022 fell under the ROE floor and used the slack"}
		return r, nil
	}
	tt := ui.NewTester(a.View, 1280, 760)
	runAndWait(t, tt)
	if err := tt.Click("Tencent"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("2022 fell under the ROE floor") {
		t.Errorf("want the slack note, texts %q", tt.Texts())
	}
}

func TestDetailIsAbsentUntilARowIsChosen(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1280, 760)
	runAndWait(t, tt)
	if tt.HasText("ROE") && tt.HasText("2024") {
		t.Errorf("no year rows before a choice, texts %q", tt.Texts())
	}
}
