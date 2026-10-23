// Value screen runs a value-investing screen over HK, US and A-share
// listings fetched from Futu's OpenD, and shows the figures it turns up in a
// native window.
package main

import (
	_ "embed"
	"log"
	"path/filepath"

	"github.com/LuckyCurve/value-screener/app"
	"github.com/LuckyCurve/value-screener/config"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// icon is the window's icon, shown in the taskbar and above the window's
// title bar. It is the same picture the executable carries as its resource.
//
//go:embed app/icon.png
var icon []byte

func main() {
	c, err := loadCriteria()
	if err != nil {
		log.Print(err)
		c = config.Default()
	}

	a := app.NewFrom(c)
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{
			Title: "Value screen",
			// The view draws its own title bar, so the window is all
			// content: rounded corners, a shadow and the resizing edges stay,
			// the system's bar and buttons do not.
			TitleBarStyle:     mygo.TitleBarHidden,
			HideWindowButtons: true,
			TitleBarHeight:    40,
			BackgroundColor:   "#ffffff",
			Width:             1160,
			Height:            760,
			MinWidth:          920,
			MinHeight:         620,
			Content:           ui.View(a.View),
		})
		if err := w.SetIcon(icon); err != nil {
			log.Print(err)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// loadCriteria reads criteria.json from the app's folder, so it opens on the
// rules the user last wrote there. A missing file means the shipped defaults.
func loadCriteria() (config.Criteria, error) {
	return config.Load(filepath.Join(app.BaseDir(), "criteria.json"))
}
