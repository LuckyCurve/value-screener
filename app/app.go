// Package app is the Value screen window: the criteria form, the run, and
// the figures it turns up. Everything the window shows is a function of App,
// so a test can build and click it without a window.
package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/LuckyCurve/value-screener/config"
	"github.com/LuckyCurve/value-screener/engine"
	"github.com/egoist/mygo/ui"
)

// Status is where a run stands.
type Status int

const (
	statusIdle Status = iota
	statusRunning
	statusFinished
	statusFailed
)

// Screen runs one screening pass and returns what it found. It is injected so
// tests never reach OpenD.
type Screen func(ctx context.Context, c config.Criteria,
	progress func(index, total int, message string)) (engine.Result, error)

// Retrier asks the channel again for one stock, for the codes a run could
// not answer for.
type Retrier func(ctx context.Context, c config.Criteria,
	stock engine.Stock) (engine.Verdict, error)

// App is the window's state.
type App struct {
	// The form. These are the only places the criteria live while the user
	// edits them; Start reads them back into a config.Criteria.
	PE       float64
	ROEMin   float64 // percent, as typed
	ROEYears float64
	Slack    float64
	CashYrs  float64
	Percenti float64
	HK, US   bool
	CN       bool

	// Screen is set by New to run the data layer and the engine.
	Screen Screen
	// Retry asks the channel again for one stock the run could not answer
	// for. New wires the real one; a test may leave it nil.
	Retry Retrier

	mu       sync.Mutex
	status   Status
	progress int
	total    int
	message  string
	elapsed  string
	result   *engine.Result
	// stopped is set when the user cancelled: the result, if any, is what
	// the run had reached, not a complete screen.
	stopped bool
	// cancelling is set the moment Cancel is pressed, so the window says it
	// is stopping instead of looking stuck until the child dies.
	cancelling bool
	running    bool
	cancel     context.CancelFunc
	done       chan struct{}
	// retryCancel stops the retry in flight, kept apart from the run's own
	// cancel so one can never clear the other.
	retryCancel context.CancelFunc

	// tab is 0 for passing stocks, 1 for rejected ones, 2 for the ones the
	// channel could not answer for.
	tab int
	// chosen is the row picked in each table, -1 for none, kept apart so
	// switching tabs keeps every choice.
	chosen [3]int
	// detail is 0 for no dialog, 1 for a stock's figures, 2 for a refusal,
	// and detailCode is the stock it describes.
	detail     int
	detailCode string
	// retrying is the code being fetched again, "" for none.
	retrying string

	passed, rejected, missing ui.ListState

	// services lets the run goroutine ask for a frame.
	services ui.Services
}

// New returns an app on the shipped criteria, wired to the real data layer.
func New() *App {
	a := &App{Screen: DefaultScreen(), Retry: DefaultRetry()}
	a.load(config.Default())
	return a
}

// NewFrom returns an app on the given criteria, wired to the real data layer.
func NewFrom(c config.Criteria) *App {
	a := &App{Screen: DefaultScreen(), Retry: DefaultRetry()}
	a.load(c)
	return a
}

func (a *App) load(c config.Criteria) {
	a.PE = c.PEmax
	a.ROEMin = c.ROEMin * 100
	a.ROEYears = float64(c.ROEYears)
	a.Slack = float64(c.ROESlack)
	a.CashYrs = float64(c.CashYears)
	a.Percenti = float64(c.PercentileYears)
	a.HK, a.US, a.CN = false, false, false
	for _, m := range c.Markets {
		switch m {
		case "HK":
			a.HK = true
		case "US":
			a.US = true
		case "CN":
			a.CN = true
		}
	}
	a.chosen = [3]int{-1, -1, -1}
	a.passed.Selected = &a.chosen[0]
	a.passed.Key = func(row int) any { return a.key(0, row) }
	a.rejected.Selected = &a.chosen[1]
	a.rejected.Key = func(row int) any { return a.key(1, row) }
	a.missing.Selected = &a.chosen[2]
	a.missing.Key = func(row int) any { return a.key(2, row) }
}

// group is which list of a finished run a row belongs to.
type group int

const (
	groupPassed group = iota
	groupRejected
	groupMissing
)

// rows returns the stocks of one group.
func rows(r *engine.Result, g group) []engine.Verdict {
	switch g {
	case groupPassed:
		return r.Passed
	case groupMissing:
		return r.Missing
	default:
		return r.Rejected
	}
}

// key identifies a row by its code, so the choice follows a stock across runs
// rather than sticking to a row number.
func (a *App) key(g group, row int) string {
	a.mu.Lock()
	r := a.result
	a.mu.Unlock()
	if r == nil {
		return fmt.Sprint(row)
	}
	vs := rows(r, g)
	if row < 0 || row >= len(vs) {
		return fmt.Sprint(row)
	}
	return vs[row].Stock.Code
}

