package app

import "github.com/egoist/mygo/ui"

// Icons are inline SVGs drawn in the text colour, so they follow the theme.
// They are parsed once, not in the view.

func icon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	// A ledger page with a magnifier: the app itself.
	iconLogo = icon(`<path d="M4 4h9a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H4z"/><path d="M7 8h5M7 12h5"/><circle cx="17.5" cy="15.5" r="3.5"/><path d="m20 18 2 2"/>`)
	// Sliders: the rules.
	iconRules = icon(`<path d="M4 6h16M4 12h16M4 18h16"/><circle cx="9" cy="6" r="2"/><circle cx="15" cy="12" r="2"/><circle cx="7" cy="18" r="2"/>`)
	iconRun   = icon(`<path d="M7 4.5v15l12-7.5z"/>`)
	iconStop  = icon(`<rect x="6" y="6" width="12" height="12" rx="2"/>`)
	iconCheck = icon(`<path d="m4 12.5 5 5L20 6.5"/>`)
	iconCross = icon(`<path d="M6 6l12 12M18 6 6 18"/>`)
	iconInfo  = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 11v6"/><path d="M12 7.6v.2"/>`)
	iconRows  = icon(`<path d="M4 6h16M4 12h16M4 18h16"/>`)
	iconClock = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.5 2"/>`)
	iconCoins = icon(`<ellipse cx="12" cy="6.5" rx="7" ry="3"/><path d="M5 6.5v5c0 1.7 3.1 3 7 3s7-1.3 7-3v-5"/><path d="M5 11.5v5c0 1.7 3.1 3 7 3s7-1.3 7-3v-5"/>`)
	iconTrend = icon(`<path d="M3 17l6-6 4 4 8-8"/><path d="M15 7h6v6"/>`)
	iconWarn  = icon(`<path d="M12 4 2.5 20h19z"/><path d="M12 10v4"/><path d="M12 17.2v.2"/>`)
	iconRetry = icon(`<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/>`)
	iconCopy  = icon(`<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V6a2 2 0 0 1 2-2h9"/>`)
	iconClose = icon(`<path d="M6 6l12 12M18 6 6 18"/>`)

	// Window controls, drawn thin like the system's own.
	iconMinimize = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><path d="M1.5 5h7" stroke="currentColor" stroke-width="1"/></svg>`))
	iconMaximize = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect x="1.5" y="1.5" width="7" height="7" rx="0.6" fill="none" stroke="currentColor" stroke-width="1"/></svg>`))
	iconRestore  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1"><rect x="1.5" y="3.5" width="5" height="5" rx="0.6"/><path d="M3.9 3.5V2.4a.9.9 0 0 1 .9-.9h2.9a.9.9 0 0 1 .9.9v2.9a.9.9 0 0 1-.9.9H6.6"/></svg>`))
	iconWinClose = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><path d="M1.6 1.6l6.8 6.8M8.4 1.6 1.6 8.4" stroke="currentColor" stroke-width="1"/></svg>`))
)
