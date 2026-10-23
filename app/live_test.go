//go:build live

// These tests reach the running OpenD on 127.0.0.1:11111 and the live Futu
// servers: `go test -tags live ./app -run Live -v`. They stay out of ordinary
// runs so the suite never depends on the market being open.
package app

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyCurve/value-screener/config"
	"github.com/LuckyCurve/value-screener/dataset"
	"github.com/LuckyCurve/value-screener/engine"
	"github.com/LuckyCurve/value-screener/runner"
)

// TestLiveDataLayerFetchesTheFigures checks the pieces the app glues together
// against a real OpenD: the child process, its progress lines, the dataset it
// writes, and the verdicts the engine draws from it.
func TestLiveDataLayerFetchesTheFigures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	dir, err := ProjectDir()
	if err != nil {
		t.Fatalf("project dir: %v", err)
	}
	c := config.Default()
	c.Markets = []string{"HK"}

	cfgPath, err := writeCriteria(dir, c)
	if err != nil {
		t.Fatal(err)
	}

	var sawFound, sawStock bool
	data, err := runner.Run(ctx, dir, runner.Options{
		ConfigPath: cfgPath,
		OutPath:    t.TempDir() + "/dataset.json",
		Limit:      2,
		OnEvent: func(ev runner.Event) {
			switch ev.Event {
			case "found":
				sawFound = true
			case "stock":
				sawStock = true
			}
		},
	})
	if err != nil {
		t.Fatalf("data layer: %v", err)
	}
	if !sawFound || !sawStock {
		t.Errorf("progress never reached the window: found=%v stock=%v", sawFound, sawStock)
	}

	verdicts, err := dataset.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(verdicts) == 0 {
		t.Fatal("the data layer returned no stocks")
	}
	for _, v := range verdicts {
		if v.Stock.Code == "" || v.Stock.PE <= 0 {
			t.Errorf("%+v came back without its figures", v.Stock)
		}
	}

	judged := make([]engine.Verdict, 0, len(verdicts))
	for _, v := range verdicts {
		judged = append(judged, engine.Evaluate(v.Stock, v.ROE, v.Balance, c))
	}
	res := engine.BuildResult(c, judged)
	if len(res.Passed)+len(res.Rejected) != len(verdicts) {
		t.Errorf("judged %d of %d stocks", len(res.Passed)+len(res.Rejected), len(verdicts))
	}
}
