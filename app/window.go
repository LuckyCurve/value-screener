package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// titleBarHeight is the band at the top of the window that drags it and
// holds the window controls. It is ours, not the system's: the window is
// created with a hidden title bar and no system buttons, so the frame looks
// like the rest of the window while keeping minimize, maximize and close.
const titleBarHeight = 40

// titleBar draws that band: a drag strip holding the window controls on the
// right. The app's name and mark belong to the sidebar's header, one band
// below; repeating them here reads as two headers stacked.
func (a *App) titleBar(c *ui.Context, t *ui.Theme) {
	bar := ui.Row(c).FillWidth().Height(titleBarHeight).
		Padding(0, 4, 0, 10).Gap(8).AlignItems(ui.Center).
		Background(t.Surface).DragWindow()
	// A title bar does what the system's does under a double click.
	if bar.DoubleClicked() {
		if w := mygo.FocusedWindow(); w != nil {
			w.ToggleMaximize()
		}
	}

	bar.Children(func() {
		ui.Row(c).Grow(1).DragWindow()
		a.windowButtons(c, t)
	})
}

// windowButtons draws minimize, maximize and close over the title bar.
func (a *App) windowButtons(c *ui.Context, t *ui.Theme) {
	w := mygo.FocusedWindow()
	maximized := w != nil && w.IsMaximized()

	if windowButton(c, t, "Minimize window", iconMinimize).Clicked() {
		if w != nil {
			w.Minimize()
		}
	}

	mark := iconMaximize
	if maximized {
		mark = iconRestore
	}
	toggle := windowButton(c, t, "Maximize window", mark)
	// The system gives a window whose maximize button says so the snap
	// layouts of Windows 11 on hover.
	toggle.MaximizeButton()
	if toggle.Clicked() {
		if w != nil {
			w.ToggleMaximize()
		}
	}

	closeBtn := windowButton(c, t, "Close window", iconWinClose)
	if closeBtn.Hovered() {
		closeBtn.Background(t.Danger)
	}
	if closeBtn.Clicked() {
		if w != nil {
			w.Close()
		}
	}
}

// windowButton is one window control: a wide mark that lights up under the
// pointer.
func windowButton(c *ui.Context, t *ui.Theme, label string, mark *ui.SVG) ui.Element {
	b := ui.ButtonBase(c).Size(46, 30).Radius(0).Center().Label(label)
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		line := t.TextMuted
		if label == "Close window" && b.Hovered() {
			line = ui.Hex("#ffffff")
		}
		ui.Icon(c, mark).FontSize(10).TextColor(line)
	})
	return b
}