// chosenRow is the row picked in the table on show, -1 for none.
func (a *App) chosenRow() int {
	return a.chosen[a.tab]
}

// Criteria reads the form back into the rule set.
func (a *App) Criteria() config.Criteria {
	c := config.Default()
	c.PEmax = a.PE
	c.ROEMin = a.ROEMin / 100
	c.ROEYears = int(a.ROEYears)
	c.ROESlack = int(a.Slack)
	c.CashYears = int(a.CashYrs)
	c.PercentileYears = int(a.Percenti)
	c.Markets = nil
	if a.HK {
		c.Markets = append(c.Markets, "HK")
	}
	if a.US {
		c.Markets = append(c.Markets, "US")
	}
	if a.CN {
		c.Markets = append(c.Markets, "CN")
	}
	return c
}

// IsStopping reports whether a cancel is under way: pressed, but the channel
// has not stopped yet.
func (a *App) IsStopping() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancelling
}

// Status reports where the run stands.
func (a *App) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// Message is the last thing worth telling the user: what went wrong, or
// which stock is being fetched.
func (a *App) Message() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.message
}

// Result is the finished run, or nil.
func (a *App) Result() *engine.Result {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.result
}

// IsRunning reports whether a run is under way.
func (a *App) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

// Start begins a run. It returns false when the form would not produce a
// usable screen, or a run is already under way; the reason is then Message.
func (a *App) Start() bool {
	a.mu.Lock()
	// A retry in flight is still talking to the channel through the same
	// files a run uses; starting one now would have the two overwrite each
	// other's codes and figures.
	if a.running || a.retrying != "" {
		a.mu.Unlock()
		return false
	}
	a.mu.Unlock()

	c := a.Criteria()
	if err := c.Validate(); err != nil {
		a.mu.Lock()
		a.status, a.message, a.elapsed = statusFailed, err.Error(), ""
		a.mu.Unlock()
		a.invalidate()
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	a.mu.Lock()
	a.running = true
	a.cancel = cancel
	a.done = done
	a.status = statusRunning
	a.message = ""
	a.progress, a.total = 0, 0
	a.elapsed = ""
	a.result = nil
	a.stopped, a.cancelling = false, false
	a.mu.Unlock()
	a.chosen = [3]int{-1, -1, -1}
	a.detail, a.detailCode, a.tab = 0, "", 0

	go a.run(ctx, cancel, c, done)
	return true
}

func (a *App) run(ctx context.Context, cancel context.CancelFunc, c config.Criteria, done chan struct{}) {
	defer close(done)
	started := time.Now()

	res, err := a.screen(ctx, c, func(index, total int, message string) {
		a.mu.Lock()
		a.progress, a.total = index, total
		if message != "" {
			a.message = message
		}
		a.mu.Unlock()
		a.invalidate()
	})
	cancel()

	elapsed := time.Since(started).Round(time.Second).String()

	a.mu.Lock()
	a.running = false
	a.cancel = nil
	a.cancelling = false
	// A cancelled run keeps what it reached: the stocks judged before the
	// stop are as true as any, and throwing them away is the one thing a
	// cancel must not do.
	a.result = &res
	a.elapsed = elapsed
	switch {
	case errors.Is(err, context.Canceled):
		a.status, a.stopped, a.message = statusFinished, true, ""
	case err != nil:
		a.status, a.message = statusFailed, err.Error()
	default:
		a.status = statusFinished
		a.message = ""
	}
	a.mu.Unlock()
	a.invalidate()
}

// Cancel stops the run under way, if any. The child process is killed with
// the context, so OpenD is not left holding a stranded connection.
func (a *App) Cancel() {
	a.mu.Lock()
	cancel := a.cancel
	a.cancelling = cancel != nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// RetryStock asks the channel again for one stock the run could not answer
// for, and puts whatever comes back in the group it now belongs to.
func (a *App) RetryStock(code string) bool {
	if a.Retry == nil {
		return false
	}

	a.mu.Lock()
	if a.running || a.retrying != "" || a.result == nil {
		a.mu.Unlock()
		return false
	}
	var stock engine.Stock
	ok := false
	for _, v := range a.result.Missing {
		if v.Stock.Code == code {
			stock, ok = v.Stock, true
			break
		}
	}
	if !ok {
		a.mu.Unlock()
		return false
	}
	// The result this answer belongs to: a run started while the retry is in
	// flight replaces a.result with one of its own.
	target := a.result
	a.retrying = code
	ctx, cancel := context.WithCancel(context.Background())
	// Not a.cancel: that names the run under way, and a run can start while
	// this retry is still in flight. Clearing it here would leave that run
	// unstoppable.
	a.retryCancel = cancel
	a.mu.Unlock()
	a.invalidate()

	go func() {
		v, err := a.retry(ctx, a.Criteria(), stock)
		cancel()

		a.mu.Lock()
		a.retrying, a.retryCancel = "", nil
		// Only into the result it was asked about. A run started while the
		// retry was in flight has a result of its own, and this answer says
		// nothing about it.
		if target != nil && a.result == target && a.status == statusFinished {
			switch {
			case err == nil:
				target.Replace(v)
			case !errors.Is(err, context.Canceled):
				// The channel could not be reached at all: the stock keeps
				// its place, and its row says what went wrong this time.
				target.Replace(engine.Verdict{Stock: stock,
					Unavailable: err.Error()})
			}
		}
		a.mu.Unlock()
		a.invalidate()
	}()
	return true
}

// screen runs one pass, turning a panic into an error for the window: a bug
// in the run goroutine would otherwise close the app and lose the run.
func (a *App) screen(ctx context.Context, c config.Criteria,
	progress func(index, total int, message string)) (res engine.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the screen stopped: %v", r)
		}
	}()
	return a.Screen(ctx, c, progress)
}

