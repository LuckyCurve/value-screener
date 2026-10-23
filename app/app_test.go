package app

import (
	"context"
	"errors"
	"strings"
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
		"PE ceiling", "ROE floor (%)", "Over", "Years may miss",
		"Net cash over", "PE history", "Hong Kong", "A-shares",
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

	for _, want := range []string{"Tencent", "HK.00700", "Passed", "Rejected"} {
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
	if !tt.HasText("24.0%") {
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

func TestStatCardsSummariseTheRun(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1280, 800)
	runAndWait(t, tt)
	if !tt.HasText("Passed") || !tt.HasText("Rejected") || !tt.HasText("Took") {
		t.Errorf("want the summary cards, texts %q", tt.Texts())
	}
}

func TestIdleWindowExplainsItself(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1280, 800)
	if !tt.HasText("No screen run yet") {
		t.Errorf("want an empty state, texts %q", tt.Texts())
	}
}

// withMissing is a finished run where the channel refused one stock.
func withMissing() engine.Result {
	r := sample()
	r.Missing = []engine.Verdict{{
		Stock:       engine.Stock{Code: "US.TCEHY", Name: "Tencent (ADR)", PE: 15.2},
		Unavailable: "暂不提供美股 OTC 市场行情",
	}}
	return r
}

func TestMissingStocksAreAGroupOfTheirOwn(t *testing.T) {
	a := newTestApp()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		return withMissing(), nil
	}
	tt := ui.NewTester(a.View, 1280, 800)
	runAndWait(t, tt)

	if !tt.HasText("No data (1)") {
		t.Fatalf("want a No data tab, texts %q", tt.Texts())
	}
	if err := tt.Click("No data (1)"); err != nil {
		t.Fatalf("click the tab: %v", err)
	}
	if !tt.HasText("US.TCEHY") {
		t.Errorf("want the stock, texts %q", tt.Texts())
	}
	if !tt.HasText("暂不提供美股 OTC 市场行情") {
		t.Errorf("want the channel's reason, texts %q", tt.Texts())
	}
	// Nothing was measured, so it is not among the rejected.
	if err := tt.Click("Rejected (1)"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("US.TCEHY") {
		t.Errorf("a missing stock should not sit under Rejected, texts %q", tt.Texts())
	}
}

func TestRetryAsksAgainAndMovesTheStock(t *testing.T) {
	a := newTestApp()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		return withMissing(), nil
	}
	asked := make(chan string, 1)
	a.Retry = func(_ context.Context, _ config.Criteria, s engine.Stock) (engine.Verdict, error) {
		asked <- s.Code
		return engine.Verdict{
			Stock:     s,
			ROE:       engine.ROEReport{ROE: map[int]float64{2024: 0.31}},
			Passed:    true,
			Valuation: &engine.Valuation{Percentile: 4},
		}, nil
	}
	tt := ui.NewTester(a.View, 1280, 800)
	runAndWait(t, tt)
	if err := tt.Click("No data (1)"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Retry US.TCEHY"); err != nil {
		t.Fatalf("click Retry: %v", err)
	}

	select {
	case code := <-asked:
		if code != "US.TCEHY" {
			t.Errorf("asked for %q", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the retry never ran")
	}
	for i := 0; i < 100 && a.Retrying() != ""; i++ {
		tt.Advance(10 * time.Millisecond)
		tt.Frame()
	}

	r := a.Result()
	if len(r.Missing) != 0 {
		t.Errorf("the stock should have left the missing group: %+v", r.Missing)
	}
	if len(r.Passed) != 2 {
		t.Errorf("it should have joined the passed group: %+v", r.Passed)
	}
	if tt.HasText("No data (1)") {
		t.Errorf("the tab should be gone, texts %q", tt.Texts())
	}
}

func TestARetryThatFailsAgainKeepsTheStockAndSaysWhy(t *testing.T) {
	a := newTestApp()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		return withMissing(), nil
	}
	a.Retry = func(context.Context, config.Criteria, engine.Stock) (engine.Verdict, error) {
		return engine.Verdict{}, errors.New("OpenD is not running on 127.0.0.1:11111")
	}
	tt := ui.NewTester(a.View, 1280, 800)
	runAndWait(t, tt)
	a.RetryStock("US.TCEHY")
	for i := 0; i < 100 && a.Retrying() != ""; i++ {
		tt.Advance(10 * time.Millisecond)
		tt.Frame()
	}
	r := a.Result()
	if len(r.Missing) != 1 {
		t.Fatalf("the stock should still be missing: %+v", r)
	}
	if !strings.Contains(r.Missing[0].Unavailable, "OpenD is not running") {
		t.Errorf("the reason should be this attempt's: %q", r.Missing[0].Unavailable)
	}
}

func TestRetryOnlyAppliesToMissingStocks(t *testing.T) {
	a := newTestApp()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		return withMissing(), nil
	}
	a.Retry = func(context.Context, config.Criteria, engine.Stock) (engine.Verdict, error) {
		t.Error("a stock with figures was asked for again")
		return engine.Verdict{}, nil
	}
	runAndWait(t, ui.NewTester(a.View, 1280, 800))
	if a.RetryStock("HK.00700") {
		t.Error("a passed stock should not be retried")
	}
}

