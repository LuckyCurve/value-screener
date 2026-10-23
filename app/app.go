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

	// Screen is set by New to run the Python data layer and the engine.
	Screen Screen

	mu       sync.Mutex
	status   Status
	progress int
	total    int
	message  string
	elapsed  string
	result   *engine.Result
	running  bool
	cancel   context.CancelFunc
	done     chan struct{}

	// tab is 0 for passing stocks, 1 for rejected ones.
	tab int
	// passedChosen and rejectedChosen are the rows picked in each table,
	// -1 for none, kept apart so switching tabs keeps both choices.
	passedChosen   int
	rejectedChosen int

	passed, rejected ui.ListState

	// services lets the run goroutine ask for a frame.
	services ui.Services
}

// New returns an app on the shipped criteria, wired to the real data layer.
func New() *App {
	a := &App{Screen: DefaultScreen()}
	a.load(config.Default())
	return a
}

// NewFrom returns an app on the given criteria, wired to the real data layer.
func NewFrom(c config.Criteria) *App {
	a := &App{Screen: DefaultScreen()}
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
	a.passedChosen, a.rejectedChosen = -1, -1
	a.passed.Selected = &a.passedChosen
	a.passed.Key = func(row int) any { return a.key(true, row) }
	a.rejected.Selected = &a.rejectedChosen
	a.rejected.Key = func(row int) any { return a.key(false, row) }
}

// key identifies a row by its code, so the choice follows a stock across runs
// rather than sticking to a row number.
func (a *App) key(passed bool, row int) string {
	a.mu.Lock()
	r := a.result
	a.mu.Unlock()
	if r == nil {
		return fmt.Sprint(row)
	}
	vs := r.Rejected
	if passed {
		vs = r.Passed
	}
	if row < 0 || row >= len(vs) {
		return fmt.Sprint(row)
	}
	return vs[row].Stock.Code
}