// retry runs one retry, turning a panic into an error for the row it came
// from: a bug in a background goroutine would otherwise take the window
// down with it, and the user would lose the run they were looking at.
func (a *App) retry(ctx context.Context, c config.Criteria,
	stock engine.Stock) (v engine.Verdict, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the retry failed: %v", r)
		}
	}()
	return a.Retry(ctx, c, stock)
}

// Retrying is the code being fetched again, "" for none.
func (a *App) Retrying() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.retrying
}

// Wait blocks until the run under way is done.
func (a *App) Wait() {
	a.mu.Lock()
	done := a.done
	a.mu.Unlock()
	if done != nil {
		<-done
	}
}

// invalidate asks for a frame. The services are read under the lock because
// View stores them from the UI thread while a run may be calling this.
func (a *App) invalidate() {
	a.mu.Lock()
	s := a.services
	a.mu.Unlock()
	s.Invalidate()
}

// View draws the window.
func (a *App) View(c *ui.Context) {
	a.mu.Lock()
	a.services = c.Services()
	a.mu.Unlock()
	t := c.Theme()

	ui.Column(c).Fill().Children(func() {
		a.titleBar(c, t)
		ui.Row(c).Fill().Grow(1).Children(func() {
			ui.Column(c).Width(356).FillHeight().Padding(22, 18, 18, 18).
				Background(t.Surface).Gap(18).Children(func() {
				a.brand(c, t)
				a.form(c, t)
			})
			ui.Column(c).Grow(1).FillHeight().Padding(24, 20, 20, 20).Gap(16).Children(func() {
				a.results(c, t)
			})
		})
	})
	a.dialogs(c, t)
}

// brand is the app's header: mark, name, and what it screens.
func (a *App) brand(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(34, 34).Radius(9).
			Background(t.Accent).Center().Children(func() {
			ui.Icon(c, iconLogo).FontSize(20).TextColor(t.AccentText)
		})
		ui.Column(c).Gap(0).Children(func() {
			ui.Text(c, "Value screen").FontSize(16).Bold()
			ui.Text(c, "HK · US · A-shares").FontSize(11).TextColor(t.TextMuted)
		})
	})
}

// section is a titled group, so the form reads as settings rather than a
// wall of inputs.
func section(c *ui.Context, t *ui.Theme, icon *ui.SVG, title string, fn func()) {
	ui.Column(c).Gap(10).Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(14).TextColor(t.TextMuted)
			ui.Text(c, title).FontSize(11).Bold().TextColor(t.TextMuted)
		})
		fn()
	})
}

// card wraps content on a raised surface.
func card(c *ui.Context, t *ui.Theme, radius float32, fn func()) {
	ui.Box(c).FillWidth().Radius(radius).Background(t.Surface).
		Border(1, t.Border).Padding(14).Children(fn)
}

// stat is one headline number with its label and mark.
func stat(c *ui.Context, t *ui.Theme, label, value string, tint ui.Color, mark *ui.SVG) {
	ui.Box(c).Width(150).Radius(10).Background(t.Surface).
		Border(1, t.Border).Padding(12).Children(func() {
		ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, mark).FontSize(15).TextColor(tint)
			ui.Column(c).Gap(0).Children(func() {
				ui.Text(c, value).FontSize(17).Bold().TextColor(tint)
				ui.Text(c, label).FontSize(10).TextColor(t.TextMuted)
			})
		})
	})
}