func TestTheDialogClosesAndCanBeOpenedForTheSameStockAgain(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1280, 800)
	runAndWait(t, tt)
	if err := tt.Click("Tencent"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("NET CASH") {
		t.Fatalf("want the dialog, texts %q", tt.Texts())
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatalf("click Close: %v", err)
	}
	if tt.HasText("NET CASH") {
		t.Errorf("the dialog should be gone, texts %q", tt.Texts())
	}
	if err := tt.Click("Tencent"); err != nil {
		t.Fatalf("pick the same stock again: %v", err)
	}
	if !tt.HasText("NET CASH") {
		t.Errorf("the dialog should open again, texts %q", tt.Texts())
	}
}

func TestTheAppIsNamedOnce(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1280, 800)
	n := 0
	for _, text := range tt.Texts() {
		if text == "Value screen" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the name should appear once, found %d times: %q", n, tt.Texts())
	}
}

func TestTheTitleBarOffersTheWindowControls(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1280, 800)
	for _, want := range []string{"Minimize window", "Maximize window", "Close window"} {
		if !tt.HasText(want) {
			t.Errorf("want the %s control, texts %q", want, tt.Texts())
		}
	}
}

func TestAMissingStocksDialogSaysWhy(t *testing.T) {
	a := newTestApp()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		return withMissing(), nil
	}
	tt := ui.NewTester(a.View, 1280, 800)
	runAndWait(t, tt)
	if err := tt.Click("No data (1)"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Tencent (ADR)"); err != nil {
		t.Fatalf("pick the row: %v", err)
	}
	if !tt.HasText("The channel returned no figures") {
		t.Errorf("want the dialog, texts %q", tt.Texts())
	}
	if !tt.HasText("Ask again") {
		t.Errorf("want the retry in the dialog, texts %q", tt.Texts())
	}
}

func TestAPanicInTheScreenBecomesAFailureNotAnExit(t *testing.T) {
	a := newTestApp()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		panic("a nil map somewhere")
	}
	tt := ui.NewTester(a.View, 1280, 800)
	runAndWait(t, tt)
	if a.Status() != statusFailed {
		t.Errorf("a panic should read as a failure, status %v", a.Status())
	}
	if !strings.Contains(a.Message(), "a nil map somewhere") {
		t.Errorf("the reason should be kept, got %q", a.Message())
	}
}

func TestAPanicInARetryStaysInItsRow(t *testing.T) {
	a := newTestApp()
	a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
		return withMissing(), nil
	}
	a.Retry = func(context.Context, config.Criteria, engine.Stock) (engine.Verdict, error) {
		panic("a nil function somewhere")
	}
	tt := ui.NewTester(a.View, 1280, 800)
	runAndWait(t, tt)
	a.RetryStock("US.TCEHY")
	for i := 0; i < 100 && a.Retrying() != ""; i++ {
		tt.Advance(10 * time.Millisecond)
		tt.Frame()
	}
	r := a.Result()
	if r == nil || len(r.Missing) != 1 {
		t.Fatalf("the stock should still be missing: %+v", r)
	}
	if !strings.Contains(r.Missing[0].Unavailable, "a nil function somewhere") {
		t.Errorf("the reason should be in the row, got %q", r.Missing[0].Unavailable)
	}
}
