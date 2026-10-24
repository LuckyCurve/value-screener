package app

import (
	"errors"

	"github.com/egoist/mygo/ui"
)

// tips is which setup page the window shows, if any. A failure is the only
// way in: nothing the rules do needs a page of its own.
type tips int

const (
	tipsNone tips = iota
	// tipsOpenD: OpenD was not reachable, so nothing was fetched.
	tipsOpenD
	// tipsFailed: the run died for a reason the window cannot name.
	tipsFailed
)

// tipsPage is what to do when a run could not get its figures. The two ways
// in are close enough to share a page: the first step is the same.
func (a *App) tipsPage(c *ui.Context, t *ui.Theme, kind tips) {
	title := "OpenD is not running"
	lead := "Nothing was fetched, so no rule is at fault."
	if kind == tipsFailed {
		title = "The screen stopped"
		lead = "The stocks judged before it stopped are still below the page."
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(12).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconWarn).FontSize(18).TextColor(t.Warning)
				ui.Text(c, title).FontSize(15).Bold()
			})
			ui.Text(c, lead).FontSize(12).TextColor(t.TextMuted)

			if kind == tipsOpenD {
				tip(c, t, "1", "Start OpenD",
					"Open the Futu OpenD program and log in, and leave it running.")
				tip(c, t, "2", "Check the port",
					"OpenD listens on 127.0.0.1:11111. If it was changed, set it back.")
			}
			tip(c, t, firstStep(kind), "Try again",
				"Then run the screen again. The rules on the left need no change.")

			ui.Row(c).Gap(8).Children(func() {
				if ui.PrimaryButton(c, "Try again").Clicked() {
					a.Start()
				}
				if ui.Button(c, "Hide tips").Clicked() {
					a.dismissTips()
				}
			})
		})
	})
}

// firstStep numbers the last tip after the ones only one failure needs.
func firstStep(kind tips) string {
	if kind == tipsOpenD {
		return "3"
	}
	return "1"
}

// tip is one numbered step: what to do, and the one detail that makes it
// work.
func tip(c *ui.Context, t *ui.Theme, n, title, body string) {
	ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Size(20, 20).Radius(10).Background(t.SurfaceHover).
			Border(1, t.Border).Center().Children(func() {
			ui.Text(c, n).FontSize(11).Bold().TextColor(t.TextMuted)
		})
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(12).Bold()
			ui.Text(c, body).FontSize(12).TextColor(t.TextMuted)
		})
	})
}

// dismissTips puts the failure back to its plain one-line form, so the page
// is not reopened on every frame.
func (a *App) dismissTips() {
	a.mu.Lock()
	a.tips = tipsNone
	a.mu.Unlock()
}

// withTips is the failure the run ended on, as the page it opens.
func withTips(err error) tips {
	switch {
	case errors.Is(err, errOpenD):
		return tipsOpenD
	case errors.Is(err, errRunFailed):
		return tipsFailed
	}
	return tipsNone
}
