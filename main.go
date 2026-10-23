// Value screen runs a value-investing screen over HK, US and A-share
// listings fetched from Futu's OpenD, and shows the figures it turns up in a
// native window.
package main

import (
	"log"
	"path/filepath"

	"github.com/LuckyCurve/value-screener/app"
	"github.com/LuckyCurve/value-screener/config"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	c, err := loadCriteria()
	if err != nil {
		log.Print(err)
		c = config.Default()
	}

	a := app.NewFrom(c)
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Value screen",
			Width:   1100,
			Height:  720,
			Content: ui.View(a.View),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// loadCriteria reads criteria.json from the project, so the app opens on the
// rules the user last wrote there. A missing file means the shipped defaults.
func loadCriteria() (config.Criteria, error) {
	dir, err := app.ProjectDir()
	if err != nil {
		return config.Default(), err
	}
	return config.Load(filepath.Join(dir, "criteria.json"))
}