func (a *App) form(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(18).Children(func() {
			section(c, t, iconRules, "VALUATION", func() {
				ui.Form(c, func() {
					ui.Field(c, "PE ceiling", func() {
						ui.NumberInput(c, &a.PE, 0.1, 500, 1).
							Tooltip("The highest trailing PE a stock may trade at. " +
								"Loss-making stocks have no PE and are left out.")
					})
					ui.Field(c, "PE history", func() {
						ui.Row(c).Gap(12).Children(func() {
							for _, y := range []int{1, 3, 5, 10} {
								ui.Radio(c, &a.Percenti, float64(y), fmt.Sprint(y))
							}
						}).Tooltip("How far back the percentile looks. It places today's " +
							"PE among every daily PE of that window, so lower is cheaper. " +
							"The comparison is with the stock's own history, not the market's.")
					})
				})
			})

			section(c, t, iconTrend, "QUALITY", func() {
				ui.Form(c, func() {
					ui.Field(c, "ROE floor (%)", func() {
						ui.NumberInput(c, &a.ROEMin, 0.1, 200, 1)
					})
					ui.Field(c, "Over", func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.NumberInput(c, &a.ROEYears, 1, 30, 1)
							ui.Text(c, "years").FontSize(13).TextColor(t.TextMuted)
						})
					})
					ui.Field(c, "Years may miss", func() {
						ui.NumberInput(c, &a.Slack, 0, 29, 1).
							Tooltip("How many of those years may come in under the floor. " +
								"Each one that does is named for the stock it let through.")
					})
				})
			})

			section(c, t, iconCoins, "BALANCE SHEET", func() {
				ui.Form(c, func() {
					ui.Field(c, "Net cash over", func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.NumberInput(c, &a.CashYrs, 1, 30, 1)
							ui.Text(c, "years").FontSize(13).TextColor(t.TextMuted)
						}).Tooltip("Cash must exceed interest-bearing debt in every one of " +
							"these years. Debt here is borrowings, not payables.")
					})
				})
			})

			section(c, t, iconRows, "MARKETS", func() {
				ui.Row(c).Gap(16).Children(func() {
					ui.Checkbox(c, &a.HK, "Hong Kong")
					ui.Checkbox(c, &a.US, "United States")
					ui.Checkbox(c, &a.CN, "A-shares")
				})
			})
		})
	})

	if a.IsRunning() {
		stopping := a.IsStopping()
		label := "Cancel"
		if stopping {
			label = "Stopping…"
		}
		btn := ui.Button(c, "").FillWidth().Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconStop).FontSize(15)
				ui.Text(c, label).SingleLine()
			})
		})
		btn.Disabled(stopping)
		if btn.Clicked() {
			a.Cancel()
		}
		return
	}

	btn := ui.PrimaryButton(c, "").FillWidth().Height(40).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconRun).FontSize(15).TextColor(t.AccentText)
			ui.Text(c, "Run screen").Bold().TextColor(t.AccentText)
		})
	})
	if btn.Clicked() {
		a.Start()
	}
}

func (a *App) results(c *ui.Context, t *ui.Theme) {
	a.mu.Lock()
	status, result, message := a.status, a.result, a.message
	progress, total, elapsed := a.progress, a.total, a.elapsed
	stopped := a.stopped
	a.mu.Unlock()

	switch status {
	case statusRunning:
		stopping := a.IsStopping()
		card(c, t, 12, func() {
			ui.Column(c).Gap(10).Children(func() {
				ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconClock).FontSize(16).TextColor(t.Accent)
					heading := "Screening"
					if stopping {
						heading = "Stopping"
					}
					ui.Text(c, heading).FontSize(14).Bold().Grow(1)
					pct := "…"
					if total > 0 {
						pct = fmt.Sprintf("%d%%", progress*100/total)
					}
					ui.Text(c, pct).FontSize(13).TextColor(t.TextMuted)
				})
				v := float64(-1)
				if total > 0 {
					v = float64(progress) / float64(total)
				}
				ui.Progress(c, v).FillWidth()
				label := "Asking OpenD for the first candidates"
				if total > 0 {
					label = fmt.Sprintf("%d of %d", progress, total)
					if message != "" {
						label += " · " + message
					}
				}
				ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
			})
		})

	case statusFailed:
		card(c, t, 12, func() {
			ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
				ui.Icon(c, iconCross).FontSize(16).TextColor(t.Danger)
				ui.Column(c).Grow(1).Gap(3).Children(func() {
					ui.Text(c, "Could not run").FontSize(14).Bold().TextColor(t.Danger)
					ui.Text(c, message).FontSize(12).TextColor(t.TextMuted)
				})
			})
		})
		// A run that failed part-way judged stocks before it did; they are
		// still worth looking at.
		if n := count(result); n > 0 {
			ui.Text(c, fmt.Sprintf("Judged before it stopped: %d stocks.", n)).
				FontSize(12).TextColor(t.TextMuted)
			a.finished(c, t, result, elapsed)
		}

	case statusFinished:
		if stopped {
			a.stoppedNote(c, t, result)
		}
		a.finished(c, t, result, elapsed)

	default:
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Icon(c, iconInfo).FontSize(30).TextColor(t.TextMuted).Opacity(0.5)
			ui.Text(c, "No screen run yet").FontSize(15).Bold()
			ui.Text(c, "Set the rules on the left, then run the screen.").
				FontSize(13).TextColor(t.TextMuted)
		})
		if message != "" {
			ui.Text(c, message).FontSize(12).TextColor(t.Danger)
		}
	}
}