// chosenRow is the row picked in the table on show, -1 for none.
func (a *App) chosenRow() int {
	if a.tab == 1 {
		return a.rejectedChosen
	}
	return a.passedChosen
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
	if a.running {
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
	a.mu.Unlock()
	a.passedChosen, a.rejectedChosen = -1, -1
	a.tab = 0

	go a.run(ctx, cancel, c, done)
	return true
}

func (a *App) run(ctx context.Context, cancel context.CancelFunc, c config.Criteria, done chan struct{}) {
	defer close(done)
	started := time.Now()

	res, err := a.Screen(ctx, c, func(index, total int, message string) {
		a.mu.Lock()
		a.progress, a.total = index, total
		if message != "" {
			a.message = message
		}
		a.mu.Unlock()
		a.invalidate()
	})
	cancel()

	a.mu.Lock()
	a.running = false
	a.cancel = nil
	switch {
	case errors.Is(err, context.Canceled):
		a.status = statusIdle
		a.message = ""
		a.progress, a.total = 0, 0
	case err != nil:
		a.status = statusFailed
		a.message = err.Error()
	default:
		a.status = statusFinished
		a.message = ""
		a.result = &res
		a.elapsed = time.Since(started).Round(time.Second).String()
	}
	a.mu.Unlock()
	a.invalidate()
}

// Cancel stops the run under way, if any. The child process is killed with
// the context, so OpenD is not left holding a stranded connection.
func (a *App) Cancel() {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
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

	ui.Row(c).Fill().Children(func() {
		ui.Column(c).Width(356).FillHeight().Padding(22, 18, 18, 18).
			Background(t.Surface).Gap(18).Children(func() {
			a.brand(c, t)
			a.form(c, t)
		})
		ui.Column(c).Grow(1).FillHeight().Padding(24, 20, 20, 20).Gap(16).Children(func() {
			a.results(c, t)
		})
	})
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
		btn := ui.Button(c, "").FillWidth().Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconStop).FontSize(15)
				ui.Text(c, "Cancel").SingleLine()
			})
		})
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
	a.mu.Unlock()

	switch status {
	case statusRunning:
		card(c, t, 12, func() {
			ui.Column(c).Gap(10).Children(func() {
				ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconClock).FontSize(16).TextColor(t.Accent)
					ui.Text(c, "Screening").FontSize(14).Bold().Grow(1)
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

	case statusFinished:
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

func (a *App) finished(c *ui.Context, t *ui.Theme, r *engine.Result, elapsed string) {
	if r == nil {
		return
	}
	ui.Row(c).Gap(10).Children(func() {
		stat(c, t, "Passed", fmt.Sprint(len(r.Passed)), t.Success, iconCheck)
		stat(c, t, "Rejected", fmt.Sprint(len(r.Rejected)), t.TextMuted, iconCross)
		if elapsed != "" {
			stat(c, t, "Took", elapsed, t.TextMuted, iconClock)
		}
	})

	ui.Tabs(c, &a.tab, fmt.Sprintf("Passed (%d)", len(r.Passed)),
		fmt.Sprintf("Rejected (%d)", len(r.Rejected)))

	// The detail sits above the table so it stays put as the table scrolls.
	a.detail(c, t, r)

	if a.tab == 0 {
		a.passedTable(c, t, r)
	} else {
		a.rejectedTable(c, t, r)
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

	ui.Table(c, &a.passed, cols, len(r.Passed), func(row, col int) {
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
	ui.Table(c, &a.rejected, cols, len(r.Rejected), func(row, col int) {
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
}

// detail shows one stock's figures year by year, so the choice can be judged
// against the rules rather than taken on the table's word.
func (a *App) detail(c *ui.Context, t *ui.Theme, r *engine.Result) {
	vs := r.Passed
	if a.tab == 1 {
		vs = r.Rejected
	}
	n := a.chosenRow()
	if n < 0 || n >= len(vs) {
		return
	}
	v := vs[n]

	card(c, t, 12, func() {
		ui.Column(c).Gap(10).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(1).Children(func() {
					ui.Text(c, v.Stock.Name).FontSize(15).Bold()
					ui.Text(c, v.Stock.Code).FontSize(11).TextColor(t.TextMuted)
				})
				if v.Valuation != nil {
					chip(c, t, fmt.Sprintf("PE %.1f", v.Stock.PE), t.TextMuted)
					chip(c, t, fmt.Sprintf("%d-yr avg %.1f",
						r.Criteria.PercentileYears, v.Valuation.Average), t.TextMuted)
				} else {
					chip(c, t, fmt.Sprintf("PE %.1f", v.Stock.PE), t.TextMuted)
				}
			})

			for _, f := range v.Failures {
				ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, iconCross).FontSize(12).TextColor(t.Danger)
					ui.Text(c, f.Criterion+": "+f.Reason).FontSize(12).
						TextColor(t.Danger).Grow(1)
				})
			}
			for _, note := range v.Notes {
				ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, iconInfo).FontSize(12).TextColor(t.Warning)
					ui.Text(c, note).FontSize(12).TextColor(t.TextMuted).Grow(1)
				})
			}

			// Capped so the table below stays in view; the years scroll.
			ui.Scroll(c).Height(112).FillWidth().Children(func() {
				years := make([]int, 0, len(v.ROE.ROE))
				for y := range v.ROE.ROE {
					years = append(years, y)
				}
				sort.Sort(sort.Reverse(sort.IntSlice(years)))
				ui.Column(c).Gap(1).Children(func() {
					for _, y := range years {
						money0 := fmt.Sprintf("%d", y)
						roe := v.ROE.ROE[y]
						bal, ok := v.Balance.Years[y]
						row := ui.Row(c).Gap(0).Children(func() {
							ui.Text(c, money0).Width(52).FontSize(12).TextColor(t.TextMuted)
							roeColour := t.Text
							if roe < r.Criteria.ROEMin {
								roeColour = t.Warning
							}
							ui.Textf(c, "ROE %.1f%%", roe*100).Width(104).
								FontSize(12).TextColor(roeColour)
							if !ok {
								ui.Text(c, "—").FontSize(12).TextColor(t.TextMuted)
								return
							}
							net := bal.Cash - bal.Debt
							ui.Textf(c, "cash %s", money(bal.Cash)).Width(104).
								FontSize(12).TextColor(t.TextMuted)
							ui.Textf(c, "debt %s", money(bal.Debt)).Width(104).
								FontSize(12).TextColor(t.TextMuted)
							netColour := t.Success
							if net <= 0 {
								netColour = t.Danger
							}
							ui.Textf(c, "net %s", money(net)).FontSize(12).TextColor(netColour)
						})
						_ = row
					}
				})
			})
		})
	})
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