// count is how many stocks a run judged, right or wrong.
func count(r *engine.Result) int {
	if r == nil {
		return 0
	}
	return len(r.Passed) + len(r.Rejected) + len(r.Missing)
}

// stoppedNote says a cancelled run was cancelled, and that the figures below
// are what it reached rather than the whole market.
func (a *App) stoppedNote(c *ui.Context, t *ui.Theme, r *engine.Result) {
	n := count(r)
	text := fmt.Sprintf("Stopped early: these are the %d stocks judged before you cancelled.", n)
	if n == 0 {
		text = "Stopped before any stock was judged."
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconStop).FontSize(13).TextColor(t.Warning)
		ui.Text(c, text).FontSize(12).TextColor(t.TextMuted)
	})
}

func (a *App) finished(c *ui.Context, t *ui.Theme, r *engine.Result, elapsed string) {
	if r == nil {
		return
	}
	ui.Row(c).Gap(10).Children(func() {
		stat(c, t, "Passed", fmt.Sprint(len(r.Passed)), t.Success, iconCheck)
		stat(c, t, "Rejected", fmt.Sprint(len(r.Rejected)), t.TextMuted, iconCross)
		if len(r.Missing) > 0 {
			stat(c, t, "No data", fmt.Sprint(len(r.Missing)), t.Warning, iconWarn)
		}
		if elapsed != "" {
			stat(c, t, "Took", elapsed, t.TextMuted, iconClock)
		}
	})

	labels := []string{
		fmt.Sprintf("Passed (%d)", len(r.Passed)),
		fmt.Sprintf("Rejected (%d)", len(r.Rejected)),
	}
	if len(r.Missing) > 0 {
		labels = append(labels, fmt.Sprintf("No data (%d)", len(r.Missing)))
	}
	// A tab that is no longer drawn must not stay chosen.
	if a.tab >= len(labels) {
		a.tab = 0
	}
	ui.Tabs(c, &a.tab, labels...)

	switch a.tab {
	case 1:
		a.rejectedTable(c, t, r)
	case 2:
		a.missingTable(c, t, r)
	default:
		a.passedTable(c, t, r)
	}
}

func (a *App) passedTable(c *ui.Context, t *ui.Theme, r *engine.Result) {
	if len(r.Passed) == 0 {
		ui.Text(c, "No stock met every rule. Loosen one on the left and run again.").
			FontSize(13).TextColor(t.TextMuted)
		return
	}
	cols := []ui.TableColumn{
		{Title: "Code", Width: 100},
		{Title: "Name", Width: 170},
		{Title: "PE", Width: 70, Align: ui.End},
	}
	showPercentile := r.Criteria.PercentileYears > 0
	if showPercentile {
		cols = append(cols, ui.TableColumn{Title: "Percentile", Width: 100, Align: ui.End})
	}
	cols = append(cols,
		ui.TableColumn{Title: "Lowest ROE", Width: 100, Align: ui.End},
		ui.TableColumn{Title: "Net cash", Align: ui.End})

	// Column positions shift when the percentile column is shown.
	pct, roe, cash := 3, 3, 4
	if showPercentile {
		roe, cash = 4, 5
	}

	table := ui.Table(c, &a.passed, cols, len(r.Passed), func(row, col int) {
		v := r.Passed[row]
		switch col {
		case 0:
			ui.Text(c, v.Stock.Code).SingleLine()
		case 1:
			ui.Text(c, v.Stock.Name).SingleLine()
		case 2:
			ui.Textf(c, "%.1f", v.Stock.PE)
		case pct:
			if v.Valuation == nil {
				ui.Text(c, "—").TextColor(t.TextMuted)
			} else {
				// Cheap against its own history is green, dear is red.
				colour := t.Text
				switch p := v.Valuation.Percentile; {
				case p < 30:
					colour = t.Success
				case p > 70:
					colour = t.Danger
				}
				ui.Textf(c, "%.1f%%", v.Valuation.Percentile).TextColor(colour)
			}
		case roe:
			if lo, _, ok := engine.ROERange(v.ROE, r.Criteria.ROEYears); ok {
				ui.Textf(c, "%.1f%%", lo*100)
			} else {
				ui.Text(c, "—").TextColor(t.TextMuted)
			}
		case cash:
			if net, ok := engine.NetCashLowest(v.Balance, r.Criteria.CashYears); ok {
				ui.Text(c, money(net)).TextColor(netColour(t, net))
			} else {
				ui.Text(c, "—").TextColor(t.TextMuted)
			}
		}
	}).Fill().Grow(1)
	a.openOnChoice(table)
}

func (a *App) rejectedTable(c *ui.Context, t *ui.Theme, r *engine.Result) {
	if len(r.Rejected) == 0 {
		ui.Text(c, "Every candidate passed.").FontSize(13).TextColor(t.TextMuted)
		return
	}
	cols := []ui.TableColumn{
		{Title: "Code", Width: 100},
		{Title: "Name", Width: 170},
		{Title: "PE", Width: 70, Align: ui.End},
		{Title: "Missed", Width: 150},
		{Title: "Why"},
	}
	table := ui.Table(c, &a.rejected, cols, len(r.Rejected), func(row, col int) {
		v := r.Rejected[row]
		switch col {
		case 0:
			ui.Text(c, v.Stock.Code).SingleLine()
		case 1:
			ui.Text(c, v.Stock.Name).SingleLine()
		case 2:
			ui.Textf(c, "%.1f", v.Stock.PE)
		case 3:
			if len(v.Failures) > 0 {
				ui.Text(c, v.Failures[0].Criterion).SingleLine()
			}
		case 4:
			ui.Text(c, v.Summary()).SingleLine().TextColor(t.TextMuted)
		}
	}).Fill().Grow(1)
	a.openOnChoice(table)
}

// missingTable is the stocks the channel could not answer for. They are a
// group of their own because nothing was measured: the row says why, and its
// button asks the channel again.
func (a *App) missingTable(c *ui.Context, t *ui.Theme, r *engine.Result) {
	if len(r.Missing) == 0 {
		ui.Text(c, "The channel answered for every candidate.").
			FontSize(13).TextColor(t.TextMuted)
		return
	}
	retrying := a.Retrying()
	cols := []ui.TableColumn{
		{Title: "Code", Width: 100},
		{Title: "Name", Width: 160},
		{Title: "PE", Width: 70, Align: ui.End},
		{Title: "Why not"},
		{Title: "", Width: 96},
	}
	table := ui.Table(c, &a.missing, cols, len(r.Missing), func(row, col int) {
		v := r.Missing[row]
		switch col {
		case 0:
			ui.Text(c, v.Stock.Code).SingleLine()
		case 1:
			ui.Text(c, v.Stock.Name).SingleLine()
		case 2:
			ui.Textf(c, "%.1f", v.Stock.PE)
		case 3:
			ui.Text(c, v.Unavailable).SingleLine().TextColor(t.Warning)
		case 4:
			if retrying == v.Stock.Code {
				ui.Text(c, "Asking…").FontSize(12).TextColor(t.TextMuted)
				return
			}
			b := ui.ButtonBase(c).Padding(3, 8).Radius(6).
				Label("Retry " + v.Stock.Code)
			if b.Hovered() {
				b.Background(t.SurfaceHover)
			}
			b.Children(func() {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconRetry).FontSize(12).TextColor(t.TextMuted)
					ui.Text(c, "Retry").FontSize(12).TextColor(t.Text)
				})
			})
			if b.Clicked() && retrying == "" {
				a.RetryStock(v.Stock.Code)
			}
		}
	}).Fill().Grow(1)
	a.openOnChoice(table)
}

// openOnChoice opens the dialog for the row just chosen: picking a stock is
// asking to see it.
func (a *App) openOnChoice(table ui.Element) {
	if (table.Changed() || table.Submitted()) && a.chosenRow() >= 0 {
		a.openDetail()
	}
}

// openDetail opens the dialog for the row chosen in the tab on show.
func (a *App) openDetail() {
	a.mu.Lock()
	r := a.result
	code := a.chosenCode()
	a.mu.Unlock()
	if r == nil || code == "" {
		return
	}
	v, ok := findVerdict(r, code)
	if !ok {
		return
	}
	a.detailCode = code
	a.detail = 1
	if v.Unavailable != "" {
		a.detail = 2
	}
}

// chosenCode is the code of the row picked in the tab on show, "" for none.
//
// The dialog follows a stock, not a row number: a retry moves a stock into
// another group, and a dialog pinned to row 3 would then describe whatever
// stock row 3 became.
func (a *App) chosenCode() string {
	row := a.chosen[a.tab]
	if a.result == nil || row < 0 {
		return ""
	}
	vs := rows(a.result, group(a.tab))
	if row >= len(vs) {
		return ""
	}
	return vs[row].Stock.Code
}

// findVerdict returns a stock's verdict from whichever group now holds it.
func findVerdict(r *engine.Result, code string) (engine.Verdict, bool) {
	if r == nil {
		return engine.Verdict{}, false
	}
	for _, vs := range [][]engine.Verdict{r.Passed, r.Rejected, r.Missing} {
		for _, v := range vs {
			if v.Stock.Code == code {
				return v, true
			}
		}
	}
	return engine.Verdict{}, false
}

// closeDetail closes the dialog and lets the same row be picked again.
func (a *App) closeDetail() {
	a.detail = 0
	a.detailCode = ""
	a.chosen[a.tab] = -1
}

// dialogs draws the dialog that may be open over the window.
func (a *App) dialogs(c *ui.Context, t *ui.Theme) {
	a.mu.Lock()
	r, code := a.result, a.detailCode
	a.mu.Unlock()
	if a.detail == 0 || r == nil || code == "" {
		return
	}
	v, ok := findVerdict(r, code)
	if !ok {
		// The stock left the report, so there is nothing to describe.
		a.closeDetail()
		return
	}

	open := a.detail != 0
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(dialogWidth(c)).Gap(14).Children(func() {
			a.dialogHead(c, t, r, v)
			ui.Divider(c)
			if v.Unavailable != "" {
				a.dialogMissing(c, t, v)
			} else {
				a.dialogFigures(c, t, r, v)
			}
		})
	})
	if !open {
		a.detail, a.detailCode = 0, ""
	}
}

// dialogWidth is wide enough for a year's row and short of the window's
// edges.
func dialogWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	if w-100 < 580 {
		return w - 100
	}
	return 580
}

// dialogHead names the stock, how cheap it is, and closes the dialog.
func (a *App) dialogHead(c *ui.Context, t *ui.Theme, r *engine.Result, v engine.Verdict) {
	ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, v.Stock.Name).FontSize(19).Bold().SingleLine()
			ui.Text(c, v.Stock.Code).FontSize(12).TextColor(t.TextMuted)
		})

		if v.Stock.PE > 0 {
			chip(c, t, fmt.Sprintf("PE %.1f", v.Stock.PE), t.TextMuted)
		}
		if v.Valuation != nil {
			colour := t.Text
			switch p := v.Valuation.Percentile; {
			case p < 30:
				colour = t.Success
			case p > 70:
				colour = t.Danger
			}
			chip(c, t, fmt.Sprintf("%.0f%% of its %.0f-yr PE",
				v.Valuation.Percentile, float64(r.Criteria.PercentileYears)), colour)
		}

		b := ui.ButtonBase(c).Size(28, 28).Radius(6).Center().Label("Close")
		if b.Hovered() {
			b.Background(t.SurfaceHover)
		}
		b.Children(func() { ui.Icon(c, iconClose).FontSize(11).TextColor(t.TextMuted) })
		if b.Clicked() {
			a.closeDetail()
		}
	})
}

// dialogFigures is the year-by-year table: every figure the rules looked at,
// with the years that broke one marked.
func (a *App) dialogFigures(c *ui.Context, t *ui.Theme, r *engine.Result, v engine.Verdict) {
	for _, f := range v.Failures {
		ui.Row(c).Gap(7).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, iconCross).FontSize(13).TextColor(t.Danger)
			ui.Text(c, f.Criterion+": "+detailReason(f)).FontSize(12).
				TextColor(t.Danger).Grow(1)
		})
	}
	for _, note := range v.Notes {
		ui.Row(c).Gap(7).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, iconInfo).FontSize(13).TextColor(t.Warning)
			ui.Text(c, note).FontSize(12).TextColor(t.TextMuted).Grow(1)
		})
	}

	years := make([]int, 0, len(v.ROE.ROE)+len(v.Balance.Years))
	seen := map[int]bool{}
	for y := range v.ROE.ROE {
		years, seen[y] = append(years, y), true
	}
	for y := range v.Balance.Years {
		if !seen[y] {
			years = append(years, y)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	if len(years) == 0 {
		ui.Text(c, "No annual figures were reported.").FontSize(13).
			TextColor(t.TextMuted)
		return
	}

	failed := map[int]bool{}
	for _, f := range v.Failures {
		for _, y := range f.Years {
			failed[y] = true
		}
	}

	ui.Row(c).Gap(0).Padding(0, 6, 6, 6).Children(func() {
		ui.Text(c, "YEAR").Width(60).FontSize(10).Bold().TextColor(t.TextMuted)
		ui.Text(c, "ROE").Width(90).FontSize(10).Bold().TextColor(t.TextMuted)
		ui.Text(c, "CASH").Width(90).FontSize(10).Bold().TextColor(t.TextMuted)
		ui.Text(c, "DEBT").Width(90).FontSize(10).Bold().TextColor(t.TextMuted)
		ui.Text(c, "NET CASH").FontSize(10).Bold().TextColor(t.TextMuted)
	})
	ui.Divider(c)

	// As tall as the years need, up to a fixed ceiling: a stock with two
	// reports should not open a dialog two thirds empty.
	height := float32(len(years))*29 + 4
	if height > 268 {
		height = 268
	}
	ui.Scroll(c).Height(height).FillWidth().Children(func() {
		ui.Column(c).Gap(1).Children(func() {
			for _, y := range years {
				roe, hasROE := v.ROE.ROE[y]
				bal, hasBal := v.Balance.Years[y]

				row := ui.Row(c).Gap(0).Padding(5, 6).Radius(4)
				if failed[y] {
					row.Background(ui.RGBA(220, 38, 38, 0.08))
				}
				row.Children(func() {
					ui.Textf(c, "%d", y).Width(60).FontSize(12).TextColor(t.TextMuted)
					if !hasROE {
						ui.Text(c, "—").Width(90).FontSize(12).TextColor(t.TextMuted)
					} else {
						colour := t.Text
						if roe < r.Criteria.ROEMin {
							colour = t.Warning
						}
						ui.Textf(c, "%.1f%%", roe*100).Width(90).
							FontSize(12).TextColor(colour)
					}
					if !hasBal {
						ui.Text(c, "—").FontSize(12).TextColor(t.TextMuted)
						return
					}
					ui.Text(c, money(bal.Cash)).Width(90).FontSize(12).
						TextColor(t.TextMuted)
					ui.Text(c, money(bal.Debt)).Width(90).FontSize(12).
						TextColor(t.TextMuted)
					net := bal.Cash - bal.Debt
					ui.Text(c, money(net)).FontSize(12).TextColor(netColour(t, net))
				})
			}
		})
	})
}

// detailReason names the years a rule was broken in, when it has them.
func detailReason(f engine.Failure) string {
	if len(f.Years) == 0 {
		return f.Reason
	}
	return fmt.Sprintf("%s (%v)", f.Reason, f.Years)
}

// dialogMissing says why a stock has no figures, and offers to ask again.
func (a *App) dialogMissing(c *ui.Context, t *ui.Theme, v engine.Verdict) {
	ui.Row(c).Gap(9).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, iconWarn).FontSize(15).TextColor(t.Warning)
		ui.Column(c).Grow(1).Gap(5).Children(func() {
			ui.Text(c, "The channel returned no figures").FontSize(13).Bold()
			ui.Text(c, v.Unavailable).FontSize(12).TextColor(t.TextMuted).Selectable()
		})
	})

	retrying := a.Retrying() == v.Stock.Code
	b := ui.PrimaryButton(c, "").Width(150).Height(34)
	b.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconRetry).FontSize(13).TextColor(t.AccentText)
			label := "Ask again"
			if retrying {
				label = "Asking…"
			}
			ui.Text(c, label).TextColor(t.AccentText).SingleLine()
		})
	})
	if retrying {
		b.Disabled(true)
	}
	if b.Clicked() {
		a.RetryStock(v.Stock.Code)
	}
}

// netColour marks a net-cash figure: red where debt has overtaken cash.
func netColour(t *ui.Theme, net float64) ui.Color {
	if net <= 0 {
		return t.Danger
	}
	return t.Text
}

// chip is a small rounded label for one figure.
func chip(c *ui.Context, t *ui.Theme, text string, tint ui.Color) {
	ui.Box(c).Radius(999).Border(1, t.Border).Padding(4, 10).
		Children(func() {
			ui.Text(c, text).FontSize(11).TextColor(tint)
		})
}

// money renders a large figure in the units the figures come in.
func money(v float64) string {
	abs := v
	if abs < 0 {
		abs = -abs
	}
	sign := ""
	if v < 0 {
		sign = "-"
	}
	switch {
	case abs >= 1e9:
		return fmt.Sprintf("%s%.1fb", sign, abs/1e9)
	case abs >= 1e6:
		return fmt.Sprintf("%s%.1fm", sign, abs/1e6)
	}
	return fmt.Sprintf("%s%.0f", sign, abs)
}
